import { useEffect, useState } from 'react';
import type { DesktopAutoUpdateState } from '../types';
const messages: Record<DesktopAutoUpdateState['phase'], string> = {
  idle: '自动检查正式版本，复用本机固定签名身份。', downloading: '正在后台下载更新…', verifying: '正在校验安装包…',
  signing: '正在复用固定证书签名…', ready: '更新已准备好；离开应用且任务、终端和未保存编辑结束后自动安装。',
  installing: '正在安装并重启…', error: '更新未完成，当前应用和授权已保留。',
};
export function DesktopUpdateSettings() {
  const [state, setState] = useState<DesktopAutoUpdateState>();
  useEffect(() => {
    void window.piAPI?.desktopUpdateStatus?.().then(setState);
    return window.piAPI?.onDesktopUpdate?.(setState);
  }, []);
  if (!state) return null;
  return <label className="settings-preference-row"><span><strong>自动更新</strong>
    <small>{state.error || state.message || messages[state.phase]}</small>
    <small>后台下载，在应用空闲时安装并重启；升级保留现有电脑控制授权。</small>
  </span><input type="checkbox" checked={state.enabled} disabled={!state.supported || state.phase === 'installing'}
    aria-label="自动更新" onChange={event => void window.piAPI?.setDesktopAutoUpdate(event.target.checked).then(setState)} /></label>;
}
