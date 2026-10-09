const {test} = require('node:test');
const assert = require('node:assert/strict');
const {parseQuestions, answersComplete} = require('../.test-output/src/client/questions.js');
const {emptyProjection, reduceEnvelope} = require('../.test-output/src/client/reducer.js');
const args = {questions:[{question:'怎么接入？',header:'接入',options:[{label:'官方 API'},{label:'Git'}]}]};
test('questions require an explicit valid choice or custom reply, never select a default', () => {
 const q = parseQuestions(args);
 assert.equal(answersComplete(q,[{selected:[]}]), false);
 assert.equal(answersComplete(q,[{selected:['官方 API']}]),true);
 assert.equal(answersComplete(q,[{selected:[],text:' 自己部署 '}]),true);
 for(const a of [[],[{selected:[],text:'  '}],[{selected:['unknown']}],[{selected:['Git','Git']}],[{selected:['Git','官方 API']}],[{selected:['Git'],text:'custom'}]])
  assert.equal(answersComplete(q,a),false);
 q[0] = {...q[0], multiSelect:true};
 assert.equal(answersComplete(q,[{selected:['Git','官方 API'],text:'custom'}]),true);
});
test('malformed question payloads fail closed', () => {
 for (const a of [null,{}, {questions:[]},{questions:[{question:'Q',options:['A','B']}]},{questions:[{question:'Q',options:[{label:'A'},{label:'B',preview:42}]}]}])
  assert.deepEqual(parseQuestions(a),[]);
});
test('reconnect restores actionable questions with options and keeps tool in progress until reply',()=>{
 const confirmation = {confirmation_id:'ask-1',tool_call_id:'tool-1',tool_name:'ask_user_question',args,description:'ask'};
 let view=reduceEnvelope(emptyProjection(), {type:'snapshot',seq:2,run:{run_id:'r',state:'waiting_confirmation'},messages:[],events:[{type:'tool_start',tool_call_id:'tool-1',tool_name:'ask_user_question'}],pending_confirmations:[confirmation]});
 assert.equal(view.phase,'approval');
 assert.deepEqual(parseQuestions(view.confirmations[0].args),args.questions);
 assert.equal(view.transcript.at(-1).status,'in_progress');
 view=reduceEnvelope(view,{type:'confirmed',run_id:'r',confirmation_id:'ask-1',seq:3});
 assert.equal(view.confirmations.length,0);
});
