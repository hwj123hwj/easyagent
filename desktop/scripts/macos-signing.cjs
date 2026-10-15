const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { execFileSync, spawnSync } = require('node:child_process');

const defaultConfig = () => path.join(os.homedir(), 'Library/Application Support/EasyAgent Signing/identity.json');
function configuration(env = process.env) {
  if (env.EA_MAC_SIGNING_MODE && !['auto', 'adhoc'].includes(env.EA_MAC_SIGNING_MODE)) throw Error('Invalid signing mode');
  if (env.EA_MAC_SIGNING_MODE === 'adhoc') return { mode: 'adhoc', identity: '-' };
  const filename = env.EA_MAC_SIGNING_CONFIG || defaultConfig();
  if (!fs.existsSync(filename)) {
    if (env.EA_MAC_SIGNING_CONFIG || fs.existsSync(path.join(path.dirname(filename), 'signing.keychain-db'))) throw Error('Configured signing identity is missing; refusing ad hoc fallback');
    return { mode: 'adhoc', identity: '-' };
  }
  const config = JSON.parse(fs.readFileSync(filename, 'utf8'));
  if (config.version !== 1 || !/^[A-Fa-f0-9]{40}$/.test(config.identity) ||
      !path.isAbsolute(config.keychain) || !path.isAbsolute(config.passwordFile) ||
      !fs.existsSync(config.keychain) || !fs.existsSync(config.passwordFile)) throw Error('Invalid local signing configuration');
  for (const file of [filename, config.keychain, config.passwordFile]) {
    const stat = fs.lstatSync(file), parent = fs.lstatSync(path.dirname(file));
    if (!stat.isFile() || (stat.mode & 0o077) || stat.uid !== process.getuid() || !parent.isDirectory() || (parent.mode & 0o077) || parent.uid !== process.getuid()) throw Error('Signing files must be private to the current user');
  }
  return { ...config, mode: 'local_certificate', identity: config.identity.toUpperCase() };
}
function unlock(config) {
  if (config.mode === 'adhoc') return;
  const password = fs.readFileSync(config.passwordFile, 'utf8').trim();
  // Never emit the credential, command arguments or child-process errors.
  try { execFileSync('/usr/bin/security', ['unlock-keychain', '-p', password, config.keychain], { stdio: 'ignore' }); }
  catch { throw Error('Unable to unlock the EasyAgent signing keychain'); }
}
function requirement(identifier, config) {
  if (!/^[A-Za-z0-9_.-]+$/.test(identifier)) throw Error('Invalid signing identifier');
  return `identifier "${identifier}" and certificate leaf = H"${config.identity}"`;
}
async function withKeychain(config, action) {
  if (config.mode === 'adhoc') return await action();
  const lock = path.join(path.dirname(config.keychain), '.codesign-lock');
  const takeLock = () => { fs.mkdirSync(lock, { mode: 0o700 }); fs.writeFileSync(path.join(lock, 'pid'), String(process.pid), { mode: 0o600 }); };
  try { takeLock(); } catch (error) {
    if (error.code !== 'EEXIST') throw Error('Unable to lock the EasyAgent signing identity');
    const stat = fs.lstatSync(lock), pid = Number(fs.readFileSync(path.join(lock, 'pid'), 'utf8'));
    if (!stat.isDirectory() || stat.uid !== process.getuid() || (stat.mode & 0o077) || !Number.isSafeInteger(pid) || pid <= 1) throw Error('Invalid signing lock');
    try { process.kill(pid, 0); throw Error('EasyAgent signing is already in progress'); }
    catch (error) { if (error.code !== 'ESRCH') throw error; }
    const snapshot = path.join(lock, 'original.json');
    if (fs.existsSync(snapshot)) {
      const previous = JSON.parse(fs.readFileSync(snapshot, 'utf8'));
      if (!Array.isArray(previous)) throw Error('Invalid signing recovery snapshot');
      if (!previous.includes(config.keychain)) {
        const current = execFileSync('/usr/bin/security', ['list-keychains', '-d', 'user'], { encoding: 'utf8' })
          .split('\n').map(line => line.trim()).filter(Boolean).map(line => JSON.parse(line));
        execFileSync('/usr/bin/security', ['list-keychains', '-d', 'user', '-s', ...current.filter(file => file !== config.keychain)], { stdio: 'ignore' });
      }
      fs.unlinkSync(snapshot);
    }
    fs.unlinkSync(path.join(lock, 'pid')); fs.rmdirSync(lock); takeLock();
  }
  try {
    unlock(config);
    const original = execFileSync('/usr/bin/security', ['list-keychains', '-d', 'user'], { encoding: 'utf8' })
      .split('\n').map(line => line.trim()).filter(Boolean).map(line => JSON.parse(line));
    fs.writeFileSync(path.join(lock, 'original.json'), JSON.stringify(original), { mode: 0o600 });
    // codesign still needs the identity in the search list even with --keychain.
    // Scope this to signing and always restore the user's original list.
    execFileSync('/usr/bin/security', ['list-keychains', '-d', 'user', '-s', ...new Set([...original, config.keychain])], { stdio: 'ignore' });
    try { return await action(); }
    finally {
      const current = execFileSync('/usr/bin/security', ['list-keychains', '-d', 'user'], { encoding: 'utf8' })
        .split('\n').map(line => line.trim()).filter(Boolean).map(line => JSON.parse(line));
      // Preserve any unrelated search-list edits made while signing was running.
      const restored = original.includes(config.keychain) ? current : current.filter(file => file !== config.keychain);
      execFileSync('/usr/bin/security', ['list-keychains', '-d', 'user', '-s', ...restored], { stdio: 'ignore' });
    }
  } finally {
    const snapshot = path.join(lock, 'original.json'); if (fs.existsSync(snapshot)) fs.unlinkSync(snapshot);
    fs.unlinkSync(path.join(lock, 'pid')); fs.rmdirSync(lock);
  }
}
async function sign(app, identifier, config = configuration()) {
  const args = ['--force', '--sign', config.identity, '--identifier', identifier, '--timestamp=none'];
  if (config.mode !== 'adhoc') args.push('--keychain', config.keychain, '--requirements', '=designated => ' + requirement(identifier, config));
  await withKeychain(config, () => execFileSync('/usr/bin/codesign', [...args, app], { stdio: 'inherit' }));
}
function verify(app, identifier, config = configuration()) {
  execFileSync('/usr/bin/codesign', ['--verify', '--deep', '--strict', app], { stdio: 'inherit' });
  if (config.mode !== 'adhoc') {
    execFileSync('/usr/bin/codesign', ['--verify', '-R', '=' + requirement(identifier, config), app], { stdio: 'inherit' });
  } else {
    if (!/^[A-Za-z0-9_.-]+$/.test(identifier)) throw Error('Invalid signing identifier');
    execFileSync('/usr/bin/codesign', ['--verify', '-R', `=identifier "${identifier}"`, app], { stdio: 'inherit' });
    const display = spawnSync('/usr/bin/codesign', ['--display', '--verbose=4', app], { encoding: 'utf8' });
    if (display.status !== 0 || !(display.stdout + display.stderr).includes('Signature=adhoc')) throw Error('Expected an ad hoc signature');
  }
}
module.exports = { configuration, unlock, requirement, withKeychain, sign, verify };
if (require.main === module) {
  (async () => {
    const [command, app, identifier] = process.argv.slice(2);
    if (command === 'sign') await sign(app, identifier);
    else if (command === 'verify') verify(app, identifier);
    else if (command === 'mode') process.stdout.write(configuration().mode);
    else throw Error('Expected sign / verify / mode');
  })().catch(error => { process.stderr.write(error.message + '\n'); process.exitCode = 1; });
}
