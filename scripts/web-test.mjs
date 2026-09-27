import { test } from 'node:test';
import assert from 'node:assert/strict';
import { renderMarkdown } from '../internal/web/static/js/markdown.js';

test('markdown safely renders links, inline code and fenced language', () => {
  const html = renderMarkdown('[危险](javascript:alert(1)) [文档](https://example.com/"onclick="bad) `**literal**`');
  assert.ok(!html.includes('href="javascript:'));
  assert.ok(html.includes('&quot;'));
  assert.ok(html.includes('<code>**literal**</code>'));
  assert.equal((renderMarkdown('[文档](https://example.com)').match(/<a /g) || []).length, 1);
  assert.ok(!renderMarkdown('```js"onmouseover="x\nalert(1)\n```').includes('onmouseover="'));
});
test('markdown renders final headings and preserves code exactly', () => {
  assert.equal(renderMarkdown('## 最后一行'), '<h2>最后一行</h2>');
  const html = renderMarkdown('```go\nfunc main() { fmt.Println("hi") }\n```');
  assert.ok(html.includes('func main() { fmt.Println("hi") }'));
  assert.ok(!html.includes('<span'));
});
test('markdown images are not transformed into links or executable attributes', () => {
  const html = renderMarkdown('![图" onload="bad](https://example.com/a.png)');
  assert.ok(html.includes('<img ')); assert.ok(!html.includes('<a '));
  assert.ok(!html.includes(' onload="bad'));
  assert.ok(!renderMarkdown('![x](data:text/html,bad)').includes('<img'));
});
