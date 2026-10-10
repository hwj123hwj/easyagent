import { useCallback, useEffect, useRef, useState } from "react";
import {
  marketSearch,
  marketSections,
  marketInstalled,
  marketInstallStart,
  marketSkillDetail,
  marketUninstall,
  subscribeInstallJob,
  formatBytes,
  type MarketSkill,
  type MarketSection,
  type InstalledSkill,
  type InstallJobState,
} from "../client/skill-market";
import { copyText } from "../client/clipboard";
import { Icon } from "./Icon";
import { useT } from "../i18n/useT";
import type { TranslationKey } from "../i18n/i18n";

// Install-phase → i18n key (server phases outside this map render raw).
const PHASE_LABEL: Record<string, TranslationKey> = {
  resolving: "settings.skillMarket.phase.resolving",
  downloading: "settings.skillMarket.phase.downloading",
  verifying: "settings.skillMarket.phase.verifying",
  extracting: "settings.skillMarket.phase.extracting",
  done: "settings.skillMarket.phase.done",
};

type SortKey = "featured" | "installs" | "name";
type View = "market" | "installed";

type InstallState =
  | { kind: "idle" }
  | { kind: "running"; jobId: string; phase: string; pct: number | null }
  | { kind: "succeeded"; name: string }
  | { kind: "failed"; message: string };

// Deterministic tile hue so the same skill always renders the same color.
function tileHue(name: string): number {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360;
  return h;
}

// Up to two leading characters: CJK names keep their first two glyphs
// ("三万同款" → "三万"), latin names keep the first two alphanumerics
// ("Word 文档" → "Wo").
function tileInitials(name: string): string {
  const chars = [...(name || "").trim()].filter((c) => !/\s/.test(c));
  if (chars.length === 0) return "?";
  if (/[\u3000-\u9fff\u3400-\u4dbf]/.test(chars[0])) return chars.slice(0, 2).join("");
  const latin = chars.filter((c) => /[a-z0-9]/i.test(c));
  return (latin.length > 0 ? latin.slice(0, 2) : chars.slice(0, 1)).join("");
}

function SkillTile({ name, iconUrl, large }: { name: string; iconUrl?: string | null; large?: boolean }) {
  const [broken, setBroken] = useState(false);
  const sizeClass = large ? " skill-market-tile-lg" : "";
  if (iconUrl && !broken) {
    return <img className={`skill-market-tile${sizeClass}`} src={iconUrl} alt="" onError={() => setBroken(true)} />;
  }
  const hue = tileHue(name);
  const initials = tileInitials(name);
  return (
    <span
      className={`skill-market-tile skill-market-tile-fallback${sizeClass}${[...initials].length > 1 ? " has-two" : ""}`}
      style={{
        background: `linear-gradient(135deg, hsl(${hue} 46% 52%), hsl(${(hue + 42) % 360} 52% 38%))`,
      }}
      aria-hidden="true"
    >
      {initials}
    </span>
  );
}

// ── Detail view (opened by clicking a card) ─────────────────────────────────

function SkillMarketDetail({
  skill,
  installed,
  installs,
  confirmName,
  onBack,
  onInstall,
  onUninstallConfirm,
  onCancelUninstall,
  onUninstall,
}: {
  skill: MarketSkill;
  installed: boolean;
  installs: InstallState | undefined;
  confirmName: string | null;
  onBack: () => void;
  onInstall: () => void;
  onUninstallConfirm: () => void;
  onCancelUninstall: () => void;
  onUninstall: () => void;
}) {
  const t = useT();
  const st = installs ?? { kind: "idle" as const };
  const usageExample = (skill.usage_example ?? "").trim();
  const previews = skill.preview_images ?? [];
  const files = skill.example_files ?? [];
  const richDescription = (skill.rich_description ?? "").trim();
  const [copied, setCopied] = useState(false);
  const [lightbox, setLightbox] = useState<number | null>(null);

  useEffect(() => {
    if (lightbox === null) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setLightbox(null);
      if (e.key === "ArrowLeft") setLightbox((c) => (c === null ? c : (c - 1 + previews.length) % previews.length));
      if (e.key === "ArrowRight") setLightbox((c) => (c === null ? c : (c + 1) % previews.length));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [lightbox, previews.length]);

  const copyUsage = async () => {
    try {
      await copyText(usageExample);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // Copy is a convenience; the prompt stays selectable if it fails.
    }
  };

  const isInstalled = installed;

  return (
    <div className="skill-market-detail">
      <button className="skill-market-back" onClick={onBack}>
        <Icon name="arrow-left" /> {t("common.back")}
      </button>

      <div className="skill-market-detail-head">
        <SkillTile name={skill.display_name || skill.name} iconUrl={skill.icon_url} large />
        <div className="skill-market-detail-titlerow">
          <h2 className="skill-market-detail-name">{skill.display_name || skill.name}</h2>
          <span className="skill-market-detail-version">v{skill.version}</span>
          {skill.featured && (
            <span className="skill-market-featured-tag">
              <Icon name="star" /> {t("settings.skillMarket.featured")}
            </span>
          )}
        </div>
        <div className="skill-market-detail-action">
          {st.kind === "running" ? (
            <div className="skill-market-progress" aria-busy="true">
              <div className="skill-market-progress-bar">
                <div
                  className="skill-market-progress-fill"
                  style={{ width: st.pct === null ? "40%" : `${st.pct}%` }}
                  data-indeterminate={st.pct === null || undefined}
                />
              </div>
              <span className="skill-market-progress-label">
                {PHASE_LABEL[st.phase] ? t(PHASE_LABEL[st.phase]) : st.phase}
                {st.pct !== null ? ` ${st.pct}%` : ""}
              </span>
            </div>
          ) : confirmName === skill.name ? (
            <span className="skill-market-confirm">
              <button className="danger" onClick={onUninstall}>
                {t("settings.skillMarket.confirmUninstall")}
              </button>
              <button onClick={onCancelUninstall}>{t("common.cancel")}</button>
            </span>
          ) : isInstalled || st.kind === "succeeded" ? (
            <span className="skill-market-installed-badge">
              <Icon name="check" /> {t("settings.skillMarket.installedBadge")}
            </span>
          ) : (
            <button className="skill-market-install-btn" onClick={onInstall}>
              <Icon name="download" /> {t("settings.skillMarket.install")}
            </button>
          )}
        </div>
      </div>

      {st.kind === "failed" && <div className="skill-market-flash err">{st.message}</div>}
      {st.kind === "succeeded" && !isInstalled && <div className="skill-market-flash ok">{t("settings.skillMarket.installDone")}</div>}

      <p className="skill-market-detail-desc">{skill.description}</p>

      {skill.tags.length > 0 && (
        <div className="skill-market-card-tags">
          {skill.tags.map((tag) => (
            <span key={tag} className="skill-market-card-tag">#{tag}</span>
          ))}
        </div>
      )}

      <div className="skill-market-detail-meta">
        <span>
          <Icon name="download" /> {t("settings.skillMarket.installs", { count: skill.install_count })}
        </span>
        {formatBytes(skill.size_bytes) && <span>{formatBytes(skill.size_bytes)}</span>}
        {skill.section_name && <span>{skill.section_name}</span>}
        {isInstalled && confirmName !== skill.name && (
          <button className="ghost skill-market-detail-uninstall" onClick={onUninstallConfirm}>
            <Icon name="delete" /> {t("settings.skillMarket.uninstall")}
          </button>
        )}
      </div>

      {usageExample && (
        <section className="skill-market-detail-section" aria-labelledby="skill-market-usage-title">
          <div className="skill-market-detail-section-head">
            <div className="skill-market-detail-section-title">
              <span className="skill-market-detail-section-icon"><Icon name="copy" /></span>
              <div>
                <h3 id="skill-market-usage-title">{t("settings.skillMarket.usageTitle")}</h3>
                <p>{t("settings.skillMarket.usageSubtitle")}</p>
              </div>
            </div>
            <button className="ghost skill-market-copy-btn" onClick={() => void copyUsage()}>
              <Icon name={copied ? "check" : "copy"} /> {copied ? t("settings.skillMarket.usageCopied") : t("settings.skillMarket.usageCopy")}
            </button>
          </div>
          <div className="skill-market-usage-prompt">{usageExample}</div>
        </section>
      )}

      {previews.length > 0 && (
        <section className="skill-market-detail-section" aria-labelledby="skill-market-preview-title">
          <div className="skill-market-detail-section-head">
            <div className="skill-market-detail-section-title">
              <span className="skill-market-detail-section-icon"><Icon name="image" /></span>
              <div>
                <h3 id="skill-market-preview-title">{t("settings.skillMarket.previewTitle")}</h3>
                <p>{t("settings.skillMarket.previewSubtitle")}</p>
              </div>
            </div>
            <span className="skill-market-preview-count">
              {previews.length === 1
                ? t("settings.skillMarket.previewCountOne")
                : t("settings.skillMarket.previewCountMany", { count: previews.length })}
            </span>
          </div>
          <div className={`skill-market-previews count-${Math.min(previews.length, 3)}`}>
            {previews.map((src, i) => (
              <button key={src} className="skill-market-preview" onClick={() => setLightbox(i)}>
                <img src={src} alt={t("settings.skillMarket.previewImageAlt", { name: skill.display_name || skill.name, index: i + 1 })} loading="lazy" />
              </button>
            ))}
          </div>
        </section>
      )}

      {files.length > 0 && (
        <section className="skill-market-detail-section" aria-labelledby="skill-market-files-title">
          <div className="skill-market-detail-section-head">
            <div className="skill-market-detail-section-title">
              <span className="skill-market-detail-section-icon"><Icon name="file" /></span>
              <div>
                <h3 id="skill-market-files-title">{t("settings.skillMarket.filesTitle")}</h3>
                <p>{t("settings.skillMarket.filesSubtitle")}</p>
              </div>
            </div>
          </div>
          <div className="skill-market-files">
            {files.map((f) => (
              <a key={f.url} className="skill-market-file" href={f.url} download={f.name} target="_blank" rel="noreferrer">
                <Icon name="file" />
                <span className="skill-market-file-name">{f.name}</span>
                {formatBytes(f.size) && <span className="skill-market-file-size">{formatBytes(f.size)}</span>}
                <span className="skill-market-file-download">
                  <Icon name="download" /> {t("settings.skillMarket.fileDownload")}
                </span>
              </a>
            ))}
          </div>
        </section>
      )}

      {richDescription && (
        <div className="skill-market-detail-rich">{richDescription}</div>
      )}

      {lightbox !== null && previews[lightbox] && (
        <div className="skill-market-lightbox" role="dialog" aria-modal="true" onClick={() => setLightbox(null)}>
          <button className="skill-market-lightbox-close" aria-label={t("common.dismiss")} onClick={() => setLightbox(null)}>
            <Icon name="x" />
          </button>
          {previews.length > 1 && (
            <button
              className="skill-market-lightbox-nav prev"
              aria-label={t("settings.skillMarket.previous")}
              onClick={(e) => { e.stopPropagation(); setLightbox((lightbox - 1 + previews.length) % previews.length); }}
            >
              <Icon name="chevron-right" className="flip-x" />
            </button>
          )}
          <img src={previews[lightbox]} alt="" onClick={(e) => e.stopPropagation()} />
          {previews.length > 1 && (
            <button
              className="skill-market-lightbox-nav next"
              aria-label={t("settings.skillMarket.next")}
              onClick={(e) => { e.stopPropagation(); setLightbox((lightbox + 1) % previews.length); }}
            >
              <Icon name="chevron-right" />
            </button>
          )}
          {previews.length > 1 && (
            <span className="skill-market-lightbox-counter">{lightbox + 1} / {previews.length}</span>
          )}
        </div>
      )}
    </div>
  );
}

export function SkillMarketPanel() {
  const t = useT();

  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<SortKey>("featured");
  const [view, setView] = useState<View>("market");
  const [skills, setSkills] = useState<MarketSkill[]>([]);
  const [sections, setSections] = useState<MarketSection[]>([]);
  const [section, setSection] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);

  const [installed, setInstalled] = useState<InstalledSkill[]>([]);
  // Install state keyed by skill id; one install per skill at a time.
  const [installs, setInstalls] = useState<Record<number, InstallState>>({});
  const [confirmName, setConfirmName] = useState<string | null>(null);
  const [detail, setDetail] = useState<MarketSkill | null>(null);
  const searchSeq = useRef(0);
  const detailSeq = useRef(0);
  const pageSize = useRef(0);
  const mounted = useRef(false);
  const starting = useRef(new Set<number>());
  const subsRef = useRef(new Map<number, () => void>());

  const refreshInstalled = useCallback(async () => {
    try {
      setInstalled(await marketInstalled());
    } catch {
      // Non-fatal: the panel still works for browsing.
    }
  }, []);

  const runSearch = useCallback(
    async (opts?: { page?: number }) => {
      const seq = ++searchSeq.current;
      const p = opts?.page ?? 1;
      if (p > 1) setLoadingMore(true);
      else setLoading(true);
      setError(null);
      try {
        const res = await marketSearch({
          q: query.trim() || undefined,
          sort,
          page: p,
          section: section ?? undefined,
        });
        if (!mounted.current || seq !== searchSeq.current) return;
        if (p === 1) pageSize.current = res.skills.length;
        setSkills((prev) => (p === 1 ? res.skills : [...prev, ...res.skills]));
        setTotal(res.total);
        setPage(p);
      } catch (err) {
        if (mounted.current && seq === searchSeq.current) setError(err instanceof Error ? err.message : String(err));
      } finally {
        if (mounted.current && seq === searchSeq.current) {
          setLoading(false);
          setLoadingMore(false);
        }
      }
    },
    [query, sort, section],
  );

  useEffect(() => {
    mounted.current = true;
    void marketSections()
      .then(setSections)
      .catch(() => setSections([]));
    void refreshInstalled();
    const subs = subsRef.current;
    return () => {
      mounted.current = false;
      searchSeq.current++;
      subs.forEach((cancel) => cancel());
      subs.clear();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (view === "installed") return;
    void runSearch();
  }, [view, sort, section]);

  const switchToInstalled = useCallback(() => {
    setView("installed");
    setConfirmName(null);
    void refreshInstalled();
  }, [refreshInstalled]);

  const watchInstall = useCallback(
    (skillId: number, jobId: string) => {
      setInstalls((m) => ({ ...m, [skillId]: { kind: "running", jobId, phase: "resolving", pct: null } }));
      subsRef.current.get(skillId)?.();
      const cancel = subscribeInstallJob(jobId, {
        onEvent: (evt: InstallJobState) => {
          if (evt.state === "succeeded") {
            setInstalls((m) => ({
              ...m,
              [skillId]: { kind: "succeeded", name: evt.display_name || evt.name || "" },
            }));
            void refreshInstalled();
          } else if (evt.state === "failed") {
            setInstalls((m) => ({
              ...m,
              [skillId]: { kind: "failed", message: evt.error || "install failed" },
            }));
          } else {
            const pct =
              evt.bytes_total && evt.bytes_total > 0
                ? Math.min(100, Math.round((100 * (evt.bytes_done ?? 0)) / evt.bytes_total))
                : null;
            setInstalls((m) => ({
              ...m,
              [skillId]: { kind: "running", jobId, phase: evt.phase || "downloading", pct },
            }));
          }
        },
        onError: (err) => {
          setInstalls((m) => ({ ...m, [skillId]: { kind: "failed", message: err.message } }));
        },
      });
      subsRef.current.set(skillId, cancel);
    },
    [refreshInstalled],
  );

  const install = useCallback(
    async (skill: MarketSkill) => {
      try {
        if (starting.current.has(skill.id)) return;
        starting.current.add(skill.id);
        setInstalls(m => ({ ...m, [skill.id]: { kind: "running", jobId: "", phase: "resolving", pct: null } }));
        const { job_id } = await marketInstallStart(skill.id);
        if (mounted.current) watchInstall(skill.id, job_id);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        setInstalls((m) => ({ ...m, [skill.id]: { kind: "failed", message: msg } }));
      } finally { starting.current.delete(skill.id); }
    },
    [watchInstall],
  );

  const uninstall = useCallback(
    async (name: string) => {
      setConfirmName(null);
      try {
        await marketUninstall(name);
        void refreshInstalled();
        setInstalls((m) => ({ ...m, [installed.find((i) => i.name === name)?.id ?? -1]: { kind: "idle" } }));
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      }
    },
    [installed, refreshInstalled],
  );

  const installedNames = new Set(installed.map((i) => i.name));
  const hasMore = view === "market" && !loading && skills.length < total;

  // Open the detail view with the browse item, then merge in the full
  // showcase payload (usage example, previews) once it arrives.
  const openDetail = useCallback((s: MarketSkill) => {
    const seq = ++detailSeq.current;
    setConfirmName(null);
    setDetail(s);
    void marketSkillDetail(s.id)
      .then((full) => {
        if (mounted.current && seq === detailSeq.current) setDetail(full);
      })
      .catch(() => {
        // The browse item still renders fine without showcase fields.
      });
  }, []);

  const renderCardActions = (s: MarketSkill) => {
    const st = installs[s.id] ?? { kind: "idle" as const };
    const isInstalled = installedNames.has(s.name);
    if (isInstalled) {
      return confirmName === s.name ? (
        <span className="skill-market-confirm">
          <button className="danger" onClick={() => void uninstall(s.name)}>
            {t("settings.skillMarket.confirmUninstall")}
          </button>
          <button onClick={() => setConfirmName(null)}>{t("common.cancel")}</button>
        </span>
      ) : (
        <>
          <span className="skill-market-installed-badge">
            <Icon name="check" /> {t("settings.skillMarket.installedBadge")}
          </span>
          <button
            className="ghost skill-market-uninstall-btn"
            title={t("settings.skillMarket.uninstall")}
            aria-label={t("settings.skillMarket.uninstall")}
            onClick={() => setConfirmName(s.name)}
          >
            <Icon name="delete" />
          </button>
        </>
      );
    }
    if (st.kind === "running") {
      return (
        <div className="skill-market-progress" aria-busy="true">
          <div className="skill-market-progress-bar">
            <div
              className="skill-market-progress-fill"
              style={{ width: st.pct === null ? "40%" : `${st.pct}%` }}
              data-indeterminate={st.pct === null || undefined}
            />
          </div>
          <span className="skill-market-progress-label">
            {PHASE_LABEL[st.phase] ? t(PHASE_LABEL[st.phase]) : st.phase}
            {st.pct !== null ? ` ${st.pct}%` : ""}
          </span>
        </div>
      );
    }
    return (
      <button className="skill-market-install-btn" onClick={() => void install(s)}>
        <Icon name="download" /> {t("settings.skillMarket.install")}
      </button>
    );
  };

  if (detail) {
    return (
      <div className="settings-section skill-market">
        {error && (
          <div className="skill-market-error" role="alert">
            <Icon name="alert" />
            <span>{error}</span>
            <button onClick={() => setError(null)} aria-label={t("common.dismiss")}>
              <Icon name="x" />
            </button>
          </div>
        )}
        <SkillMarketDetail
          skill={detail}
          installed={installedNames.has(detail.name)}
          installs={installs[detail.id]}
          confirmName={confirmName}
          onBack={() => { setDetail(null); setConfirmName(null); }}
          onInstall={() => void install(detail)}
          onUninstallConfirm={() => setConfirmName(detail.name)}
          onCancelUninstall={() => setConfirmName(null)}
          onUninstall={() => void uninstall(detail.name)}
        />
      </div>
    );
  }

  return (
    <div className="settings-section skill-market">
      <div className="skill-market-toolbar">
        <form
          className="skill-market-search"
          onSubmit={(e) => {
            e.preventDefault();
            setView("market");
            void runSearch();
          }}
        >
          <Icon name="search" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("settings.skillMarket.searchPlaceholder")}
            aria-label={t("settings.skillMarket.searchPlaceholder")}
          />
        </form>
        <div className="skill-market-sort" role="group" aria-label={t("settings.skillMarket.sortFeatured")}>
          <button aria-pressed={view === "market" && sort === "featured"} className={view === "market" && sort === "featured" ? "active" : ""} onClick={() => { setView("market"); setSort("featured"); }}>
            {t("settings.skillMarket.sortFeatured")}
          </button>
          <button aria-pressed={view === "market" && sort === "installs"} className={view === "market" && sort === "installs" ? "active" : ""} onClick={() => { setView("market"); setSort("installs"); }}>
            {t("settings.skillMarket.sortInstalls")}
          </button>
          <button aria-pressed={view === "market" && sort === "name"} className={view === "market" && sort === "name" ? "active" : ""} onClick={() => { setView("market"); setSort("name"); }}>
            {t("settings.skillMarket.sortName")}
          </button>
          <button aria-pressed={view === "installed"} className={view === "installed" ? "active" : ""} onClick={switchToInstalled}>
            {t("settings.skillMarket.sortInstalled")}
          </button>
        </div>
      </div>

      {view === "market" && sections.length > 0 && (
        <div className="skill-market-sections" aria-label={t("settings.skillMarket.allSections")}>
          <button
            aria-pressed={section === null}
            className={section === null ? "active" : ""}
            onClick={() => { setSection(null); }}
          >
            {t("settings.skillMarket.allSections")}
          </button>
          {sections.map((s) => (
            <button key={s.id} aria-pressed={section === s.id} className={section === s.id ? "active" : ""} onClick={() => setSection(s.id)}>
              {s.name}
            </button>
          ))}
        </div>
      )}

      {error && (
        <div className="skill-market-error" role="alert">
          <Icon name="alert" />
          <span>{error}</span>
          <button onClick={() => setError(null)} aria-label={t("common.dismiss")}>
            <Icon name="x" />
          </button>
        </div>
      )}

      {view === "market" ? (
        <>
          {loading && skills.length === 0 ? (
            <div className="skill-market-grid" aria-hidden="true" aria-busy="true">
              {Array.from({ length: 8 }, (_, i) => (
                <div key={i} className="skill-market-card skel">
                  <div className="skill-market-card-header">
                    <div className="skill-market-tile sk-shimmer" />
                    <div className="skill-market-skel-lines">
                      <div className="sk-shimmer" style={{ width: "62%" }} />
                      <div className="sk-shimmer" style={{ width: "28%" }} />
                    </div>
                  </div>
                  <div className="skill-market-skel-desc">
                    <div className="sk-shimmer" style={{ width: "100%" }} />
                    <div className="sk-shimmer" style={{ width: "76%" }} />
                  </div>
                  <div className="skill-market-card-footer">
                    <div className="sk-shimmer" style={{ width: 52, height: 12 }} />
                    <div className="sk-shimmer skill-market-skel-btn" />
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className={`skill-market-grid${loading ? " is-refreshing" : ""}`}>
              {skills.map((s) => {
                const st = installs[s.id] ?? { kind: "idle" as const };
                return (
                  <article
                    key={s.id}
                    className={`skill-market-card${s.featured ? " featured" : ""}`}
                    role="button"
                    tabIndex={0}
                    onClick={() => openDetail(s)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        openDetail(s);
                      }
                    }}
                  >
                    {s.featured && (
                      <span className="skill-market-featured-badge">
                        <Icon name="star" /> {t("settings.skillMarket.featured")}
                      </span>
                    )}
                    <div className="skill-market-card-header">
                      <SkillTile name={s.display_name || s.name} iconUrl={s.icon_url} />
                      <div className="skill-market-card-title">
                        <h3 className="skill-market-card-name" title={s.display_name || s.name}>
                          {s.display_name || s.name}
                        </h3>
                        <span className="skill-market-card-version">v{s.version}</span>
                      </div>
                    </div>
                    <p className="skill-market-card-desc" title={s.description}>{s.description}</p>
                    {s.tags.length > 0 && (
                      <div className="skill-market-card-tags">
                        {s.tags.slice(0, 3).map((tag) => (
                          <span key={tag} className="skill-market-card-tag">#{tag}</span>
                        ))}
                      </div>
                    )}
                    {st.kind === "succeeded" && (
                      <div className="skill-market-flash ok">{t("settings.skillMarket.installDone")}</div>
                    )}
                    {st.kind === "failed" && (
                      <div className="skill-market-flash err">{st.message}</div>
                    )}
                    <div className="skill-market-card-footer">
                      <div className="skill-market-card-stats">
                        <span
                          className="skill-market-card-installs"
                          aria-label={t("settings.skillMarket.installs", { count: s.install_count })}
                        >
                          <Icon name="download" /> {s.install_count}
                        </span>
                        {formatBytes(s.size_bytes) && (
                          <span className="skill-market-card-size">{formatBytes(s.size_bytes)}</span>
                        )}
                      </div>
                      <div className="skill-market-card-actions" onClick={(e) => e.stopPropagation()}>{renderCardActions(s)}</div>
                    </div>
                  </article>
                );
              })}
            </div>
          )}

          {!loading && skills.length === 0 && !error && (
            <div className="skill-market-empty">{t("settings.skillMarket.empty")}</div>
          )}

          {hasMore && (
            <div className="skill-market-load-more">
              <button disabled={loadingMore} onClick={() => void runSearch({ page: page + 1 })}>
                {loadingMore ? t("settings.skillMarket.loading") : t("settings.skillMarket.loadMore")}
              </button>
            </div>
          )}
        </>
      ) : (
        <>
          {installed.length === 0 ? (
            <div className="skill-market-empty">{t("settings.skillMarket.emptyInstalled")}</div>
          ) : (
            <div className="skill-market-grid">
              {installed.map((s) => (
                <article key={s.name} className="skill-market-card">
                  <div className="skill-market-card-header">
                    <SkillTile name={s.display_name || s.name} />
                    <div className="skill-market-card-title">
                      <h3 className="skill-market-card-name" title={s.display_name || s.name}>
                        {s.display_name || s.name}
                      </h3>
                      {s.version && <span className="skill-market-card-version">v{s.version}</span>}
                    </div>
                  </div>
                  <div className="skill-market-card-footer">
                    <span className="skill-market-installed-badge">
                      <Icon name="check" /> {t("settings.skillMarket.installedBadge")}
                    </span>
                    <div className="skill-market-card-actions">
                      {confirmName === s.name ? (
                        <span className="skill-market-confirm">
                          <button className="danger" onClick={() => void uninstall(s.name)}>
                            {t("settings.skillMarket.confirmUninstall")}
                          </button>
                          <button onClick={() => setConfirmName(null)}>{t("common.cancel")}</button>
                        </span>
                      ) : (
                        <button className="ghost skill-market-uninstall-btn" onClick={() => setConfirmName(s.name)}>
                          <Icon name="delete" />
                          <span>{t("settings.skillMarket.uninstall")}</span>
                        </button>
                      )}
                    </div>
                  </div>
                </article>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
