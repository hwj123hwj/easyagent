const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../.test-output/electron/update-checker.js'), 'utf8');
const manifest = {
  version: 'v0.3.0', commit: 'a'.repeat(40),
  assets: { 'EasyAgent-0.3.0-arm64.dmg': 'b'.repeat(64), 'EasyAgent-0.3.0-x64.dmg': 'c'.repeat(64) },
};
function response(status, body, headers = {}) {
  return { ok: status === 200, status, json: async () => body, headers: new Headers(headers) };
}
function fixture(fetch, current = '0.2.0-rc.1') {
  const calls = [];
  let now = 1_000_000;
  const module = { exports: {} };
  vm.runInNewContext(source, {
    module, exports: module.exports, URL, AbortSignal,
    Date: class extends Date { static now() { return now; } },
    process: { arch: 'arm64' },
    require: name => name === 'electron' ? {
      app: { getVersion: () => current },
      net: { fetch: (...args) => { calls.push(args); return fetch(...args); } },
    } : require(name),
  });
  return { ...module.exports, calls, advance: ms => { now += ms; } };
}
test('403 API quota falls back to the published manifest and respects reset time', async () => {
  const f = fixture(async url => url.includes('api.github.com')
    ? response(403, {}, { 'x-ratelimit-remaining': '0', 'x-ratelimit-reset': '4600' })
    : response(200, manifest));
  const info = await f.checkForUpdate();
  assert.equal(info.version, '0.3.0');
  assert.equal(info.downloadUrl, 'https://github.com/hwj123hwj/easyagent/releases/download/v0.3.0/EasyAgent-0.3.0-arm64.dmg');
  assert.equal(f.calls.length, 2);
  assert.equal(f.calls[0][1].headers['User-Agent'], 'EasyAgent-Desktop/0.2.0-rc.1');
  await f.checkForUpdate();
  assert.equal(f.calls.length, 2);
  f.advance(61000);
  await f.checkForUpdate();
  assert.equal(f.calls.length, 3);
  assert(!f.calls[2][0].includes('api.github.com'));
});
test('429 Retry-After postpones API calls beyond the success cache', async () => {
  const f = fixture(async url => url.includes('api.github.com')
    ? response(429, {}, { 'retry-after': '120' }) : response(200, manifest));
  await f.checkForUpdate(); f.advance(61000); await f.checkForUpdate();
  assert.equal(f.calls.filter(([url]) => url.includes('api.github.com')).length, 1);
  f.advance(61000); await f.checkForUpdate();
  assert.equal(f.calls.filter(([url]) => url.includes('api.github.com')).length, 2);
});
test('concurrent checks share the same request', async () => {
  let resolve;
  const f = fixture(() => new Promise(done => { resolve = done; }));
  const first = f.checkForUpdate(), second = f.checkForUpdate();
  assert.equal(first, second); assert.equal(f.calls.length, 1);
  resolve(response(200, []));
  assert.equal(await first, null);
});
test('working API keeps selection across independent core and desktop releases', async () => {
  const f = fixture(async () => response(200, [{ assets: [] }, { assets: [{
    name: 'EasyAgent-0.3.0-arm64.dmg',
    browser_download_url: 'https://github.com/hwj123hwj/easyagent/releases/download/v0.2.0/EasyAgent-0.3.0-arm64.dmg',
  }] }]));
  assert.equal((await f.checkForUpdate()).version, '0.3.0');
  assert.equal(f.calls.length, 1);
});
test('network failures also fall back, without leaking low-level errors', async () => {
  const f = fixture(async url => {
    if (url.includes('api.github.com')) throw Error('proxy private credentials');
    return response(200, manifest);
  });
  assert.equal((await f.checkForUpdate()).version, '0.3.0');
  const failed = fixture(async () => { throw Error('proxy private credentials'); });
  await assert.rejects(failed.checkForUpdate(), e => /发布页/.test(e.message) && !e.message.includes('private'));
});
test('failed or invalid fallback is an error, never a false up-to-date result', async () => {
  for (const fallback of [response(404, {}), response(200, {}), response(200, { ...manifest, assets: {} })]) {
    const f = fixture(async url => url.includes('api.github.com') ? response(403, {}) : fallback);
    await assert.rejects(f.checkForUpdate(), /更新服务/);
  }
});
test('manifest chooses native architecture, refuses prereleases and malformed digests', () => {
  const f = fixture(async () => response(200, []));
  assert(f.manifestUpdate(manifest, '0.2.0', 'x64').downloadUrl.endsWith('-x64.dmg'));
  assert.equal(f.manifestUpdate(manifest, '0.3.0', 'arm64'), null);
  assert.throws(() => f.manifestUpdate({ ...manifest, version: 'v0.4.0-rc.1' }, '0.2.0', 'arm64'), /格式/);
  assert.throws(() => f.manifestUpdate({ ...manifest, assets: { 'EasyAgent-0.4.0-arm64.dmg': 'bad' } }, '0.2.0', 'arm64'), /安装包/);
});
test('malformed asset URLs do not prevent later valid updates', () => {
  const f = fixture(async () => response(200, []));
  assert.equal(f.desktopUpdate([{ assets: [{ name: 'EasyAgent-0.4.0-arm64.dmg', browser_download_url: 'malformed' }] }], '0.2.0', 'arm64'), null);
});
