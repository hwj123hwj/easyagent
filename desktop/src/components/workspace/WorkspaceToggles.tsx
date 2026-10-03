/**
 * WorkspaceToggles.tsx — The top-right layout toggle buttons (Codex-style):
 * show/hide the left session sidebar, the right feature sidebar, and the
 * bottom terminal panel.
 */

import { useStore } from '../../store';
import { Icon } from '../Icon';
import { useT } from '../../i18n/useT';
import { isElectron } from '../../platform';

export function WorkspaceToggles({ variant = 'inline' }: { variant?: 'inline' | 'menu' }) {
  const workspace = useStore((s) => s.workspace);
  const toggleSidebar = useStore((s) => s.toggleSidebar);
  const toggleBottom = useStore((s) => s.toggleWorkspaceBottom);
  const toggleRight = useStore((s) => s.toggleWorkspaceRight);
  const t = useT();

  return (
    <div className={`ws-toggles ${variant === 'menu' ? 'ws-toggles-menu' : ''}`} role="group" aria-label="工作区布局">
      {/* Sidebar toggle: hidden on mobile (use hamburger button instead) */}
      {isElectron && (
        <button
          className={`ws-toggle ${workspace.sidebarOpen ? 'active' : ''}`}
          title={workspace.sidebarOpen ? t('sidebar.collapse') : t('sidebar.expand')}
          aria-pressed={workspace.sidebarOpen}
          aria-label={workspace.sidebarOpen ? t('sidebar.collapse') : t('sidebar.expand')}
          onClick={toggleSidebar}
        >
          <Icon name="panel" size={16} />
          {variant === 'menu' && <span>会话列表</span>}
          {variant === 'menu' && workspace.sidebarOpen && <Icon name="check" size={14} />}
        </button>
      )}
      {/* Bottom terminal toggle: hidden on mobile */}
      {isElectron && (
        <button
          className={`ws-toggle ${workspace.bottomOpen ? 'active' : ''}`}
          title={t('workspace.toggleBottom')}
          aria-pressed={workspace.bottomOpen}
          aria-label={t('workspace.toggleBottom')}
          onClick={toggleBottom}
        >
          <Icon name="panel-bottom" size={16} />
          {variant === 'menu' && <span>底部终端</span>}
          {variant === 'menu' && workspace.bottomOpen && <Icon name="check" size={14} />}
        </button>
      )}
      <button
        className={`ws-toggle ${workspace.rightOpen ? 'active' : ''}`}
        title={t('workspace.toggleRight')}
        aria-pressed={workspace.rightOpen}
        aria-label={t('workspace.toggleRight')}
        onClick={toggleRight}
      >
        <Icon name="panel-right" size={16} />
        {variant === 'menu' && <span>工作台</span>}
        {variant === 'menu' && workspace.rightOpen && <Icon name="check" size={14} />}
      </button>
    </div>
  );
}
