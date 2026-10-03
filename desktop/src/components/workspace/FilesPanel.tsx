/**
 * FilesPanel.tsx — project explorer and file preview for the right workspace.
 * Features: lazy-loading explorer tree, multiple open-file tabs, breadcrumb,
 * per-extension icons, Markdown preview, code highlighting with line numbers,
 * fuzzy file search, and "Open in" menu (reveal in folder / open in terminal).
 */

import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from 'react';
import fuzzysort from 'fuzzysort';
import { useStore, apiRequest } from '../../store';
import { Icon } from '../Icon';
import { copyText } from '../../client/clipboard';
import { FileIcon } from './FileIcons';
import { Markdown } from '../Markdown';
import { highlightCode } from './codeHighlight';
import { useT, type TFunc } from '../../i18n/useT';

/** DirEntry shape returned by the /workspace/list-dir endpoint. */
interface DirEntry {
  name: string;
  path: string;
  isDir: boolean;
}

const IMAGE_EXT = /\.(png|jpe?g|gif|webp|bmp|svg)$/i;
const MARKDOWN_EXT = /\.(md|markdown)$/i;

/** The last path segment, tolerating both separators. */
function baseName(p: string): string {
  const parts = p.replace(/[\\/]+$/, '').split(/[\\/]/);
  return parts[parts.length - 1] || p;
}

/** Breadcrumb segments of `file` relative to `root`. */
function breadcrumb(root: string, file: string): string[] {
  const norm = (s: string) => s.replace(/\\/g, '/').replace(/\/+$/, '');
  const r = norm(root);
  const f = norm(file);
  const rel = f.startsWith(r + '/') ? f.slice(r.length + 1) : baseName(f);
  return [baseName(r), ...rel.split('/').filter(Boolean)];
}

// ── REST API helpers (talk to easyagent backend) ────────────────────────────────

async function listDir(path:string):Promise<DirEntry[]> { return apiRequest('GET', '/workspace/list-dir?path='+encodeURIComponent(path)); }
async function searchFiles(root:string):Promise<string[]> { return apiRequest('GET', '/workspace/search-files?path='+encodeURIComponent(root)); }
async function readFileText(path:string):Promise<string> { const value=await apiRequest<{content:string}>('GET','/workspace/read-file?path='+encodeURIComponent(path)); return value.content; }
async function readFileBase64(path:string):Promise<{data:string;mimeType:string}|null> { return apiRequest('GET','/workspace/read-file-base64?path='+encodeURIComponent(path)); }
async function writeFileText(path:string,content:string):Promise<boolean> { await apiRequest('PUT','/workspace/write-file?path='+encodeURIComponent(path),{content}); return true; }

export function FilesPanel() {
  const activeId = useStore((s) => s.activeSessionId);
  const meta = useStore((s) => (activeId ? s.sessions[activeId]?.meta : undefined));
  const tabs = useStore((s) => s.workspace.fileTabs);
  const activeTab = useStore((s) => s.workspace.activeFileTab);
  const openFileTab = useStore((s) => s.openFileTab);
  const closeFileTab = useStore((s) => s.closeFileTab);
  const [showTree, setShowTree] = useState(false);
  const t = useT();

  const root = meta?.cwd;
  const browsing = !activeTab || showTree;
  useEffect(() => setShowTree(false), [root]);
  const openFile = (path: string) => {
    openFileTab(path);
    setShowTree(false);
  };

  if (!root) {
    return (
      <div className="ws-panel">
        <div className="ws-panel-head">
          <Icon name="folder" size={15} />
          <span>{t('files.title')}</span>
        </div>
        <div className="empty">{t('files.noProject')}</div>
      </div>
    );
  }

  return (
    <div className="ws-panel files-panel">
      <div className="files-topbar">
        <button
          className={`files-tree-toggle ${browsing ? 'active' : ''}`}
          aria-label={browsing && activeTab ? '返回文件预览' : '浏览项目目录'}
          title={browsing && activeTab ? '返回文件预览' : '浏览项目目录'}
          aria-pressed={browsing}
          onClick={() => setShowTree((value) => !value)}
          disabled={!activeTab}
        >
          <Icon name={browsing && activeTab ? 'arrow-left' : 'folder'} size={14} />
          <span>{browsing && activeTab ? '返回文件' : '目录'}</span>
        </button>
        <FileTabs
          tabs={tabs}
          activeTab={activeTab}
          onSelect={openFile}
          onClose={closeFileTab}
          t={t}
        />
      </div>
      <div className="files-body">
        <div className="files-main" hidden={browsing}>
          {activeTab && (
            <>
              <FileToolbar root={root} file={activeTab} t={t} />
              <FileContent path={activeTab} t={t} />
            </>
          )}
        </div>
        <div className="files-explorer" hidden={!browsing}>
          <FileTree key={root} root={root} activeTab={activeTab} onOpen={openFile} />
        </div>
      </div>
    </div>
  );
}

// ── tab bar ──

function FileTabs({
  tabs,
  activeTab,
  onSelect,
  onClose,
  t,
}: {
  tabs: string[];
  activeTab?: string;
  onSelect: (p: string) => void;
  onClose: (p: string) => void;
  t: TFunc;
}) {
  if (tabs.length === 0) {
    return (
      <div className="file-tabs">
        <span className="file-tabs-empty">
          <Icon name="folder" size={14} />
          {t('files.title')}
        </span>
      </div>
    );
  }
  return (
    <div className="file-tabs">
      {tabs.map((p) => (
        <div
          key={p}
          className={`file-tab ${activeTab === p ? 'active' : ''}`}
          title={p}
        >
          <button className="file-tab-select" onClick={() => onSelect(p)} aria-pressed={activeTab === p}>
            <FileIcon name={baseName(p)} size={14} />
            <span className="file-tab-name">{baseName(p)}</span>
          </button>
          <button
            className="file-tab-close"
            title={t('files.closeTab')}
            aria-label={`${t('files.closeTab')} · ${baseName(p)}`}
            onClick={(e) => {
              e.stopPropagation();
              onClose(p);
            }}
          >
            <Icon name="x" size={12} />
          </button>
        </div>
      ))}
    </div>
  );
}

// ── breadcrumb + "Open in" toolbar ──

function FileToolbar({ root, file, t }: { root: string; file: string; t: TFunc }) {
  const local = useStore(s=>s.profiles.find(p=>p.id===s.selectedProfile)?.kind==='local');
  const crumbs = useMemo(() => breadcrumb(root, file), [root, file]);
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!menuOpen) return;
    const onDown = (e: MouseEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) setMenuOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setMenuOpen(false);
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [menuOpen]);

  const dir = file.replace(/[\\/][^\\/]*$/, '') || file;

  return (
    <div className="file-toolbar">
      <div className="file-breadcrumb" title={file}>
        {crumbs.map((c, i) => (
          <span key={i} className="crumb">
            {i > 0 && <Icon name="chevron-right" size={12} />}
            <span className={i === crumbs.length - 1 ? 'crumb-leaf' : ''}>{c}</span>
          </span>
        ))}
      </div>
      <span className="grow" />
      <div ref={menuRef} style={{ position: 'relative' }}>
        <button
          className="chip interactive file-open-in"
          disabled={!local}
          title={local?t('files.openIn'):'路径属于远程主机，无法在本机打开'}
          onClick={() => setMenuOpen((o) => !o)}
        >
          <Icon name="external-link" size={13} />
          <span className="chip-label">{t('files.openIn')}</span>
          <Icon name="chevron-down" size={12} className="chip-caret" />
        </button>
        {menuOpen && (
          <div className="menu-pop" style={{ right: 0, top: '120%' }}>
            <button
              onClick={() => {
                void window.piAPI?.revealInFolder(file);
                setMenuOpen(false);
              }}
            >
              <Icon name="folder-open" size={14} />
              {t('files.openInFolder')}
            </button>
            <button
              onClick={() => {
                void window.piAPI?.openInTerminal(dir);
                setMenuOpen(false);
              }}
            >
              <Icon name="terminal" size={14} />
              {t('files.openInTerminal')}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

// ── explorer tree ──

function joinRoot(root: string, rel: string): string {
  return `${root.replace(/[\\/]+$/, '')}/${rel}`;
}

function FileTree({
  root,
  activeTab,
  onOpen,
}: {
  root: string;
  activeTab?: string;
  onOpen: (p: string) => void;
}) {
  const t = useT();
  const [query, setQuery] = useState('');
  const [files, setFiles] = useState<string[] | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    setFiles(null);
    setQuery('');
    setLoading(false);
  }, [root]);

  const searching = query.trim().length > 0;
  // Loading entries when filters change
  useEffect(() => {
    if (!searching || files !== null) return;
    let alive = true;
    setLoading(true);
    void searchFiles(root)
      .then((list) => alive && setFiles(list))
      .catch(() => alive && setFiles([]))
      .finally(() => alive && setLoading(false));
    return () => { alive = false; };
  }, [searching, files, root]);

  const results = useMemo(() => {
    if (!searching || !files) return [];
    return fuzzysort.go(query.trim(), files, { limit: 80 }).map((r) => r.target);
  }, [query, files, searching]);

  return (
    <div className="file-tree">
      <div className="file-tree-head">
        <Icon name="folder" size={13} />
        <span title={root}>{baseName(root)}</span>
      </div>
      <div className="file-search">
        <Icon name="search" size={13} />
        <input
          className="file-search-input"
          placeholder={t('files.searchPlaceholder')}
          aria-label={t('files.searchPlaceholder')}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          spellCheck={false}
        />
        {query && (
          <button className="file-search-clear" title={t('files.searchClear')} onClick={() => setQuery('')}>
            <Icon name="x" size={12} />
          </button>
        )}
      </div>
      <div className="file-tree-body">
        {searching ? (
          loading && !files ? (
            <div className="tree-row tree-loading" style={{ paddingLeft: 10 }}>
              <Icon name="loader" size={13} spin />
            </div>
          ) : results.length === 0 ? (
            <div className="empty file-search-empty">{t('files.searchNoResults')}</div>
          ) : (
            results.map((rel) => {
              const abs = joinRoot(root, rel);
              const slash = rel.lastIndexOf('/');
              const fname = slash < 0 ? rel : rel.slice(slash + 1);
              const dirPart = slash < 0 ? '' : rel.slice(0, slash);
              return (
                <button
                  key={rel}
                  className={`tree-row search-row ${activeTab === abs ? 'active' : ''}`}
                  style={{ paddingLeft: 10 }}
                  onClick={() => onOpen(abs)}
                  title={rel}
                >
                  <FileIcon name={fname} size={15} />
                  <span className="search-name">{fname}</span>
                  {dirPart && <span className="search-dir">{dirPart}</span>}
                </button>
              );
            })
          )
        ) : (
          <TreeNode path={root} name={baseName(root)} isDir depth={0} activeTab={activeTab} onOpen={onOpen} defaultOpen />
        )}
      </div>
    </div>
  );
}

function TreeNode({
  path,
  name,
  isDir,
  depth,
  activeTab,
  onOpen,
  defaultOpen,
}: {
  path: string;
  name: string;
  isDir: boolean;
  depth: number;
  activeTab?: string;
  onOpen: (p: string) => void;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(!!defaultOpen);
  const [children, setChildren] = useState<DirEntry[] | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!isDir || !open || children) return;
    let alive = true;
    setLoading(true);
    void listDir(path)
      .then((entries) => alive && setChildren(entries))
      .catch(() => alive && setChildren([]))
      .finally(() => alive && setLoading(false));
    return () => { alive = false; };
  }, [isDir, open, children, path]);

  const active = !isDir && activeTab === path;
  const indent = 8 + depth * 13;

  if (!isDir) {
    return (
      <button
        className={`tree-row ${active ? 'active' : ''}`}
        style={{ paddingLeft: indent }}
        onClick={() => onOpen(path)}
        title={path}
      >
        <FileIcon name={name} size={15} />
        <span className="tree-name">{name}</span>
      </button>
    );
  }

  return (
    <>
      <button
        className="tree-row"
        style={{ paddingLeft: indent }}
        onClick={() => setOpen((o) => !o)}
        title={path}
      >
        <Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} />
        <FileIcon name={name} isDir open={open} size={15} />
        <span className="tree-name">{name}</span>
      </button>
      {open &&
        (loading && !children ? (
          <div className="tree-row tree-loading" style={{ paddingLeft: indent + 19 }}>
            <Icon name="loader" size={13} spin />
          </div>
        ) : (
          (children ?? []).map((c) => (
            <TreeNode
              key={c.path}
              path={c.path}
              name={c.name}
              isDir={c.isDir}
              depth={depth + 1}
              activeTab={activeTab}
              onOpen={onOpen}
            />
          ))
        ))}
    </>
  );
}

// ── content viewer ──

type Loaded =
  | { kind: 'text'; text: string }
  | { kind: 'image'; url: string }
  | { kind: 'binary' }
  | { kind: 'error' };

function FileContent({ path, t }: { path: string; t: TFunc }) {
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const isMarkdown = MARKDOWN_EXT.test(path);
  const isImage = IMAGE_EXT.test(path);
  const [preview, setPreview] = useState(true);
  const [editing, setEditing] = useState(false);
  const [editContent, setEditContent] = useState('');
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setLoaded(null);
    setPreview(true);
    setEditing(false);
    setDirty(false);
    let alive = true;
    if (isImage) {
      void readFileBase64(path)
        .then((b64) =>
          alive && setLoaded(b64 ? { kind: 'image', url: `data:${b64.mimeType};base64,${b64.data}` } : { kind: 'binary' }),
        )
        .catch(() => alive && setLoaded({ kind: 'binary' }));
      return () => {
        alive = false;
      };
    }
    void readFileText(path)
      .then((text) => {
        if (!alive) return;
        setLoaded(text.includes(String.fromCharCode(0)) ? { kind: 'binary' } : { kind: 'text', text });
      })
      .catch(() => alive && setLoaded({ kind: 'error' }));
    return () => {
      alive = false;
    };
  }, [path, isImage]);

  const handleStartEdit = () => {
    if (loaded?.kind === 'text') {
      setEditContent(loaded.text);
      setEditing(true);
      setDirty(false);
    }
  };

  const handleCancelEdit = () => {
    setEditing(false);
    setEditContent('');
    setDirty(false);
  };

  const handleSave = async () => {
    if (!loaded || loaded.kind !== 'text') return;
    setSaving(true);
    let ok=false; try { ok=await writeFileText(path,editContent); } catch(error) { window.alert((error as Error).message); } finally {setSaving(false);}
    if (ok) {
      // Update loaded content to reflect saved state
      setLoaded({ kind: 'text', text: editContent });
      setEditing(false);
      setDirty(false);
    }
  };

  if (!loaded) {
    return (
      <div className="file-content">
        <div className="empty">
          <Icon name="loader" size={18} spin />
        </div>
      </div>
    );
  }
  if (loaded.kind === 'error') {
    return (
      <div className="file-content">
        <div className="empty">{t('files.loadFailed')}</div>
      </div>
    );
  }
  if (loaded.kind === 'binary') {
    return (
      <div className="file-content">
        <div className="empty">{t('files.binary')}</div>
      </div>
    );
  }
  if (loaded.kind === 'image') {
    return (
      <div className="file-content">
        <div className="file-image-wrap">
          <img className="file-image" src={loaded.url} alt={baseName(path)} />
        </div>
      </div>
    );
  }

  // text
  const canEdit = loaded.kind === 'text';

  return (
    <div className="file-content">
      <div className="file-content-toolbar">
        {isMarkdown && !editing && (
          <div className="views-menu">
            <button className={preview ? 'active' : ''} onClick={() => setPreview(true)}>
              {t('files.preview')}
            </button>
            <button className={!preview ? 'active' : ''} onClick={() => setPreview(false)}>
              {t('files.source')}
            </button>
          </div>
        )}
        {(isMarkdown && !editing) && <span className="grow" />}
        {!editing && canEdit && (
          <button className="file-edit-btn" onClick={handleStartEdit} title={t('files.edit')}>
            <Icon name="edit" size={13} />
            <span>{t('files.edit')}</span>
          </button>
        )}
        {editing && (
          <>
            <span className={`file-dirty-indicator ${dirty ? 'visible' : ''}`}>
              <span className="file-dirty-dot" />
              {t('files.unsaved')}
            </span>
            <span className="grow" />
            <button className="file-edit-btn cancel" onClick={handleCancelEdit} disabled={saving}>
              <Icon name="x" size={13} />
              <span>{t('files.cancel')}</span>
            </button>
            <button className="file-edit-btn save" onClick={handleSave} disabled={saving || !dirty}>
              <Icon name={saving ? 'loader' : 'check'} size={13} spin={saving} />
              <span>{t('files.save')}</span>
            </button>
          </>
        )}
      </div>
      {editing ? (
        <textarea
          className="file-edit-area"
          value={editContent}
          onChange={(e) => {
            setEditContent(e.target.value);
            setDirty(e.target.value !== (loaded.kind === 'text' ? loaded.text : ''));
          }}
          spellCheck={false}
          autoFocus
        />
      ) : isMarkdown && preview ? (
        <div className="file-md">
          <Markdown text={loaded.text} basePath={path} />
        </div>
      ) : (
        <HighlightedCode text={loaded.text} fileName={baseName(path)} />
      )}
    </div>
  );
}

function HighlightedCode({ text, fileName }: { text: string; fileName: string }) {
  const body = useMemo(() => (text.endsWith('\n') ? text.slice(0, -1) : text), [text]);
  const html = useMemo(() => highlightCode(body, fileName).html, [body, fileName]);
  const lineCount = useMemo(() => body.split('\n').length, [body]);
  const codeRef = useRef<HTMLElement>(null);

  const [menu, setMenu] = useState<{ x: number; y: number; selection: string } | null>(null);

  return (
    <div
      className="file-code-scroll"
      onContextMenu={(e: ReactMouseEvent<HTMLDivElement>) => {
        e.preventDefault();
        const sel = window.getSelection()?.toString() ?? '';
        setMenu({ x: e.clientX, y: e.clientY, selection: sel });
      }}
    >
      <div className="file-code-rows">
        <div className="file-gutter" aria-hidden="true">
          {Array.from({ length: Math.max(lineCount, 1) }, (_, i) => (
            <span key={i}>{i + 1}</span>
          ))}
        </div>
        <pre className="file-code hljs">
          <code ref={codeRef} dangerouslySetInnerHTML={{ __html: html }} />
        </pre>
      </div>
      {menu && (
        <CodeContextMenu
          x={menu.x}
          y={menu.y}
          selection={menu.selection}
          onClose={() => setMenu(null)}
        />
      )}
    </div>
  );
}

function CodeContextMenu({
  x,
  y,
  selection,
  onClose,
}: {
  x: number;
  y: number;
  selection: string;
  onClose: () => void;
}) {
  const t = useT();
  useEffect(() => {
    const onDown = () => onClose();
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [onClose]);

  return (
    <div
      className="menu-pop code-context-menu"
      style={{ left: x, top: y, position: 'fixed' }}
    >
      {selection && (
        <>
          <button
            onClick={() => {
              void window.piAPI?.openExternal(
                `https://www.google.com/search?q=${encodeURIComponent(selection)}`,
              );
              onClose();
            }}
          >
            <Icon name="search" size={14} />
            {t('codeMenu.searchGoogle')}
          </button>
          <button
            onClick={() => {
              void copyText(selection);
              onClose();
            }}
          >
            <Icon name="copy" size={14} />
            {t('codeMenu.copy')}
          </button>
          <div className="menu-sep" />
        </>
      )}
      <button
        onClick={() => {
          const code = document.querySelector('.file-code code') as HTMLElement | null;
          if (code) {
            const sel = window.getSelection();
            const range = document.createRange();
            range.selectNodeContents(code);
            sel?.removeAllRanges();
            sel?.addRange(range);
            void copyText(sel?.toString() ?? '');
          }
          onClose();
        }}
      >
        <Icon name="check" size={14} />
        {t('codeMenu.selectAll')}
      </button>
    </div>
  );
}
