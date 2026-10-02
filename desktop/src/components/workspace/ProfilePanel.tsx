/**
 * ProfilePanel.tsx — User profile viewer for the right sidebar.
 *
 * Shows the "condensed second brain" — facts the agent has learned about
 * the user across all agents (coding, music, kb), grouped by category.
 *
 * Data is fetched from the /profile REST endpoint.
 */

import { useEffect, useState } from 'react';
import { apiRequest } from '../../store';
import { Icon } from '../Icon';
import { Markdown } from '../Markdown';
import { useT } from '../../i18n/useT';

// ── API Types ──────────────────────────────────────────────────────────────

interface ProfileFact {
  key: string;
  value: string;
  source: string;
  updated: string;
  access_count: number;
}

interface ProfileCategory {
  name: string;
  label: string;
  count: number;
  facts: ProfileFact[];
}

interface ProfileData {
  categories: ProfileCategory[];
  summary: string;
  total_facts: number;
}

// ── API helpers ────────────────────────────────────────────────────────────

async function fetchProfile():Promise<ProfileData> { return apiRequest('GET','/profile'); }
async function deleteFact(category:string,key:string):Promise<void> { await apiRequest('DELETE','/profile',{category,key}); }

// ── Helpers ────────────────────────────────────────────────────────────────

function relTime(iso: string): string {
  if (!iso) return '';
  const ts = new Date(iso).getTime();
  if (isNaN(ts)) return '';
  const diff = Date.now() - ts;
  const m = Math.floor(diff / 60000);
  if (m < 1) return '刚刚';
  if (m < 60) return `${m}分钟前`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}小时前`;
  return `${Math.floor(h / 24)}天前`;
}

// ── Category icons ─────────────────────────────────────────────────────────

const CATEGORY_ICONS: Record<string, string> = {
  coding: 'code',
  music: 'play',
  general: 'user',
};

// ── Component ──────────────────────────────────────────────────────────────

export function ProfilePanel() {
  const t = useT();
  const [data, setData] = useState<ProfileData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expandedCats, setExpandedCats] = useState<Set<string>>(new Set());

  const load = () => {
    setLoading(true);
    setError(null);
    void fetchProfile()
      .then((d) => {
        setData(d);
        setError(null);
        // Expand all categories by default
        setExpandedCats(new Set(d.categories.map((c) => c.name)));
      })
      .catch((err) => {
        console.error('[ProfilePanel] Failed to load profile:', err);
        setError(err.message || 'Failed to load');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
  }, []);

  const handleDelete = async (category: string, key: string) => {
    try {
      await deleteFact(category, key);
      load(); // refresh
    } catch {
      // best-effort
    }
  };

  const toggleCategory = (name: string) => {
    setExpandedCats((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  if (loading) {
    return (
      <div className="ws-panel">
        <div className="ws-panel-head">
          <Icon name="user" size={15} />
          <span>{t('profile.title')}</span>
        </div>
        <div className="empty">
          <Icon name="loader" size={18} spin />
        </div>
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="ws-panel">
        <div className="ws-panel-head">
          <Icon name="user" size={15} />
          <span>{t('profile.title')}</span>
        </div>
        <div className="empty">
          {t('profile.loadFailed')}
          <br />
          <span style={{ fontSize: '11px', color: 'var(--text-faint)', fontFamily: 'var(--mono)' }}>
            {error}
          </span>
          <br />
          <button
            className="chip interactive"
            style={{ marginTop: 8 }}
            onClick={load}
          >
            <Icon name="refresh" size={13} />
            <span>{t('profile.retry')}</span>
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="ws-panel profile-panel">
      {/* Header */}
      <div className="profile-header">
        <Icon name="user" size={16} />
        <span className="profile-header-title">{t('profile.title')}</span>
        <span className="grow" />
        <span className="profile-stat-chip">{data.total_facts} {t('profile.facts')}</span>
      </div>

      {/* Summary — the "condensed second brain" as injected into agent prompts */}
      {data.summary && (
        <div className="profile-summary">
          <div className="profile-summary-label">{t('profile.agentSummary')}</div>
          <div className="profile-summary-body">
            <Markdown text={data.summary} />
          </div>
        </div>
      )}

      {/* Categories */}
      {data.categories.length === 0 ? (
        <div className="empty">{t('profile.empty')}</div>
      ) : (
        <div className="profile-categories">
          {data.categories.map((cat) => {
            const expanded = expandedCats.has(cat.name);
            const iconName = CATEGORY_ICONS[cat.name] || 'tag';
            return (
              <div key={cat.name} className="profile-cat">
                <button
                  className={`profile-cat-header ${expanded ? 'expanded' : ''}`}
                  onClick={() => toggleCategory(cat.name)}
                >
                  <Icon name={iconName as any} size={14} />
                  <span className="profile-cat-label">{cat.label}</span>
                  <span className="profile-cat-count">{cat.count}</span>
                  <span className="grow" />
                  <Icon
                    name={expanded ? 'chevron-down' : 'chevron-right'}
                    size={14}
                  />
                </button>
                {expanded && (
                  <div className="profile-cat-facts">
                    {cat.facts.map((fact) => (
                      <div key={fact.key} className="profile-fact">
                        <div className="profile-fact-main">
                          <span className="profile-fact-key">{fact.key}</span>
                          <span className="profile-fact-value">{fact.value}</span>
                        </div>
                        <div className="profile-fact-meta">
                          {fact.source && (
                            <span className="profile-fact-source">{fact.source}</span>
                          )}
                          <span className="profile-fact-date">{relTime(fact.updated)}</span>
                          {fact.access_count > 1 && (
                            <span className="profile-fact-count">×{fact.access_count}</span>
                          )}
                          <button
                            className="profile-fact-delete"
                            title={t('profile.delete')}
                            onClick={(e) => {
                              e.stopPropagation();
                              void handleDelete(cat.name, fact.key);
                            }}
                          >
                            <Icon name="x" size={12} />
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {data.categories.length === 0 && !data.summary && (
        <div className="profile-hint">{t('profile.hint')}</div>
      )}
    </div>
  );
}
