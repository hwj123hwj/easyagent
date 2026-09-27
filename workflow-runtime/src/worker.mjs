import { createInterface } from 'node:readline';
import { createHash } from 'node:crypto';
import { analyzeWorkflowScript,createWorkflowProgram,collectSites,synthesizeAskSchemas,buildAskSpecs,lowerWorkflow,validate,refToString,WorkflowError,FACADE_DTS } from '@zcode/dynamic-workflow';
import { runWorkflowScript } from '@zcode/dynamic-workflow-runtime';
import { openJournal } from './journal.mjs';

if (process.argv.includes('--check')) { console.log(JSON.stringify({engine:'zcode',source:'ba61ca16e1790878c49566fc2f3cb6e908a4ea77'})); process.exit(0); }
if (process.argv.includes('--facade')) { console.log(FACADE_DTS); process.exit(0); }
const input=createInterface({input:process.stdin,crlfDelay:Infinity});
const pending=new Map(), abort=new AbortController(); let seq=0, started=false;
function send(value) { process.stdout.write(JSON.stringify(value)+'\n'); }
function rpc(op,args) {
  const id=String(++seq);
  return new Promise((resolve,reject)=>{pending.set(id,{resolve,reject});send({type:'rpc',id,op,args});});
}
input.on('line',line=>{
  try {
    const message=JSON.parse(line);
    if (message.type==='start' && !started) { started=true; run(message).catch(error=>{send({type:'fatal',error:error.message});process.exitCode=1;input.close();}); }
    else if (message.type==='cancel') abort.abort();
    else if (message.type==='response') {
      const promise=pending.get(message.id);if (!promise)return;pending.delete(message.id);
      if(message.error)promise.reject(Error(message.error));else promise.resolve(message.result);
    }
  } catch(error) { send({type:'fatal',error:error.message});abort.abort();input.close(); }
});
input.on('close',()=>{abort.abort();for(const p of pending.values())p.reject(Error('host disconnected'));pending.clear();});

async function run(request) {
  const {script,run_id:runId,name,directory}=request;
  if(typeof script!=='string'||Buffer.byteLength(script)>64*1024)throw Error('script must be at most 64 KiB');
  const analysis=analyzeWorkflowScript(script);
  if(!analysis.ok)throw Error(analysis.diagnostics.map(d=>`L${d.line}:${d.column} ${d.message}`).join('\n'));
  const program=createWorkflowProgram(script),table=collectSites(program);
  const {schemas,diagnostics}=synthesizeAskSchemas(program,table);
  if(diagnostics.length)throw Error(diagnostics.map(d=>d.message).join('\n'));
  const journal=openJournal(directory+'/journal.jsonl');
  const snapshot=()=>send({type:'snapshot',state:{run:journal.port.getRun(runId),actors:journal.port.listActors(runId),nodes:journal.port.listNodes(runId),events:journal.port.listEvents(runId).slice(-100)}});
  const asks=new Map();
  try {
    const result=await runWorkflowScript({
      scriptText:script,lowered:lowerWorkflow(program,table).code,runId,name,
      scriptHash:createHash('sha256').update(script).digest('hex'),
      caps:{maxConcurrency:4},askSpecs:buildAskSpecs(table,schemas),validate,
      cwd:directory,timeoutMs:30*60*1000,maxOldSpaceSizeMb:128,signal:abort.signal,
      makeDriver:sink=>{
        async function turn(ref,entry,instructions) {
          try {
            const reply=await rpc('ask',{session_id:entry.session.id,key:refToString(ref),instructions,typed:entry.message.typed,schema:entry.message.schema});
            if(asks.get(refToString(ref))!==entry)return;
            if(entry.message.typed) {
              let text=reply.text.trim().replace(/^```(?:json)?\s*\n/,'').replace(/\n```$/,'');
              let value;try {value=JSON.parse(text);}catch {value=text;}
              sink.askSubmitAttempted(ref,value);
            } else sink.askTurnEnded(ref,reply.text);
          } catch(error) {if(asks.get(refToString(ref))===entry&&!abort.signal.aborted)sink.stopRun(new WorkflowError('ProviderStop',error.message));}
        }
        return {
          journal:journal.port,emit:snapshot,
          createActorSession:async(actor,persona,seed)=>{
            if(seed)throw Error('amended-script resume is not supported by this adapter');
            return rpc('actor',{key:refToString(actor),persona});
          },
          startAsk(session,ref,message){const entry={session,message};asks.set(refToString(ref),entry);void turn(ref,entry,message.instructions);},
          respondToSubmit(ref,verdict){
            const entry=asks.get(refToString(ref));if(!entry)return;
            if(verdict.kind==='accept'){asks.delete(refToString(ref));return;}
            void turn(ref,entry,verdict.kind==='reject'?'Return corrected JSON only. Validation errors: '+JSON.stringify(verdict.violations):'Complete the assigned task and return its result.');
          },
          cancelAsk(ref){asks.delete(refToString(ref));send({type:'cancel_ask',key:refToString(ref)});},
          async executeWorldRead(){throw Error('Direct world APIs are unavailable; use an actor with EasyAgent tools.');},
        };
      },
    });
    snapshot();send({type:'settled',result});
  } finally {journal.close();input.close();}
}
