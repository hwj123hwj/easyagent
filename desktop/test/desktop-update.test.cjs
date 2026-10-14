const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const { releasePayload, download } = require('../scripts/desktop-update.cjs');
const { install } = require('../scripts/install-desktop-update.cjs');
const manifest = { version: 'v0.15.5', commit: 'a'.repeat(40), assets: { 'EasyAgent-0.15.5-arm64.dmg': 'b'.repeat(64) } };
test('update URL is derived from an exact version, architecture, SHA and digest', () => {
 assert.equal(releasePayload(manifest, '0.15.5', 'arm64').url, 'https://github.com/hwj123hwj/easyagent/releases/download/v0.15.5/EasyAgent-0.15.5-arm64.dmg');
 for (const [m,v,a] of [[manifest,'0.15.6','arm64'],[manifest,'0.15.5','x64'],[manifest,'../unsafe','arm64'],[{...manifest,commit:'bad'},'0.15.5','arm64'],[{...manifest,assets:{'EasyAgent-0.15.5-arm64.dmg':'bad'}},'0.15.5','arm64']]) assert.throws(()=>releasePayload(m,v,a));
});
test('streamed download refuses tampered or empty payloads before mounting or signing', async () => {
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'ea-download-'));
 try {
  const bytes=Buffer.from('verified installer');
  await download(new Response(bytes),path.join(dir,'valid'),crypto.createHash('sha256').update(bytes).digest('hex'));
  assert.deepEqual(fs.readFileSync(path.join(dir,'valid')),bytes);
  await assert.rejects(download(new Response(bytes),path.join(dir,'bad'),'b'.repeat(64)),/校验和/);
  await assert.rejects(download(new Response(''),path.join(dir,'empty'),crypto.createHash('sha256').update('').digest('hex')),/校验和/);
 } finally {fs.rmSync(dir,{recursive:true});}
});
function fixture() {
 const stage=fs.mkdtempSync(path.join(os.tmpdir(),'ea-install-'));const target=path.join(stage,'installed.app'),candidate=path.join(stage,'EasyAgent.app');
 fs.mkdirSync(target);fs.writeFileSync(path.join(target,'version'),'old');fs.mkdirSync(candidate);fs.writeFileSync(path.join(candidate,'version'),'new');
 return {stage,target,candidate,receipt:path.join(stage,'receipt.json'),resultFile:path.join(stage,'result.json'),parentPid:process.pid,version:'0.15.5',requirePermissions:true};
}
test('parent still running or a signature failure leaves the old installation untouched', async () => {
 for(const failure of ['parent','signature']) {
  const p=fixture();let launches=0;
  try {
   const result=await install(p,{waitForExit:async()=>{if(failure==='parent')throw Error('busy');},verify:()=>{if(failure==='signature')throw Error('invalid');},launch:()=>{launches++;}});
   assert.equal(result.ok,false);assert.equal(launches,0);assert.equal(fs.readFileSync(path.join(p.target,'version'),'utf8'),'old');
  }finally{fs.rmSync(p.stage,{recursive:true});}
 }
});
test('successful install waits for version, health and retained permission receipt; keeps rollback', async () => {
 const p=fixture();
 try {
  const result=await install(p,{waitForExit:async()=>{},verify:()=>{},launch:()=>{fs.writeFileSync(p.receipt,JSON.stringify({version:p.version,healthy:true,granted:true}));return {pid:process.pid};}});
  assert.equal(result.ok,true);assert.equal(fs.readFileSync(path.join(p.target,'version'),'utf8'),'new');assert.equal(fs.readFileSync(path.join(p.stage,'previous.app/version'),'utf8'),'old');
 }finally{fs.rmSync(p.stage,{recursive:true});}
});
test('failed startup or missing previously granted permissions rolls back to the original app', async () => {
 for(const granted of [false,undefined]) {
  const p=fixture();let launches=0,kills=0;
  try {
   const result=await install(p,{healthTimeout:1,waitForExit:async()=>{},verify:()=>{},launch:()=>{launches++;if(launches===1)fs.writeFileSync(p.receipt,JSON.stringify({version:p.version,healthy:granted===false,granted}));return {pid:process.pid,kill:()=>{kills++;}};}});
   assert.equal(result.ok,false);assert.equal(launches,2);assert.equal(kills,1);assert.equal(fs.readFileSync(path.join(p.target,'version'),'utf8'),'old');
  }finally{fs.rmSync(p.stage,{recursive:true});}
 }
});
test('an unusable fixed identity never downloads, re-signs ad hoc, or exposes a keychain error',async()=>{
 const Module=require('node:module'), filename=path.resolve(__dirname,'../scripts/desktop-update.cjs');
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'ea-update-identity-'));let fetches=0,signs=0;
 const m=new Module(filename,module);m.paths=module.paths;
 m.require=name=>name==='./macos-signing.cjs'?{configuration:env=>{assert.equal(env.EA_MAC_SIGNING_MODE,'auto');throw Error('private-password-in-command');},verify:()=>{throw Error('unexpected');}}:name==='./sign-adhoc.cjs'?async()=>{signs++;}:module.require(name);
 m._compile(fs.readFileSync(filename,'utf8'),filename);
 try {
  const updater=m.exports.createUpdater({target:'/Applications/EasyAgent.app',userData:dir,platform:'darwin',arch:'arm64',version:'0.15.4',fetch:async()=>{fetches++;}});
  assert.equal(updater.status().supported,false);assert.notEqual(updater.status().manualDownload,true);
  const state=await updater.prepare('0.15.5');assert.equal(state.phase,'error');assert(!state.error.includes('private-password'));assert.equal(fetches,0);assert.equal(signs,0);
 }finally{fs.rmSync(dir,{recursive:true});}
});
