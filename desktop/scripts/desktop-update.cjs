const fs = require('node:fs');
const path = require('node:path');
const { createHash } = require('node:crypto');
const { execFileSync, spawn } = require('node:child_process');
const { valid, gt } = require('semver');
const signing = require('./macos-signing.cjs');
const signApp = require('./sign-adhoc.cjs');
const REPO = 'hwj123hwj/easyagent';
const APP_ID = 'com.easyagent.desktop', HELPER_ID = 'com.easyagent.cua-helper';
const helperPath = app => path.join(app, 'Contents/Resources/cua-helper/EasyAgent Computer Use.app');
const run = (command, args) => execFileSync(command, args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], timeout: 120000 });
const plist = (app, key) => run('/usr/bin/plutil', ['-extract', key, 'raw', '-o', '-', path.join(app, 'Contents/Info.plist')]).trim();
function releasePayload(manifest, version, arch) {
  if (!valid(version) || !/^\d+\.\d+\.\d+$/.test(version) || !['arm64', 'x64'].includes(arch) ||
      manifest?.version !== 'v' + version || !/^[a-f0-9]{40}$/.test(manifest.commit)) throw Error('更新清单的版本或提交无效');
  const name = `EasyAgent-${version}-${arch}.dmg`, digest = manifest.assets?.[name];
  if (typeof digest !== 'string' || !/^[a-f0-9]{64}$/.test(digest)) throw Error('更新清单缺少本机安装包校验和');
  return { url: `https://github.com/${REPO}/releases/download/v${version}/${name}`, digest, commit: manifest.commit };
}
function unregister(app) {
  if (!fs.existsSync(app)) return;
  const bundles = [];
  const visit = dir => { for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const p = path.join(dir, entry.name); visit(p); if (entry.name.endsWith('.app')) bundles.push(p);
  } };
  visit(app);
  for (const p of [...bundles, app]) {
    try { run('/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister', ['-u', p]); } catch { /* -10814 after unregister is harmless. */ }
  }
}
async function download(response, filename, digest) {
  if (!response.ok || !response.body) throw Error('下载安装包失败');
  const hash = createHash('sha256'), fd = fs.openSync(filename, 'wx', 0o600);
  let size = 0;
  try {
    for await (const chunk of response.body) {
      size += chunk.length; if (size > 512 * 1024 * 1024) throw Error('安装包超过大小限制');
      hash.update(chunk); fs.writeSync(fd, chunk);
    }
  } finally { fs.closeSync(fd); }
  if (!size || hash.digest('hex') !== digest) throw Error('安装包校验和不匹配，保留当前版本');
}
function createUpdater(options) {
  const target = path.resolve(options.target), stateFile = path.join(options.userData, 'desktop-updates.json');
  const journal = path.join(options.userData, 'desktop-update-pending.json');
  const savePending = value => { fs.mkdirSync(options.userData, { recursive: true, mode: 0o700 }); fs.writeFileSync(journal + '.tmp', JSON.stringify(value), { mode: 0o600 }); fs.renameSync(journal + '.tmp', journal); };
  const validStage = stage => typeof stage === 'string' && path.dirname(stage) === path.dirname(target) && path.basename(stage).startsWith('.EasyAgent-update-') && stage.endsWith('.noindex') && fs.lstatSync(stage).isDirectory() && fs.lstatSync(stage).uid === process.getuid() && !(fs.lstatSync(stage).mode & 0o077);
  let state = { supported: false, enabled: true, phase: 'idle' }, prepared, pending, installer;
  try {
    const saved = JSON.parse(fs.readFileSync(stateFile, 'utf8'));
    state.enabled = saved.enabled === true;
  } catch (error) { if (error.code !== 'ENOENT') state.enabled = false; }
  const emit = patch => { state = { ...state, ...patch }; options.publish?.(state); return { ...state }; };
  const identity = () => {
    if (options.platform !== 'darwin' || target !== '/Applications/EasyAgent.app') throw Error('自动安装仅支持 Applications 中的 Mac 应用');
    // The environment cannot force an ad hoc downgrade in this update path.
    const config = signing.configuration({ ...process.env, EA_MAC_SIGNING_MODE: 'auto' });
    if (config.mode !== 'local_certificate') throw Error('需要已配置的固定签名身份；不会自动创建证书或更改系统授权');
    signing.verify(target, APP_ID, config); signing.verify(helperPath(target), HELPER_ID, config);
    if (plist(target, 'CFBundleShortVersionString') !== options.version) throw Error('安装版本已改变，请重新启动应用后再检查更新');
    return config;
  };
  try { identity(); state.supported = true; } catch {
    state.message = '本机固定签名身份未就绪，自动安装已暂停；当前应用不受影响';
    try {
      const config = signing.configuration({ ...process.env, EA_MAC_SIGNING_MODE: 'auto' });
      if (config.mode === 'adhoc') { signing.verify(target, APP_ID, config); state.manualDownload = true; }
    } catch { /* A fixed identity mismatch can never authorize an ad hoc overwrite. */ }
  }
  // A normal app quit may stop the worker mid-download. Recover only our
  // private journaled staging directory; never scan or delete unrelated apps.
  try {
    const saved = JSON.parse(fs.readFileSync(journal, 'utf8'));
    if (!validStage(saved.stage)) throw Error('invalid staging journal');
    if (saved.phase === 'ready' && gt(saved.version, options.version) && state.supported &&
        saved.candidate === path.join(saved.stage, 'EasyAgent.app')) {
      const config = identity();
      signing.verify(saved.candidate, APP_ID, config); signing.verify(helperPath(saved.candidate), HELPER_ID, config);
      if (plist(saved.candidate, 'CFBundleShortVersionString') !== saved.version) throw Error('invalid staged version');
      prepared = saved; state.phase = 'ready'; state.version = saved.version;
    } else if (!fs.existsSync(path.join(saved.stage, 'previous.app'))) {
      unregister(path.join(saved.stage, 'mount/EasyAgent.app'));
      if (fs.existsSync(path.join(saved.stage, 'mount/EasyAgent.app'))) run('/usr/bin/hdiutil', ['detach', path.join(saved.stage, 'mount')]);
      unregister(path.join(saved.stage, 'EasyAgent.app')); fs.rmSync(saved.stage, { recursive: true }); fs.unlinkSync(journal);
    }
  } catch { /* Retain an unverified/busy stage for recovery, without installing it. */ }
  try {
    const result = JSON.parse(fs.readFileSync(path.join(options.userData, 'desktop-update-result.json'), 'utf8'));
    if (result.ok === false) { state.phase = 'error'; state.error = result.error; }
  } catch { /* No previous install report. */ }
  async function prepare(version) {
    if (pending) return pending;
    if (prepared?.version === version && state.phase !== 'error') return { ...state };
    if (prepared && state.phase === 'error') {
      unregister(prepared.candidate); fs.rmSync(prepared.stage, { recursive: true }); prepared = undefined;
      try { fs.unlinkSync(journal); } catch {}
    }
    pending = (async () => {
      let stage, mounted = false;
      try {
        if (!gt(version, options.version)) throw Error('拒绝重复版本或降级');
        const config = identity();
        emit({ phase: 'downloading', version, error: undefined });
        const manifestResponse = await options.fetch(`https://github.com/${REPO}/releases/download/v${version}/release.json`, { signal: AbortSignal.timeout(30000) });
        if (!manifestResponse.ok) throw Error('更新清单下载失败');
        const text = await manifestResponse.text(); if (text.length > 1024 * 1024) throw Error('更新清单过大');
        const payload = releasePayload(JSON.parse(text), version, options.arch);
        stage = fs.mkdtempSync(path.join(path.dirname(target), '.EasyAgent-update-')); fs.chmodSync(stage, 0o700);
        const noindex = stage + '.noindex'; fs.renameSync(stage, noindex); stage = noindex; savePending({ stage, version, phase: 'preparing' });
        const dmg = path.join(stage, 'installer.dmg'), mount = path.join(stage, 'mount'), candidate = path.join(stage, 'EasyAgent.app');
        await download(await options.fetch(payload.url, { signal: AbortSignal.timeout(10 * 60 * 1000) }), dmg, payload.digest);
        emit({ phase: 'verifying' }); run('/usr/bin/hdiutil', ['verify', dmg]); fs.mkdirSync(mount);
        run('/usr/bin/hdiutil', ['attach', '-readonly', '-nobrowse', '-mountpoint', mount, dmg]); mounted = true;
        const source = path.join(mount, 'EasyAgent.app');
        if (!fs.lstatSync(source).isDirectory() || plist(source, 'CFBundleIdentifier') !== APP_ID || plist(source, 'CFBundleShortVersionString') !== version ||
            plist(helperPath(source), 'CFBundleIdentifier') !== HELPER_ID || plist(helperPath(source), 'CFBundleShortVersionString') !== version) throw Error('安装包应用身份或版本不匹配');
        run('/usr/bin/codesign', ['--verify', '--deep', '--strict', source]);
        const core = path.join(source, 'Contents/Resources/easyagent'), metadata = fs.readFileSync(core);
        if (!metadata.includes(Buffer.from('vcs.revision=' + payload.commit)) || !metadata.includes(Buffer.from('vcs.modified=false'))) throw Error('安装包核心源码与发布清单不匹配');
        if (run(core, ['--version']).trim() !== 'easyagent v' + version || run(path.join(source, 'Contents/Resources/runtime/bin/node'), ['-p', 'process.arch']).trim() !== options.arch) throw Error('安装包核心版本或架构不匹配');
        run('/usr/bin/ditto', [source, candidate]); unregister(source);
        run('/usr/bin/hdiutil', ['detach', mount]); mounted = false;
        // Sign the downloaded helper first; the app signer preserves that signature.
        emit({ phase: 'signing' });
        await signing.sign(helperPath(candidate), HELPER_ID, config);
        // Ignore build-only EA_MAC_SIGNING_MODE overrides while reusing the validated identity.
        await signApp({ app: candidate, platform: 'darwin', config });
        signing.verify(candidate, APP_ID, config); signing.verify(helperPath(candidate), HELPER_ID, config);
        identity(); // Identity must still match the installed app after a long download.
        fs.copyFileSync(path.join(__dirname, 'install-desktop-update.cjs'), path.join(stage, 'install.cjs'));
        fs.copyFileSync(options.nodePath, path.join(stage, 'node')); fs.chmodSync(path.join(stage, 'node'), 0o700);
        fs.unlinkSync(dmg); unregister(candidate);
        prepared = { stage, candidate, version, requirement: signing.requirement(APP_ID, config), helperRequirement: signing.requirement(HELPER_ID, config) };
        savePending({ ...prepared, phase: 'ready' });
        return emit({ phase: 'ready' });
      } catch (error) {
        if (stage) {
          if (mounted) {
            unregister(path.join(stage, 'mount/EasyAgent.app'));
            try { run('/usr/bin/hdiutil', ['detach', path.join(stage, 'mount')]); mounted = false; } catch { /* Retain a busy volume. */ }
          }
          if (!mounted) { unregister(path.join(stage, 'EasyAgent.app')); fs.rmSync(stage, { recursive: true, force: true }); try { fs.unlinkSync(journal); } catch {} }
        }
        // Child argv/stderr can contain the keychain credential; never forward it.
        return emit({ phase: 'error', error: '自动更新准备失败，当前版本和授权未改动；请检查网络、磁盘空间与固定签名配置后重试' });
      }
    })().finally(() => { pending = undefined; });
    return pending;
  }
  function setEnabled(value) {
    if (typeof value !== 'boolean') throw Error('更新开关参数无效');
    fs.mkdirSync(options.userData, { recursive: true, mode: 0o700 });
    const tmp = stateFile + '.tmp'; fs.writeFileSync(tmp, JSON.stringify({ enabled: value }), { mode: 0o600 }); fs.renameSync(tmp, stateFile);
    return emit({ enabled: value });
  }
  async function handoff(parentPid, requirePermissions, automatic = false) {
    if (!prepared || state.phase !== 'ready') throw Error('更新尚未准备就绪');
    try {
    identity();
    const receipt = path.join(prepared.stage, 'receipt.json');
    const plan = { ...prepared, target, currentVersion: options.version, parentPid, receipt, requirePermissions, automatic, journal, resultFile: path.join(options.userData, 'desktop-update-result.json') };
    fs.writeFileSync(path.join(prepared.stage, 'plan.json'), JSON.stringify(plan), { mode: 0o600 });
    const child = spawn(path.join(prepared.stage, 'node'), [path.join(prepared.stage, 'install.cjs'), path.join(prepared.stage, 'plan.json')], { detached: true, stdio: ['ignore', 'pipe', 'ignore'] });
    try { await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(Error('安装进程启动超时')), 30000);
      child.once('error', () => { clearTimeout(timeout); reject(Error('安装进程无法启动')); });
      child.once('exit', () => { clearTimeout(timeout); reject(Error('安装进程提前退出')); });
      child.stdout.once('data', data => { clearTimeout(timeout); data.toString().includes('READY') ? resolve() : reject(Error('安装进程未就绪')); });
    }); } catch (error) { child.kill('SIGTERM'); throw error; }
    child.stdout.destroy(); child.unref(); installer = child; emit({ phase: 'installing' });
    return { ...state };
    } catch { emit({ phase: 'error', error: '更新安装未启动，当前版本和授权已保留；请重试' }); throw Error('更新安装未启动，当前版本保留'); }
  }
  function cancelHandoff() {
    // The parent has not quit yet, so the installer is only waiting. A late
    // edit/focus/terminal or disable event can safely cancel before replacement.
    if (installer) { installer.kill('SIGTERM'); installer = undefined; }
    return emit({ phase: prepared ? 'ready' : 'idle' });
  }
  return { status: () => ({ ...state }), prepare, setEnabled, handoff, cancelHandoff };
}
module.exports = { createUpdater, releasePayload, download };
