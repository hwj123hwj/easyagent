import { app, utilityProcess } from 'electron';
import * as path from 'path';
export interface DesktopUpdateState {
  supported: boolean; enabled: boolean; manualDownload?: boolean;
  phase: 'idle' | 'downloading' | 'verifying' | 'signing' | 'ready' | 'installing' | 'error';
  version?: string; error?: string; message?: string;
}
export function desktopUpdater(publish: (state: DesktopUpdateState) => void) {
  let state: DesktopUpdateState = { supported: false, enabled: true, phase: 'idle' }, sequence = 0;
  const calls = new Map<number, { resolve: (value: DesktopUpdateState) => void; reject: (error: Error) => void; timer: ReturnType<typeof setTimeout> }>();
  const root = path.join(app.getAppPath(), 'scripts');
  const worker = utilityProcess.fork(path.join(root, 'desktop-update-worker.cjs'), [], { serviceName: 'EasyAgent Update', stdio: 'ignore' });
  worker.on('message', value => {
    if (value.state) { state = value.state; publish(state); }
    const call = calls.get(value.id);
    if (call) { calls.delete(value.id); clearTimeout(call.timer); value.error ? call.reject(Error(value.error)) : call.resolve(value.result); }
  });
  worker.on('exit', () => {
    for (const call of calls.values()) { clearTimeout(call.timer); call.reject(Error('更新进程已停止，当前应用未改动')); } calls.clear();
    state = { ...state, supported: false, phase: 'error', error: '更新进程已停止，请重新启动应用后重试' }; publish(state);
  });
  function call(command: string, ...args: unknown[]): Promise<DesktopUpdateState> {
    const id = ++sequence;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { calls.delete(id); reject(Error('更新操作超时，当前版本保留')); }, command === 'prepare' ? 15 * 60 * 1000 : 45000);
      calls.set(id, { resolve, reject, timer }); worker.postMessage({ id, command, args });
    });
  }
  const ready = call('init', { target: path.resolve(app.getAppPath(), '../../..'), platform: process.platform, arch: process.arch,
    version: app.getVersion(), userData: app.getPath('userData'), nodePath: path.join(process.resourcesPath, 'runtime/bin/node') }).then(value => { state = value; publish(state); }).catch(() => {});
  return {
    status: async () => { await ready; return state; },
    prepare: async (version: string) => { await ready; return call('prepare', version); },
    setEnabled: async (value: boolean) => { await ready; return call('setEnabled', value); },
    cancelHandoff: async () => call('cancelHandoff'),
    handoff: async (requirePermissions: boolean, automatic: boolean) => { await ready; return call('handoff', process.pid, requirePermissions, automatic); },
  };
}
