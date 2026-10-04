import { useStore } from '../store';
import { Icon } from './Icon';
import { useT } from '../i18n/useT';

export function UpdateBanner() {
  const t = useT();
  const update = useStore((s) => s.update);
  const download = useStore((s) => s.downloadUpdate);
  const snooze = useStore((s) => s.snoozeUpdate);

  if (!update || !update.supported || update.snoozed) return null;
  if (update.phase !== 'available' && update.phase !== 'error') return null;
  if (!update.info && update.phase !== 'error') return null;

  const version = update.info?.version ?? '';

  return (
    <div className="update-toast" role="status">
      <div className="update-toast-head">
        <span className="update-toast-icon">
          <Icon name={update.phase === 'error' ? 'alert' : 'sparkle'} size={15} />
        </span>
        <div className="update-toast-titles">
          <div className="update-toast-title">
            {update.phase === 'error'
              ? t('update.failed')
              : t('update.available', { version })}
          </div>
          <div className="update-toast-sub">
            {update.phase === 'error'
              ? update.error
              : t('update.currentVersion', { version: update.currentVersion ?? '' })}
          </div>
        </div>
        <button
          className="icon-btn update-toast-x"
          title={t('update.later')}
          onClick={() => snooze()}
        >
          <Icon name="x" size={14} />
        </button>
      </div>
      <div className="update-toast-actions">
        {update.phase === 'available' && (
          <>
            <button className="btn ghost sm" onClick={() => snooze()}>
              {t('update.later')}
            </button>
            <button className="btn primary sm" onClick={() => void download()}>
              <Icon name="send" size={13} />
              {t('update.updateNow')}
            </button>
          </>
        )}
        {update.phase === 'error' && (
          <>
            <button
              className="btn ghost sm"
              onClick={() => void window.piAPI?.openDownloadPage('https://github.com/hwj123hwj/easyagent/releases/latest')}
            >
              {t('update.releasePage')}
            </button>
            <button className="btn primary sm" onClick={() => void download()}>
              <Icon name="refresh" size={13} />
              {t('update.retry')}
            </button>
          </>
        )}
      </div>
    </div>
  );
}
