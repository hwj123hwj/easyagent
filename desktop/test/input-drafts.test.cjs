const {test}=require('node:test'),assert=require('node:assert/strict');
const {sanitizeInputDrafts}=require('../.test-output/src/client/input-drafts.js');
test('corrupt or mixed draft storage keeps only valid scoped context',()=>{
 assert.deepEqual(sanitizeInputDrafts(null),{});
 const actual=sanitizeInputDrafts({mac:{a:{attachments:[null,{id:'bad'}],files:[{path:'/project/a',workspace:'/project'},false]},b:'invalid'},mini:null});
 assert.deepEqual(actual,{mac:{a:{attachments:[],files:[{path:'/project/a',workspace:'/project'}]}}});
});
