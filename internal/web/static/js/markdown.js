// Lightweight Markdown renderer — zero dependencies
// Handles: headings, bold, italic, code blocks, inline code, lists, links, blockquotes, tables, hr

const escapeHtml = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

function highlightCode(code) { return escapeHtml(code); }

export function renderMarkdown(text) {
  if (!text) return '';

  // Normalize line endings
  let src = text.replace(/\r\n/g, '\n');

  // Split into blocks
  const blocks = [];
  let i = 0;

  while (i < src.length) {
    // Code block (fenced)
    if (src.slice(i, i + 3) === '```') {
      const langMatch = src.slice(i + 3).match(/^(\S*)\n/);
      const lang = langMatch ? langMatch[1] : '';
      const start = i + 3 + (langMatch ? langMatch[0].length : 1);
      const end = src.indexOf('\n```', start);
      if (end !== -1) {
        const code = src.slice(start, end);
        blocks.push(`<pre><code class="lang-${escapeHtml(lang.replace(/[^a-zA-Z0-9_-]/g, ''))}">${highlightCode(code, lang.toLowerCase())}</code></pre>`);
        i = end + 4;
        continue;
      }
    }

    // Blockquote
    if (src[i] === '>' && (i === 0 || src[i - 1] === '\n')) {
      let line = '';
      let j = i + 1;
      if (src[j] === ' ') j++;
      while (j < src.length && src[j] !== '\n') {
        line += src[j];
        j++;
      }
      blocks.push(`<blockquote>${renderInline(line)}</blockquote>`);
      i = j + 1;
      continue;
    }

    // Heading
    const headingMatch = src.slice(i).match(/^(#{1,6})\s+(.+)(?:\n|$)/);
    if (headingMatch) {
      const level = headingMatch[1].length;
      blocks.push(`<h${level}>${renderInline(headingMatch[2])}</h${level}>`);
      i += headingMatch[0].length;
      continue;
    }

    // Horizontal rule
    if (src.slice(i).match(/^(-{3,}|\*{3,}|_{3,})\n/)) {
      blocks.push('<hr>');
      i += src.slice(i).indexOf('\n') + 1;
      continue;
    }

    // Unordered list
    if (src.slice(i).match(/^[\-\*]\s/)) {
      let items = '';
      while (i < src.length && src.slice(i).match(/^[\-\*]\s/)) {
        const lineEnd = src.indexOf('\n', i);
        const line = lineEnd === -1 ? src.slice(i + 2) : src.slice(i + 2, lineEnd);
        items += `<li>${renderInline(line)}</li>`;
        i = lineEnd === -1 ? src.length : lineEnd + 1;
      }
      blocks.push(`<ul>${items}</ul>`);
      continue;
    }

    // Ordered list
    if (src.slice(i).match(/^\d+\.\s/)) {
      let items = '';
      while (i < src.length && src.slice(i).match(/^\d+\.\s/)) {
        const lineEnd = src.indexOf('\n', i);
        const lineStart = src.indexOf(' ', i) + 1;
        const line = lineEnd === -1 ? src.slice(lineStart) : src.slice(lineStart, lineEnd);
        items += `<li>${renderInline(line)}</li>`;
        i = lineEnd === -1 ? src.length : lineEnd + 1;
      }
      blocks.push(`<ol>${items}</ol>`);
      continue;
    }

    // Table (simple detection)
    if (src.slice(i).match(/^\|.+\|/) && src.slice(i).match(/\n\|[-:|\s]+\|/)) {
      const tableEnd = src.indexOf('\n\n', i);
      const tableSrc = src.slice(i, tableEnd === -1 ? src.length : tableEnd);
      const rows = tableSrc.split('\n').filter(r => r.trim());
      let table = '<table>';
      rows.forEach((row, ri) => {
        if (row.match(/^\|[-:|\s]+\|$/)) return; // separator row
        const tag = ri === 0 ? 'th' : 'td';
        const cells = row.split('|').filter((_, ci, arr) => ci > 0 && ci < arr.length - 1);
        table += '<tr>' + cells.map(c => `<${tag}>${renderInline(c.trim())}</${tag}>`).join('') + '</tr>';
      });
      table += '</table>';
      blocks.push(table);
      i = tableEnd === -1 ? src.length : tableEnd + 2;
      continue;
    }

    // Paragraph
    const lineEnd = src.indexOf('\n', i);
    const line = lineEnd === -1 ? src.slice(i) : src.slice(i, lineEnd);
    if (line.trim()) {
      blocks.push(`<p>${renderInline(line)}</p>`);
    }
    i = lineEnd === -1 ? src.length : lineEnd + 1;
  }

  return blocks.join('\n');
}

function safeURL(raw, image = false) {
  try {
    const url = new URL(raw, 'https://local.invalid');
    if (!['http:', 'https:'].includes(url.protocol) && !(url.protocol === 'mailto:' && !image)) return null;
    return raw.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/'/g, '&#39;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  } catch { return null; }
}
function renderInline(text) {
  const format = value => escapeHtml(value)
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    .replace(/\*(.+?)\*/g, '<em>$1</em>')
    .replace(/~~(.+?)~~/g, '<del>$1</del>');
  const pattern = /`([^`]+)`|(!?)\[([^\]]*)\]\(([^)]+)\)|(https?:\/\/[^\s<>]+)/g;
  let output = '', last = 0;
  for (const match of text.matchAll(pattern)) {
    output += format(text.slice(last, match.index));
    const [raw, code, image, label, target, autoURL] = match;
    if (code !== undefined) output += '<code>' + escapeHtml(code) + '</code>';
    else {
      const url = safeURL(target || autoURL, !!image);
      if (!url) output += format(label || raw);
      else if (image) output += '<img src="' + url + '" alt="' + escapeHtml(label).replace(/"/g, '&quot;') + '" loading="lazy">';
      else output += '<a href="' + url + '" target="_blank" rel="noopener noreferrer">' + format(label || autoURL) + '</a>';
    }
    last = match.index + raw.length;
  }
  return output + format(text.slice(last));
}
