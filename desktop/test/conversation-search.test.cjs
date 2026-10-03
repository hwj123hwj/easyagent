const { test } = require('node:test');
const assert = require('node:assert/strict');
const { findConversation } = require('../.test-output/src/client/conversation-search.js');

test('find searches literal text, repeated matches and collapsed tool output', () => {
 const items = [{ id: 'a', kind: 'assistant', text: '正文 ERROR error' }, { id: 'b', kind: 'tool', title: 'bash', rawInput: { command: 'echo error' }, content: [{ text: '工具 error 结果' }], terminalOutput: '' }];
 const results = findConversation(items, 'error');
 assert.equal(results.length, 4);
 assert.deepEqual(results.map(item => item.itemId), ['a', 'a', 'b', 'b']);
 assert.equal(findConversation(items, '   ').length, 0);
 assert.equal(findConversation(items, '.*').length, 0);
 assert.equal(findConversation(items, '工具')[0].itemId, 'b');
});
