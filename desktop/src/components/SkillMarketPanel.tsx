import { useCallback, useEffect, useRef, useState } from "react";
import {
  marketSearch,
  marketSections,
  marketInstalled,
  marketInstallStart,
  marketUninstall,
  subscribeInstallJob,
  formatBytes,
  type MarketSkill,
  type MarketSection,
  type InstalledSkill,
  type InstallJobState,
} from "../client/skill-market";
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

type InstallState =
  | { kind: "idle" }
  | { kind: "running"; jobId: string; phase: string; pct: number | null }
  | { kind: "succeeded"; name: string }
  | { kind: "failed"; message: string };

export function SkillMarketPanel() {
  const t = useT();

  const [query, setQuery] = useState("");
  const [sort, setSort] = useState("featured");
  const [skills, setSkills] = useState<MarketSkill[]>([]);
  const [sections, setSections] = useState<MarketSection[]>([]);
  const [section, setSection] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);

  const [installed, setInstalled] = useState<InstalledSkill[]>([]);
  // Install state keyed by skill id; one install per skill at a time.
  const [installs, setInstalls] = useState<Record<number, InstallState>>({});
  const [confirmName, setConfirmName] = useState<string | null>(null);
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
      setLoading(true);
      setError(null);
      try {
        const p = opts?.page ?? 1;
        const res = await marketSearch({
          q: query.trim() || undefined,
          sort,
          page: p,
        });
        setSkills(res.skills);
        setTotal(res.total);
        setPage(p);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    },
    [query, sort],
  );

  useEffect(() => {
    void runSearch();
    void marketSections()
      .then(setSections)
      .catch(() => setSections([]));
    void refreshInstalled();
    const subs = subsRef.current;
    return () => {
      subs.forEach((cancel) => cancel());
      subs.clear();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const watchInstall = useCallback(
    (skillId: number, jobId: string) => {
      setInstalls((m) => ({ ...m, [skillId]: { kind: "running", jobId, phase: "resolving", pct: null } }));
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
        const { job_id } = await marketInstallStart(skill.id);
        watchInstall(skill.id, job_id);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        setInstalls((m) => ({ ...m, [skill.id]: { kind: "failed", message: msg } }));
      }
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

  return (
    <div className="settings-section skill-market">
      <div className="skill-market-toolbar">
        <form
          className="skill-market-search"
          onSubmit={(e) => {
            e.preventDefault();
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
          <select value={sort} onChange={(e) => { setSort(e.target.value); }}>
            <option value="featured">{t("settings.skillMarket.sortFeatured")}</option>
            <option value="installs">{t("settings.skillMarket.sortInstalls")}</option>
            <option value="name">{t("settings.skillMarket.sortName")}</option>
          </select>
        </form>
        {sections.length > 0 && (
          <div className="skill-market-sections" role="tablist">
            <button
              className={section === null ? "active" : ""}
              onClick={() => { setSection(null); }}
            >
              {t("settings.skillMarket.allSections")}
            </button>
            {sections.map((s) => (
              <button key={s.id} className={section === s.id ? "active" : ""} onClick={() => setSection(s.id)}>
                {s.name}
              </button>
            ))}
          </div>
        )}
      </div>

      {error && (
        <div className="skill-market-error" role="alert">
          <Icon name="alert" />
          <span>{error}</span>
          <button onClick={() => setError(null)} aria-label={t("common.dismiss")}>
            <Icon name="x" />
          </button>
        </div>
      )}

      {loading && <div className="skill-market-loading">{t("settings.skillMarket.loading")}</div>}

      <ul className="skill-market-list">
        {skills.map((s) => {
          const st = installs[s.id] ?? { kind: "idle" as const };
          const isInstalled = installedNames.has(s.name);
          return (
            <li key={s.id} className="skill-market-item">
              <div className="skill-market-item-main">
                <div className="skill-market-item-title">
                  <strong>{s.display_name}</strong>
                  <span className="skill-market-item-meta">v{s.version}</span>
                  <span className="skill-market-item-meta">
                    {t("settings.skillMarket.installs", { count: s.install_count })}
                  </span>
                  {formatBytes(s.size_bytes) && (
                    <span className="skill-market-item-meta">{formatBytes(s.size_bytes)}</span>
                  )}
                </div>
                <p className="skill-market-item-desc">{s.description}</p>
                {st.kind === "running" && (
                  <div className="skill-market-progress" aria-busy="true">
                    <div className="skill-market-progress-bar">
                      <div
                        className="skill-market-progress-fill"
                        style={{ width: st.pct === null ? "40%" : `${st.pct}%` }}
                        data-indeterminate={st.pct === null || undefined}
                      />
                    </div>
                    <span className="skill-market-progress-label">
                      {PHASE_LABEL[st.phase]
                        ? t(PHASE_LABEL[st.phase])
                        : st.phase}
                      {st.pct !== null ? ` ${st.pct}%` : ""}
                    </span>
                  </div>
                )}
                {st.kind === "succeeded" && (
                  <div className="skill-market-flash ok">{t("settings.skillMarket.installDone")}</div>
                )}
                {st.kind === "failed" && (
                  <div className="skill-market-flash err">{st.message}</div>
                )}
              </div>
              <div className="skill-market-item-actions">
                {isInstalled ? (
                  confirmName === s.name ? (
                    <>
                      <button className="danger" onClick={() => void uninstall(s.name)}>
                        {t("settings.skillMarket.confirmUninstall")}
                      </button>
                      <button onClick={() => setConfirmName(null)}>
                        {t("common.cancel")}
                      </button>
                    </>
                  ) : (
                    <span className="skill-market-installed-badge">
                      <Icon name="check" /> {t("settings.skillMarket.installedBadge")}
                    </span>
                  )
                ) : st.kind === "running" ? (
                  <button disabled aria-busy="true">
                    {t("settings.skillMarket.installing")}
                  </button>
                ) : (
                  <button onClick={() => void install(s)}>
                    {t("settings.skillMarket.install")}
                  </button>
                )}
                {isInstalled && confirmName !== s.name && (
                  <button
                    className="ghost"
                    title={t("settings.skillMarket.uninstall")}
                    onClick={() => setConfirmName(s.name)}
                  >
                    <Icon name="delete" />
                  </button>
                )}
              </div>
            </li>
          );
        })}
      </ul>

      {!loading && skills.length === 0 && !error && (
        <div className="skill-market-empty">{t("settings.skillMarket.empty")}</div>
      )}

      {total > skills.length && (
        <div className="skill-market-pager">
          <button disabled={page <= 1} onClick={() => void runSearch({ page: page - 1 })}>
            <Icon name="chevron-right" className="flip-x" />
          </button>
          <span>
            {t("settings.skillMarket.pageOf", { page, pages: Math.max(1, Math.ceil(total / skills.length)) })}
          </span>
          <button
            disabled={page >= Math.ceil(total / Math.max(1, skills.length))}
            onClick={() => void runSearch({ page: page + 1 })}
          >
            <Icon name="chevron-right" />
          </button>
        </div>
      )}
    </div>
  );
}
