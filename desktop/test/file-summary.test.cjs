const { test } = require('node:test');
const assert = require('node:assert/strict');
const { aggregateRunFiles, workspaceFileRuns } = require('../.test-output/src/client/file-summary.js');
const { fileDiff } = require('../.test-output/src/client/file-review.js');
const run = (id, before, after, extra = {}) => ({ run_id: id, workspace: '/project', complete: true, files: [{ path: 'a.txt', kind: 'modified', attribution: 'file_tool', before, after, binary: false, ...extra }] });

test('same-file repeated edits use first before and final after rather than accumulated diffs', () => {
 const result = aggregateRunFiles([run('one', 'a\n', 'b\n'), run('two', 'b\n', 'c\n')]);
 assert.equal(result.length, 1);
 assert.deepEqual(result[0].runs, ['one', 'two']);
 assert.equal(result[0].latestRunId, 'two');
 assert.equal(result[0].file.before, 'a\n');
 assert.equal(result[0].file.after, 'c\n');
 assert.equal(fileDiff(result[0].file).additions, 1);
 assert.equal(fileDiff(result[0].file).deletions, 1);
 const restored = aggregateRunFiles([run('one', 'a\n', 'b\n'), run('two', 'b\n', 'a\n')]);
 assert.equal(fileDiff(restored[0].file).additions, 0);
 assert.equal(fileDiff(restored[0].file).deletions, 0);
});

test('scope is the current workspace, with a bounded history and isolated equal relative paths', () => {
 const runs = Array.from({length: 23}, (_,i) => run(String(i), 'a', 'b'));
 runs.push({...run('foreign', 'a', 'b'), workspace: '/project-other'});
 runs.push({...run('unfinished', 'a', 'b'), complete: false});
 assert.deepEqual(workspaceFileRuns(runs, '/project/').map(r => r.run_id), Array.from({length:20},(_,i)=>String(i+3)));
 assert.equal(aggregateRunFiles([runs[0], runs[23]]).length, 2);
});

test('external changes and discontinuous histories never become confirmed tool net stats', () => {
 const result = aggregateRunFiles([run('one', 'a\n', 'b\n'), run('external', 'b\n', 'c\n', {attribution:'observed'})]);
 assert.equal(result[0].file.attribution, 'observed');
 assert.equal(fileDiff(result[0].file), undefined);
 const gap = aggregateRunFiles([run('one', 'a\n', 'b\n'), run('two', 'user\n', 'c\n')]);
 assert.equal(fileDiff(gap[0].file), undefined);
});

test('legacy truncated summary phantom additions and deletions are excluded before counting', () => {
 const r = run('old-server', 'a', 'b');
 r.skipped = ['扫描达到上限'];
 r.files.push({...r.files[0], path:'phantom.tmp', kind:'created', attribution:'observed'});
 r.files.push({...r.files[0], path:'phantom-deleted.txt', kind:'deleted'});
 const result = workspaceFileRuns([r], '/project');
 assert.equal(result[0].incomplete, true);
 assert.deepEqual(result[0].files.map(f => f.path), ['a.txt']);
 assert.equal(r.files.length, 3, 'server results remain immutable');
});
