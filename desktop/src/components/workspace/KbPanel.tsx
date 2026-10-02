/**
 * KbPanel.tsx — Knowledge-base browser panel for the right sidebar.
 *
 * Three views:
 *   - Browse: category tree + entry list + entry detail (Markdown preview)
 *   - Health: visual dashboard of the KB health report
 *   - Tags:   tag cloud with usage counts
 *
 * Data is fetched from the /kb/* REST endpoints registered by kb_handler.go.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiRequest } from '../../store';
import { Icon } from '../Icon';
import { Markdown } from '../Markdown';
import { useT, type TFunc } from '../../i18n/useT';

// ── API Types ──────────────────────────────────────────────────────────────

interface KbEntry {
  path: string;
  rel_path: string;
  title: string;
  category: string;
  tags: string[];
  summary: string;
  source: string;
  modified: string;
}

interface KbStats {
  total_entries: number;
  categories: number;
  tags: number;
  repo_path: string;
}

interface KbCategory {
  name: string;
  count: number;
}

interface KbTag {
  name: string;
  count: number;
}

interface KbHealth {
  total_entries: number;
  categories: number;
  tags: number;
  entries_missing_title: KbEntry[];
  entries_missing_summary: KbEntry[];
  entries_missing_tags: KbEntry[];
  duplicate_groups: KbDupGroup[];
  tag_clusters: KbTagCluster[];
}

interface KbDupGroup {
  canonical_title: string;
  entries: KbEntry[];
}

interface KbTagCluster {
  canonical: string;
  variants: string[];
  count: number;
}

// ── API helpers ────────────────────────────────────────────────────────────

async function apiGet<T>(path:string):Promise<T> { return apiRequest('GET',path); }

async function fetchStats(): Promise<KbStats> {
  return apiGet<KbStats>('/kb/stats');
}

async function fetchEntries(category?: string, tag?: string, query?: string): Promise<KbEntry[]> {
  const params = new URLSearchParams();
  if (category) params.set('category', category);
  if (tag) params.set('tag', tag);
  if (query) params.set('q', query);
  const qs = params.toString();
  const resp = await apiGet<{ entries: KbEntry[]; total: number }>(`/kb/entries${qs ? `?${qs}` : ''}`);
  return resp.entries;
}

async function fetchCategories(): Promise<KbCategory[]> {
  const resp = await apiGet<{ categories: KbCategory[] }>('/kb/categories');
  return resp.categories;
}

async function fetchTags(): Promise<KbTag[]> {
  const resp = await apiGet<{ tags: KbTag[] }>('/kb/tags');
  return resp.tags;
}

async function fetchHealth(): Promise<KbHealth> {
  return apiGet<KbHealth>('/kb/health');
}

async function fetchEntryContent(relPath: string): Promise<string> {
  const resp = await apiGet<{ content: string; path: string }>('/kb/read?path=' + encodeURIComponent(relPath));
  return resp.content;
}

// ── Helpers ────────────────────────────────────────────────────────────────

function relTime(t: TFunc, iso: string): string {
  if (!iso) return '';
  const ts = new Date(iso).getTime();
  if (isNaN(ts)) return '';
  const diff = Date.now() - ts;
  const m = Math.floor(diff / 60000);
  if (m < 1) return t('time.justNow');
  if (m < 60) return t('time.minutesAgo', { m });
  const h = Math.floor(m / 60);
  if (h < 24) return t('time.hoursAgo', { h });
  return t('time.daysAgo', { d: Math.floor(h / 24) });
}

// ── Component ──────────────────────────────────────────────────────────────

type KbView = 'browse' | 'health' | 'tags';

export function KbPanel() {
  const t = useT();
  const [view, setView] = useState<KbView>('browse');
  const [stats, setStats] = useState<KbStats | null>(null);
  const [statsError, setStatsError] = useState(false);
  const [loadingStats, setLoadingStats] = useState(true);

  // Load stats on mount
  useEffect(() => {
    let alive = true;
    setLoadingStats(true);
    void fetchStats()
      .then((s) => alive && (setStats(s), setStatsError(false)))
      .catch(() => alive && setStatsError(true))
      .finally(() => alive && setLoadingStats(false));
    return () => { alive = false; };
  }, []);

  if (loadingStats) {
    return (
      <div className="ws-panel">
        <div className="ws-panel-head">
          <Icon name="book" size={15} />
          <span>{t('kb.title')}</span>
        </div>
        <div className="empty">
          <Icon name="loader" size={18} spin />
        </div>
      </div>
    );
  }

  if (statsError || !stats) {
    return (
      <div className="ws-panel">
        <div className="ws-panel-head">
          <Icon name="book" size={15} />
          <span>{t('kb.title')}</span>
        </div>
        <div className="empty">{t('kb.loadFailed')}</div>
      </div>
    );
  }

  return (
    <div className="ws-panel kb-panel">
      <div className="kb-header">
        <Icon name="database" size={16} />
        <span className="kb-header-title">{t('kb.title')}</span>
        <span className="grow" />
        <div className="kb-stats-mini">
          <span className="kb-stat-chip">{stats.total_entries} {t('kb.entries')}</span>
          <span className="kb-stat-chip">{stats.categories} {t('kb.cats')}</span>
        </div>
      </div>
      <div className="kb-view-tabs">
        <button
          className={`kb-tab ${view === 'browse' ? 'active' : ''}`}
          onClick={() => setView('browse')}
        >
          <Icon name="book" size={14} />
          {t('kb.browse')}
        </button>
        <button
          className={`kb-tab ${view === 'tags' ? 'active' : ''}`}
          onClick={() => setView('tags')}
        >
          <Icon name="tag" size={14} />
          {t('kb.tagsView')}
        </button>
        <button
          className={`kb-tab ${view === 'health' ? 'active' : ''}`}
          onClick={() => setView('health')}
        >
          <Icon name="stethoscope" size={14} />
          {t('kb.health')}
        </button>
      </div>
      {view === 'browse' && <KbBrowseView />}
      {view === 'tags' && <KbTagsView />}
      {view === 'health' && <KbHealthView />}
    </div>
  );
}

// ── Browse View ────────────────────────────────────────────────────────────

function KbBrowseView() {
  const t = useT();
  const [categories, setCategories] = useState<KbCategory[] | null>(null);
  const [activeCategory, setActiveCategory] = useState<string | null>(null);
  const [entries, setEntries] = useState<KbEntry[]>([]);
  const [selectedEntry, setSelectedEntry] = useState<KbEntry | null>(null);
  const [query, setQuery] = useState('');
  const [loadingEntries, setLoadingEntries] = useState(false);

  // Load categories
  useEffect(() => {
    let alive = true;
    void fetchCategories()
      .then((c) => alive && setCategories(c))
      .catch(() => alive && setCategories([]));
    return () => { alive = false; };
  }, []);

  // Load entries when filters change
  // NOTE: query is debounced via a 300ms timer to avoid spamming the API
  // on every keystroke.
  useEffect(() => {
    setLoadingEntries(true);
    setSelectedEntry(null);
    const timer = setTimeout(() => {
      let alive = true;
      void fetchEntries(activeCategory ?? undefined, undefined, query.trim() || undefined)
        .then((e) => alive && setEntries(e))
        .catch(() => alive && setEntries([]))
        .finally(() => alive && setLoadingEntries(false));
      return () => { alive = false; };
    }, 300);
    return () => clearTimeout(timer);
  }, [activeCategory, query]);

  return (
    <div className="kb-browse">
      {/* Search bar */}
      <div className="kb-search">
        <Icon name="search" size={13} />
        <input
          className="kb-search-input"
          placeholder={t('kb.searchPlaceholder')}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          spellCheck={false}
        />
        {query && (
          <button className="kb-search-clear" title={t('files.searchClear')} onClick={() => setQuery('')}>
            <Icon name="x" size={12} />
          </button>
        )}
      </div>

      {/* Category chips */}
      {categories && categories.length > 0 && (
        <div className="kb-categories">
          <button
            className={`kb-cat-chip ${!activeCategory ? 'active' : ''}`}
            onClick={() => setActiveCategory(null)}
          >
            {t('kb.all')}
          </button>
          {categories.map((c) => (
            <button
              key={c.name}
              className={`kb-cat-chip ${activeCategory === c.name ? 'active' : ''}`}
              onClick={() => setActiveCategory(c.name === activeCategory ? null : c.name)}
            >
              {c.name}
              <span className="kb-cat-count">{c.count}</span>
            </button>
          ))}
        </div>
      )}

      {/* Entry list + detail */}
      <div className={`kb-content ${selectedEntry ? 'has-detail' : ''}`}>
        <div className="kb-entry-list">
          {loadingEntries ? (
            <div className="empty">
              <Icon name="loader" size={16} spin />
            </div>
          ) : entries.length === 0 ? (
            <div className="empty">{query ? t('kb.noResults') : t('kb.empty')}</div>
          ) : (
            entries.map((e) => (
              <button
                key={e.rel_path}
                className={`kb-entry-item ${selectedEntry?.rel_path === e.rel_path ? 'active' : ''}`}
                onClick={() => setSelectedEntry(e)}
              >
                <span className="kb-entry-title">{e.title}</span>
                {e.category && <span className="kb-entry-cat">{e.category}</span>}
                {e.tags.length > 0 && (
                  <span className="kb-entry-tags">
                    {e.tags.slice(0, 3).map((tag) => (
                      <span key={tag} className="kb-entry-tag">{tag}</span>
                    ))}
                  </span>
                )}
                {e.summary && <span className="kb-entry-summary">{e.summary}</span>}
              </button>
            ))
          )}
        </div>
        {selectedEntry && (
          <KbEntryDetail entry={selectedEntry} onClose={() => setSelectedEntry(null)} />
        )}
      </div>
    </div>
  );
}

// ── Entry Detail ───────────────────────────────────────────────────────────

function KbEntryDetail({ entry, onClose }: { entry: KbEntry; onClose: () => void }) {
  const t = useT();
  const [content, setContent] = useState<string | null>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    setContent(null);
    setError(false);
    let alive = true;
    void fetchEntryContent(entry.rel_path)
      .then((c) => alive && setContent(c))
      .catch(() => alive && setError(true));
    return () => { alive = false; };
  }, [entry.rel_path]);

  return (
    <div className="kb-entry-detail">
      <div className="kb-detail-head">
        <button className="kb-back-btn" onClick={onClose} title={t('common.back')}>
          <Icon name="arrow-left" size={16} />
        </button>
        <span className="kb-detail-title">{entry.title}</span>
      </div>
      <div className="kb-detail-meta">
        {entry.category && <span className="kb-detail-cat">{entry.category}</span>}
        {entry.tags.length > 0 && (
          <span className="kb-detail-tags">
            {entry.tags.map((tag) => (
              <span key={tag} className="kb-entry-tag">{tag}</span>
            ))}
          </span>
        )}
        <span className="kb-detail-date">{relTime(t, entry.modified)}</span>
      </div>
      <div className="kb-detail-body">
        {error ? (
          <div className="empty">{t('kb.loadFailed')}</div>
        ) : content === null ? (
          <div className="empty">
            <Icon name="loader" size={16} spin />
          </div>
        ) : (
          <Markdown text={content} />
        )}
      </div>
    </div>
  );
}

// ── Tags View ──────────────────────────────────────────────────────────────

function KbTagsView() {
  const t = useT();
  const [tags, setTags] = useState<KbTag[] | null>(null);
  const [activeTag, setActiveTag] = useState<string | null>(null);
  const [entries, setEntries] = useState<KbEntry[]>([]);
  const [selectedEntry, setSelectedEntry] = useState<KbEntry | null>(null);

  useEffect(() => {
    let alive = true;
    void fetchTags()
      .then((t) => alive && setTags(t))
      .catch(() => alive && setTags([]));
    return () => { alive = false; };
  }, []);

  useEffect(() => {
    if (!activeTag) return;
    let alive = true;
    void fetchEntries(undefined, activeTag)
      .then((e) => alive && setEntries(e))
      .catch(() => alive && setEntries([]));
    return () => { alive = false; };
  }, [activeTag]);

  // Reset selected entry when tag changes
  useEffect(() => {
    setSelectedEntry(null);
  }, [activeTag]);

  if (activeTag) {
    return (
      <div className="kb-tags-detail">
        <div className="kb-tags-detail-head">
          <button className="kb-back-btn" onClick={() => setActiveTag(null)} title={t('common.back')}>
            <Icon name="arrow-left" size={16} />
          </button>
          <span className="kb-detail-title">#{activeTag}</span>
          <span className="kb-tag-count">{entries.length}</span>
        </div>
        <div className="kb-entry-list">
          {entries.length === 0 ? (
            <div className="empty">
              <Icon name="loader" size={16} spin />
            </div>
          ) : (
            entries.map((e) => (
              <button
                key={e.rel_path}
                className={`kb-entry-item ${selectedEntry?.rel_path === e.rel_path ? 'active' : ''}`}
                onClick={() => setSelectedEntry(e)}
              >
                <span className="kb-entry-title">{e.title}</span>
                {e.category && <span className="kb-entry-cat">{e.category}</span>}
              </button>
            ))
          )}
        </div>
        {selectedEntry && (
          <KbEntryDetail entry={selectedEntry} onClose={() => setSelectedEntry(null)} />
        )}
      </div>
    );
  }

  // Tag cloud
  if (!tags) {
    return (
      <div className="empty">
        <Icon name="loader" size={16} spin />
      </div>
    );
  }

  if (tags.length === 0) {
    return <div className="empty">{t('kb.noTags')}</div>;
  }

  // Tag cloud layout
  const maxCount = Math.max(...tags.map((tg) => tg.count), 1);

  return (
    <div className="kb-tag-cloud">
      {tags.map((tag) => {
        // Scale font-size: 12px (smallest) to 22px (largest)
        const ratio = tag.count / maxCount;
        const fontSize = 12 + Math.round(ratio * 10);
        return (
          <button
            key={tag.name}
            className="kb-tag-item"
            style={{ fontSize: `${fontSize}px` }}
            onClick={() => setActiveTag(tag.name)}
            title={`${tag.name} (${tag.count})`}
          >
            #{tag.name}
            <span className="kb-tag-num">{tag.count}</span>
          </button>
        );
      })}
    </div>
  );
}

// ── Health View ────────────────────────────────────────────────────────────

function KbHealthView() {
  const t = useT();
  const [health, setHealth] = useState<KbHealth | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    void fetchHealth()
      .then((h) => alive && (setHealth(h), setError(false)))
      .catch(() => alive && setError(true))
      .finally(() => alive && setLoading(false));
    return () => { alive = false; };
  }, []);

  if (loading) {
    return (
      <div className="empty">
        <Icon name="loader" size={18} spin />
      </div>
    );
  }

  if (error || !health) {
    return <div className="empty">{t('kb.loadFailed')}</div>;
  }

  const issues =
    health.entries_missing_title.length +
    health.entries_missing_summary.length +
    health.entries_missing_tags.length +
    health.duplicate_groups.length +
    health.tag_clusters.length;

  const allGood = issues === 0;

  return (
    <div className="kb-health">
      {/* Summary cards */}
      <div className="kb-health-summary">
        <div className="kb-health-card">
          <span className="kb-health-card-value">{health.total_entries}</span>
          <span className="kb-health-card-label">{t('kb.entries')}</span>
        </div>
        <div className="kb-health-card">
          <span className="kb-health-card-value">{health.categories}</span>
          <span className="kb-health-card-label">{t('kb.cats')}</span>
        </div>
        <div className="kb-health-card">
          <span className="kb-health-card-value">{health.tags}</span>
          <span className="kb-health-card-label">{t('kb.tagsView')}</span>
        </div>
        <div className={`kb-health-card ${allGood ? 'good' : 'warn'}`}>
          <span className="kb-health-card-value">{issues}</span>
          <span className="kb-health-card-label">{t('kb.issues')}</span>
        </div>
      </div>

      {allGood && (
        <div className="kb-health-allgood">
          <Icon name="check" size={16} />
          {t('kb.allGood')}
        </div>
      )}

      {/* Missing metadata */}
      {(health.entries_missing_title.length > 0 ||
        health.entries_missing_summary.length > 0 ||
        health.entries_missing_tags.length > 0) && (
        <div className="kb-health-section">
          <div className="kb-health-section-head">
            <Icon name="alert-circle" size={14} />
            <span>{t('kb.missingMeta')}</span>
          </div>
          {health.entries_missing_title.length > 0 && (
            <div className="kb-health-issue">
              <span className="kb-health-issue-label">
                {t('kb.missingTitle')} ({health.entries_missing_title.length})
              </span>
              <div className="kb-health-issue-items">
                {health.entries_missing_title.slice(0, 5).map((e) => (
                  <span key={e.rel_path} className="kb-health-pill" title={e.rel_path}>
                    {e.rel_path}
                  </span>
                ))}
                {health.entries_missing_title.length > 5 && (
                  <span className="kb-health-more">
                    +{health.entries_missing_title.length - 5}
                  </span>
                )}
              </div>
            </div>
          )}
          {health.entries_missing_summary.length > 0 && (
            <div className="kb-health-issue">
              <span className="kb-health-issue-label">
                {t('kb.missingSummary')} ({health.entries_missing_summary.length})
              </span>
              <div className="kb-health-issue-items">
                {health.entries_missing_summary.slice(0, 5).map((e) => (
                  <span key={e.rel_path} className="kb-health-pill" title={e.rel_path}>
                    {e.rel_path}
                  </span>
                ))}
                {health.entries_missing_summary.length > 5 && (
                  <span className="kb-health-more">
                    +{health.entries_missing_summary.length - 5}
                  </span>
                )}
              </div>
            </div>
          )}
          {health.entries_missing_tags.length > 0 && (
            <div className="kb-health-issue">
              <span className="kb-health-issue-label">
                {t('kb.missingTags')} ({health.entries_missing_tags.length})
              </span>
              <div className="kb-health-issue-items">
                {health.entries_missing_tags.slice(0, 5).map((e) => (
                  <span key={e.rel_path} className="kb-health-pill" title={e.rel_path}>
                    {e.rel_path}
                  </span>
                ))}
                {health.entries_missing_tags.length > 5 && (
                  <span className="kb-health-more">
                    +{health.entries_missing_tags.length - 5}
                  </span>
                )}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Duplicates */}
      {health.duplicate_groups.length > 0 && (
        <div className="kb-health-section">
          <div className="kb-health-section-head">
            <Icon name="copy" size={14} />
            <span>{t('kb.duplicates')} ({health.duplicate_groups.length})</span>
          </div>
          {health.duplicate_groups.slice(0, 10).map((g, i) => (
            <div key={i} className="kb-health-dup">
              <span className="kb-health-dup-title">{g.canonical_title}</span>
              <span className="kb-health-dup-count">{g.entries.length}</span>
            </div>
          ))}
        </div>
      )}

      {/* Tag clusters */}
      {health.tag_clusters.length > 0 && (
        <div className="kb-health-section">
          <div className="kb-health-section-head">
            <Icon name="tag" size={14} />
            <span>{t('kb.tagClusters')} ({health.tag_clusters.length})</span>
          </div>
          {health.tag_clusters.map((c, i) => (
            <div key={i} className="kb-health-cluster">
              <span className="kb-health-cluster-canonical">{c.canonical}</span>
              <span className="kb-health-cluster-arrow">←</span>
              <span className="kb-health-cluster-variants">
                {c.variants.join(' / ')}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
