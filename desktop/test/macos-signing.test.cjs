const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const Module = require('node:module');
const signing = require('../scripts/macos-signing.cjs');

function fixture() {
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'ea-signing-unit-'));
 const config={version:1,identity:'a'.repeat(40),keychain:path.join(root,'signing.keychain-db'),passwordFile:path.join(root,'password')};
 for(const file of [config.keychain,config.passwordFile]) fs.writeFileSync(file,'test-only',{mode:0o600});
 const filename=path.join(root,'identity.json');fs.writeFileSync(filename,JSON.stringify(config),{mode:0o600});
 return {root,config,filename};
}
test('configured signing identity never silently downgrades on missing, malformed or public files',()=>{
 const f=fixture();try {
  assert.equal(signing.configuration({EA_MAC_SIGNING_MODE:'adhoc',EA_MAC_SIGNING_CONFIG:f.filename}).mode,'adhoc');
  assert.equal(signing.configuration({EA_MAC_SIGNING_CONFIG:f.filename}).identity,'A'.repeat(40));
  assert.throws(()=>signing.configuration({EA_MAC_SIGNING_CONFIG:f.filename+'missing'}),/missing/);
  assert.throws(()=>signing.configuration({EA_MAC_SIGNING_MODE:'typo'}),/Invalid signing mode/);
  fs.chmodSync(f.config.passwordFile,0o644);assert.throws(()=>signing.configuration({EA_MAC_SIGNING_CONFIG:f.filename}),/private/);
  fs.chmodSync(f.config.passwordFile,0o600);fs.writeFileSync(f.filename,'{}');assert.throws(()=>signing.configuration({EA_MAC_SIGNING_CONFIG:f.filename}),/Invalid/);
 }finally{fs.rmSync(f.root,{recursive:true});}
});
test('designated requirement pins both certificate and bundle identity, with no version or code hash',()=>{
 const config={identity:'A'.repeat(40)};
 assert.equal(signing.requirement('com.easyagent.desktop',config),'identifier "com.easyagent.desktop" and certificate leaf = H"'+'A'.repeat(40)+'"');
 assert.throws(()=>signing.requirement('com.easyagent" or true',config),/Invalid/);
});
test('failed signing restores the original user keychain list and does not print credential-bearing errors',async()=>{
 const f=fixture(),calls=[];
 const filename=path.resolve(__dirname,'../scripts/macos-signing.cjs');const m=new Module(filename,module);m.paths=module.paths;
 m.require=name=>name==='node:child_process'?{execFileSync:(cmd,args)=>{calls.push({cmd,args});if(args[0]==='list-keychains'&&args.length===3)return '    "/original/login.keychain-db"\n';}}:module.require(name);
 m._compile(fs.readFileSync(filename,'utf8'),filename);
 try {
  await assert.rejects(m.exports.withKeychain({...f.config,mode:'local_certificate'},async()=>{throw Error('sign failed')}),/sign failed/);
  assert.deepEqual(calls.at(-1).args,['list-keychains','-d','user','-s','/original/login.keychain-db']);
 }finally{fs.rmSync(f.root,{recursive:true});}
});
test('automatic signing and manual builds cannot modify the keychain search list concurrently',async()=>{
 const f=fixture(), filename=path.resolve(__dirname,'../scripts/macos-signing.cjs'), m=new Module(filename,module);m.paths=module.paths;
 m.require=name=>name==='node:child_process'?{execFileSync:(_cmd,args)=>args[0]==='list-keychains'&&args.length===3?'    "/original/login.keychain-db"\n':undefined}:module.require(name);
 m._compile(fs.readFileSync(filename,'utf8'),filename);
 let finish;
 try {
  const first=m.exports.withKeychain({...f.config,mode:'local_certificate'},()=>new Promise(resolve=>{finish=resolve;}));
  await assert.rejects(m.exports.withKeychain({...f.config,mode:'local_certificate'},async()=>{}),/already in progress/);
  finish();await first;assert(!fs.existsSync(path.join(f.root,'.codesign-lock')));
 }finally{fs.rmSync(f.root,{recursive:true});}
});
test('a crashed signer removes only its introduced search item before taking the lock again',async()=>{
 const f=fixture(),filename=path.resolve(__dirname,'../scripts/macos-signing.cjs'),m=new Module(filename,module);m.paths=module.paths;
 const lock=path.join(f.root,'.codesign-lock');fs.mkdirSync(lock,{mode:0o700});fs.writeFileSync(path.join(lock,'pid'),'999999999');fs.writeFileSync(path.join(lock,'original.json'),JSON.stringify(['/original/login.keychain-db']));
 let list=['/original/login.keychain-db',f.config.keychain,'/unrelated/new.keychain-db'];
 m.require=name=>name==='node:child_process'?{execFileSync:(_cmd,args)=>{if(args[0]==='list-keychains'){if(args.length===3)return list.map(x=>JSON.stringify(x)).join('\n');list=args.slice(4);}}}:module.require(name);
 m._compile(fs.readFileSync(filename,'utf8'),filename);
 try {
  await m.exports.withKeychain({...f.config,mode:'local_certificate'},async()=>{});
  assert.deepEqual(list,['/original/login.keychain-db','/unrelated/new.keychain-db']);assert(!fs.existsSync(lock));
 }finally{fs.rmSync(f.root,{recursive:true});}
});
