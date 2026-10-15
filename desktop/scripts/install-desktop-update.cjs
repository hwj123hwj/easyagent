const fs = require('node:fs');
const path = require('node:path');
const { spawn, execFileSync } = require('node:child_process');
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const alive = pid => { try { process.kill(pid, 0); return true; } catch { return false; } };
async function waitForExit(pid, timeout = 60000) {
  const end = Date.now() + timeout;
  while (alive(pid)) { if (Date.now() >= end) throw Error('previous process is still running'); await delay(200); }
}
function verify(plan, app) {
  const version = execFileSync('/usr/bin/plutil', ['-extract', 'CFBundleShortVersionString', 'raw', '-o', '-', path.join(app, 'Contents/Info.plist')], { encoding: 'utf8' }).trim();
  if (version !== (app === plan.candidate ? plan.version : plan.currentVersion)) throw Error('installation version changed');
  execFileSync('/usr/bin/codesign', ['--verify', '--deep', '--strict', '-R', '=' + plan.requirement, app], { stdio: 'ignore' });
  execFileSync('/usr/bin/codesign', ['--verify', '--strict', '-R', '=' + plan.helperRequirement, path.join(app, 'Contents/Resources/cua-helper/EasyAgent Computer Use.app')], { stdio: 'ignore' });
}
function unregister(app) {
  const bundles = [];
  const visit = dir => { for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const p = path.join(dir, entry.name); visit(p); if (entry.name.endsWith('.app')) bundles.push(p);
  } };
  visit(app);
  for (const p of [...bundles, app]) {
    try { execFileSync('/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister', ['-u', p], { stdio: 'ignore' }); } catch {}
  }
}
async function install(plan, deps = {}) {
  const verifyApp = deps.verify || verify, launch = deps.launch || ((app, receipt) => {
    const env = { ...process.env }; delete env.ELECTRON_RUN_AS_NODE;
    if (plan.automatic) env.EA_DESKTOP_UPDATE_BACKGROUND = '1'; else delete env.EA_DESKTOP_UPDATE_BACKGROUND;
    if (receipt) { env.EA_DESKTOP_UPDATE_RECEIPT = receipt; env.EA_DESKTOP_UPDATE_REQUIRE_PERMISSIONS = plan.requirePermissions ? '1' : '0'; }
    else { delete env.EA_DESKTOP_UPDATE_RECEIPT; delete env.EA_DESKTOP_UPDATE_REQUIRE_PERMISSIONS; env.EA_DESKTOP_UPDATE_ROLLBACK = '1'; }
    const child = spawn(path.join(app, 'Contents/MacOS/EasyAgent'), [], { detached: true, env, stdio: 'ignore' });
    // Keep error handling attached after spawn to avoid an uncaught ENOENT.
    child.on('error', () => {}); child.unref(); return child;
  });
  const backup = path.join(plan.stage, 'previous.app'); let swapped = false, child;
  try {
    await (deps.waitForExit || waitForExit)(plan.parentPid);
    verifyApp(plan, plan.target); verifyApp(plan, plan.candidate);
    if (fs.existsSync(backup)) throw Error('rollback path already exists');
    fs.renameSync(plan.target, backup);
    try { fs.renameSync(plan.candidate, plan.target); } catch (error) { fs.renameSync(backup, plan.target); throw error; }
    swapped = true;
    child = launch(plan.target, plan.receipt);
    const end = Date.now() + (deps.healthTimeout || 60000);
    let healthy = false;
    while (Date.now() < end) {
      try { const receipt = JSON.parse(fs.readFileSync(plan.receipt, 'utf8'));
        if (receipt.version === plan.version && receipt.healthy === true && (!plan.requirePermissions || receipt.granted === true)) { healthy = true; break; }
        if (receipt.healthy === false) break;
      } catch { /* The app writes the receipt atomically after its own core is healthy. */ }
      if (!child.pid || !alive(child.pid)) break;
      await delay(200);
    }
    if (!healthy) throw Error('updated app failed health or permission verification');
    fs.writeFileSync(plan.resultFile, JSON.stringify({ version: plan.version, ok: true, backup }), { mode: 0o600 });
    if (plan.journal) { try { fs.unlinkSync(plan.journal); } catch {} }
    if (!deps.verify) unregister(backup);
    return { ok: true };
  } catch {
    if (swapped) {
      if (child?.pid && alive(child.pid)) { child.kill('SIGTERM'); await (deps.waitForExit || waitForExit)(child.pid, 30000); }
      // Never replace a failed candidate while its process is still running.
      verifyApp(plan, backup);
      fs.renameSync(plan.target, plan.candidate); fs.renameSync(backup, plan.target);
      if (!deps.verify) unregister(plan.candidate);
      launch(plan.target, null);
    }
    if (plan.journal) { try { fs.unlinkSync(plan.journal); } catch {} }
    fs.writeFileSync(plan.resultFile, JSON.stringify({ version: plan.version, ok: false, error: '更新安装失败，已保留或恢复旧版；自动安装暂停至下次启动' }), { mode: 0o600 });
    return { ok: false };
  }
}
module.exports = { install, waitForExit };
if (require.main === module) {
  (async () => {
    const filename = path.resolve(process.argv[2]);
    const plan = JSON.parse(fs.readFileSync(filename, 'utf8'));
    const stage = path.dirname(filename);
    if (plan.target !== '/Applications/EasyAgent.app' || path.dirname(stage) !== '/Applications' ||
        !path.basename(stage).startsWith('.EasyAgent-update-') || !stage.endsWith('.noindex') ||
        plan.stage !== stage || plan.candidate !== path.join(stage, 'EasyAgent.app') || plan.receipt !== path.join(stage, 'receipt.json') ||
        !Number.isSafeInteger(plan.parentPid) || plan.parentPid <= 1 || !/^\d+\.\d+\.\d+$/.test(plan.version) || !/^\d+\.\d+\.\d+$/.test(plan.currentVersion) ||
        fs.lstatSync(stage).uid !== process.getuid() || (fs.lstatSync(stage).mode & 0o077)) throw Error('invalid update handoff');
    verify(plan, plan.target); verify(plan, plan.candidate);
    process.stdout.write('READY\n'); await install(plan);
  })().catch(() => { process.exitCode = 1; });
}
