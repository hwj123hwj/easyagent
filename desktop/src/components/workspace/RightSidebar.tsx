/**
 * RightSidebar.tsx — The right feature sidebar (Codex/VSCode-style):
 * a vertical function rail plus a content panel showing the active feature.
 * Rail entries: Review / Files / Plan / Tasks.
 */

import type { ReactNode } from 'react';
import { useStore, type RightView } from '../../store';
import { Icon, type IconName } from '../Icon';
import { useT, type TFunc } from '../../i18n/useT';
import { ReviewPanel } from './ReviewPanel';
import { FilesPanel } from './FilesPanel';
import { PlanPanel } from './PlanPanel';
import { TasksPanel } from './TasksPanel';
import { KbPanel } from './KbPanel';
import { ProfilePanel } from './ProfilePanel';

interface RailItem {
  key: string;
  icon: IconName;
  labelKey: 'workspace.review' | 'workspace.files' | 'workspace.plan' | 'workspace.tasks' | 'workspace.kb' | 'workspace.profile';
  shortcut: string;
  view: RightView;
}

const RAIL: RailItem[] = [
  { key: 'review', icon: 'review', labelKey: 'workspace.review', shortcut: '⇧G', view: 'review' },
  { key: 'files', icon: 'folder', labelKey: 'workspace.files', shortcut: 'P', view: 'files' },
  { key: 'plan', icon: 'plan', labelKey: 'workspace.plan', shortcut: '', view: 'plan' },
  { key: 'tasks', icon: 'tasks', labelKey: 'workspace.tasks', shortcut: '', view: 'tasks' },
  { key: 'kb', icon: 'book', labelKey: 'workspace.kb', shortcut: '', view: 'kb' },
  { key: 'profile', icon: 'user', labelKey: 'workspace.profile', shortcut: '', view: 'profile' },
];

export function RightSidebar() {
  const rightView = useStore((s) => s.workspace.rightView);
  const rightWidth = useStore((s) => s.workspace.rightWidth);
  const toggleView = useStore((s) => s.toggleWorkspaceView);
  const t = useT();
  if (!rightView) return null;

  let content: ReactNode = null;
  if (rightView === 'review') content = <ReviewPanel />;
  else if (rightView === 'files') content = <FilesPanel />;
  else if (rightView === 'plan') content = <PlanPanel />;
  else if (rightView === 'tasks') content = <TasksPanel />;
  else if (rightView === 'kb') content = <KbPanel />;
  else if (rightView === 'profile') content = <ProfilePanel />;

  return (
    <div
      className="rsidebar"
      style={{ flexBasis: rightWidth, width: rightWidth }}
    >
      <button
        className="rsidebar-mobile-close"
        onClick={() => useStore.getState().toggleWorkspaceRight()}
        aria-label={t('common.close')}
      >
        <Icon name="arrow-left" size={18} />
      </button>
      <div className="rsidebar-content">
        {content}
      </div>
      <RightRail
        items={RAIL}
        rightView={rightView}
        onSelect={(item) => toggleView(item.view)}
        t={t}
      />
    </div>
  );
}

function RightRail({
  items,
  rightView,
  onSelect,
  t,
}: {
  items: RailItem[];
  rightView: RightView | null;
  onSelect: (item: RailItem) => void;
  t: TFunc;
}) {
  return (
    <nav className="rsidebar-rail collapsed">
      {items.map((item) => {
        const active = rightView === item.view;
        return (
          <button
            key={item.key}
            className={`rail-item ${active ? 'active' : ''}`}
            onClick={() => onSelect(item)}
            title={item.shortcut ? `${t(item.labelKey)} · ⌘${item.shortcut}` : t(item.labelKey)}
          >
            <Icon name={item.icon} size={18} />
            <span className="rail-label">{t(item.labelKey)}</span>
            {item.shortcut && <span className="rail-shortcut">{item.shortcut}</span>}
          </button>
        );
      })}
    </nav>
  );
}
