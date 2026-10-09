const {test} = require("node:test");
const assert = require("node:assert/strict");
global.localStorage={getItem:()=>null,setItem:()=>{}};
global.sessionStorage={getItem:()=>null,setItem:()=>{}};
global.window={};
const client=require("../.test-output/src/client/skill-market.js");
const store=require("../.test-output/src/store.js");

test("desktop job tracking uses authenticated IPC, maps polling fields, and never fetches a token-free stream",async()=>{
 let calls=0; const requests=[]; let cancel;
 global.window.piAPI={request:async(method,path)=>{
   requests.push([method,path]); calls++;
   return {id:"job",skill_id:42,state:calls===1?"running":"succeeded",skill_name:"demo"};
 }};
 const oldFetch=global.fetch; global.fetch=()=>{throw Error("renderer must use IPC")};
 try {
  const events=await new Promise((resolve,reject)=>{
   const events=[];
   cancel=client.subscribeInstallJob("job",{onEvent:e=>{events.push(e);if(e.state==="succeeded")resolve(events)},onError:reject});
  });
  assert.equal(events.length,2); assert.equal(events[1].job_id,"job"); assert.equal(events[1].name,"demo");
  assert.deepEqual(requests,[["GET","/skills/market/jobs/job"],["GET","/skills/market/jobs/job"]]);
 } finally { cancel?.(); global.window.piAPI=undefined; global.fetch=oldFetch; }
});

test("browser stream EOF recovers authoritative completion via polling",async()=>{
 global.window={}; store.setBaseUrl("http://qa");
 const oldFetch=global.fetch; const requests=[]; let cancel;
 global.fetch=async url=>{
  requests.push(url);
  if(url.endsWith("/events")) return new Response(new ReadableStream({start(c){c.close()}}));
  return new Response(JSON.stringify({id:"j",skill_id:1,state:"succeeded",skill_name:"done"}),{headers:{"Content-Type":"application/json"}});
 };
 try {
  const evt=await new Promise((resolve,reject)=>{cancel=client.subscribeInstallJob("j",{onEvent:resolve,onError:reject})});
  assert.equal(evt.state,"succeeded"); assert.equal(evt.name,"done"); assert.equal(requests.length,2);
 } finally {cancel?.();global.fetch=oldFetch;}
});

test("search forwards page, sort and section to the server",async()=>{
 let request;
 global.window.piAPI={request:async(method,path)=>{request=path;return{skills:[],total:0,page:3}}};
 try {await client.marketSearch({section:8,sort:"name",page:3,q:"a b"});assert.equal(request,"/skills/market/search?q=a+b&sort=name&page=3&section=8");}
 finally {global.window.piAPI=undefined;}
});
