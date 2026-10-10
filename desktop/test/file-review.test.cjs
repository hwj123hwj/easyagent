const { test } = require('node:test');
const assert = require('node:assert/strict');
const { fileDiff, summarizeDiffs } = require('../.test-output/src/client/file-review.js');
const file = (before, after, extra = {}) => ({path:'test.txt', before, after, kind:'modified', binary:false, ...extra});
test('diff counts replacements, preserves whitespace and maps both line numbers', () => {
 const d = fileDiff(file('first\nold\nlast\n', 'first\n new\nlast\n'));
 assert.equal(d.additions,1); assert.equal(d.deletions,1);
 assert.deepEqual(d.lines.find(l=>l.kind==='removed'), {kind:'removed',text:'old',before:2});
 assert.deepEqual(d.lines.find(l=>l.kind==='added'), {kind:'added',text:' new',after:2});
 assert.deepEqual(d.lines.find(l=>l.text==='last'), {kind:'context',text:'last',before:3,after:3});
});
test('created, deleted, empty and missing final newline are distinct', () => {
 assert.equal(fileDiff(file('', 'a\nb', {kind:'created'})).additions,2);
 assert.equal(fileDiff(file('a\nb\n', '', {kind:'deleted'})).deletions,2);
 assert.deepEqual(fileDiff(file('', '')).lines,[]);
 const d=fileDiff(file('a\n', 'a'));
 assert.equal(d.additions,1);assert.equal(d.deletions,1);
 assert.ok(d.lines.some(l=>l.kind==='note'));
});
test('binary, absent snapshot, oversized and excessive edit distance never produce fake zero stats', () => {
 assert.equal(fileDiff(file('a', 'b', {binary:true})),undefined);
 assert.equal(fileDiff(file(undefined,undefined)),undefined);
 assert.equal(fileDiff(file('', 'x'.repeat(400001))),undefined);
 assert.equal(fileDiff(file('', Array(2002).fill('x').join('\n'))),undefined);
 const summary = summarizeDiffs([file('a\n','b\n'),file('a','b',{path:'blob',binary:true})]);
 assert.equal(summary.partial,true);assert.equal(summary.additions,1);assert.equal(summary.deletions,1);
});
test('render clipping does not truncate aggregate line counts', () => {
 const d=fileDiff(file('', Array(900).fill('line').join('\n')));
 assert.equal(d.additions,900);assert.equal(d.lines.length,800);assert.equal(d.truncated,true);
});
