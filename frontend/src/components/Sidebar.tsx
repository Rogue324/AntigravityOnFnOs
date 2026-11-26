import React from 'react';
import {
  MessageSquare,
  Terminal,
  Settings,
  Plus,
  Trash2,
  LogOut,
  CheckCircle2,
  AlertCircle,
  Globe,
  Sun,
  Moon,
  X,
} from 'lucide-react';
import type { ActiveTab, AgyStatus, SessionSummary, ThemeMode } from '../types';

interface SidebarProps {
  activeTab: ActiveTab;
  onSelectTab: (tab: ActiveTab) => void;
  sessions: SessionSummary[];
  activeSessionId: string | null;
  onSelectSession: (id: string) => void;
  onNewSession: () => void;
  onDeleteSession: (id: string) => void;
  agyStatus: AgyStatus | null;
  theme: ThemeMode;
  onToggleTheme: () => void;
  onLogout: () => void;
  authDisabled: boolean;
  mobileOpen: boolean;
  onCloseMobile: () => void;
}

const QQPenguinIcon: React.FC<{ className?: string }> = ({ className = 'h-4 w-4' }) => (
  <svg
    viewBox="0 0 24 24"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    className={className}
    aria-label="QQ 会话"
  >
    <path
      d="M12 2.2C8.35 2.2 6.45 4.85 6.45 8.15C6.45 8.85 6.55 9.5 6.72 10.1C5.4 11.45 4.25 13.5 4.25 15.35C4.25 16.45 4.78 16.95 5.38 16.65C5.9 16.4 6.32 15.7 6.6 15.05C6.9 16.25 7.5 17.3 8.38 18.08C7.42 18.52 6.65 19.22 6.65 19.95C6.65 20.78 7.82 21.35 9.5 21.35C10.55 21.35 11.42 21.05 12 20.62C12.58 21.05 13.45 21.35 14.5 21.35C16.18 21.35 17.35 20.78 17.35 19.95C17.35 19.22 16.58 18.52 15.62 18.08C16.5 17.3 17.1 16.25 17.4 15.05C17.68 15.7 18.1 16.4 18.62 16.65C19.22 16.95 19.75 16.45 19.75 15.35C19.75 13.5 18.6 11.45 17.28 10.1C17.45 9.5 17.55 8.85 17.55 8.15C17.55 4.85 15.65 2.2 12 2.2Z"
      fill="#12B7F5"
    />
  </svg>
);

export const Sidebar: React.FC<SidebarProps> = ({
  activeTab,
  onSelectTab,
  sessions,
  activeSessionId,
  onSelectSession,
  onNewSession,
  onDeleteSession,
  agyStatus,
  theme,
  onToggleTheme,
  onLogout,
  authDisabled,
  mobileOpen,
  onCloseMobile,
}) => {
  const sidebarBody = (
    <aside className="flex h-full w-72 max-w-[82vw] shrink-0 flex-col border-r border-slate-200 bg-white lg:w-64 dark:border-slate-800/90 dark:bg-slate-900/95">
      {/* Brand */}
      <div className="flex items-center gap-2.5 border-b border-slate-200 px-4 py-3.5 dark:border-slate-800">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-slate-200 bg-white p-1 shadow-xs dark:border-slate-700 dark:bg-slate-800">
          <img src="./icon.png" alt="Antigravity" className="h-full w-full object-contain" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-slate-900 dark:text-white">
            Antigravity
          </div>
          <div className="text-[11px] text-slate-500 dark:text-slate-400">fnOS 原生工作台</div>
        </div>
        <button
          onClick={onCloseMobile}
          aria-label="关闭菜单"
          className="rounded-lg p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-700 lg:hidden dark:hover:bg-slate-800 dark:hover:text-white"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      {/* Primary navigation */}
      <nav className="space-y-1 p-2.5">
        <button
          onClick={() => onSelectTab('chat')}
          className={`flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-sm font-medium transition md:py-2 ${
            activeTab === 'chat'
              ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-600/20 dark:text-indigo-300'
              : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-slate-800/70 dark:hover:text-white'
          }`}
        >
          <MessageSquare className="h-4 w-4" />
          <span>Web Chat 对话</span>
        </button>

        <button
          onClick={() => onSelectTab('terminal')}
          className={`flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-sm font-medium transition md:py-2 ${
            activeTab === 'terminal'
              ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-600/20 dark:text-indigo-300'
              : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-slate-800/70 dark:hover:text-white'
          }`}
        >
          <Terminal className="h-4 w-4" />
          <span>内置 Web 终端</span>
        </button>

        <button
          onClick={() => onSelectTab('settings')}
          className={`flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-sm font-medium transition md:py-2 ${
            activeTab === 'settings'
              ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-600/20 dark:text-indigo-300'
              : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-slate-800/70 dark:hover:text-white'
          }`}
        >
          <Settings className="h-4 w-4" />
          <span>设置中心</span>
        </button>
      </nav>

      {/* Sessions list */}
      <div className="flex items-center justify-between border-t border-slate-200 px-4 pb-1.5 pt-3 dark:border-slate-800/80">
        <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">
          历史会话
        </span>
        <button
          onClick={onNewSession}
          className="flex items-center gap-1 rounded-lg bg-slate-100 px-2 py-1 text-[11px] font-medium text-slate-700 transition hover:bg-indigo-600 hover:text-white dark:bg-slate-800 dark:text-slate-200"
        >
          <Plus className="h-3 w-3" />
          <span>新对话</span>
        </button>
      </div>

      <div className="flex-1 space-y-0.5 overflow-y-auto px-2.5 py-1.5">
        {sessions.length === 0 ? (
          <div className="px-2 py-6 text-center text-xs text-slate-400 dark:text-slate-500">
            暂无历史会话
          </div>
        ) : (
          sessions.map((sess) => {
            const isSelected = activeTab === 'chat' && sess.id === activeSessionId;
            const rawTitle = sess.title || '新对话';
            const isQQSession = sess.channel === 'qq' || rawTitle.startsWith('[QQ] ');
            const displayTitle = isQQSession ? rawTitle.replace(/^\[QQ\]\s*/, '') || '新对话' : rawTitle;
            return (
              <div
                key={sess.id}
                onClick={() => {
                  onSelectSession(sess.id);
                  onSelectTab('chat');
                }}
                className={`group flex cursor-pointer items-center justify-between rounded-xl px-3 py-2 text-xs transition ${
                  isSelected
                    ? 'bg-slate-100 text-slate-900 dark:bg-slate-800 dark:text-white'
                    : 'text-slate-600 hover:bg-slate-100/70 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800/50 dark:hover:text-slate-200'
                }`}
              >
                <div className="min-w-0 flex-1 pr-2">
                  <div className="flex items-center gap-1.5">
                    {isQQSession && (
                      <span title="QQ 机器人会话" className="shrink-0">
                        <QQPenguinIcon className="h-3.5 w-3.5" />
                      </span>
                    )}
                    <div className="truncate font-medium">{displayTitle}</div>
                  </div>
                  <div className="truncate text-[10px] text-slate-400 dark:text-slate-500">
                    {sess.workspace}
                  </div>
                </div>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onDeleteSession(sess.id);
                  }}
                  title="删除会话"
                  className="rounded p-1 text-slate-400 transition hover:bg-rose-500/15 hover:text-rose-500 md:opacity-0 md:group-hover:opacity-100"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            );
          })
        )}
      </div>

      {/* Bottom status, theme toggle & logout */}
      <div className="space-y-2 border-t border-slate-200 p-3 dark:border-slate-800">
        <div
          onClick={() => onSelectTab('settings')}
          className="cursor-pointer rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-[11px] transition hover:border-slate-300 dark:border-slate-800 dark:bg-slate-950/70 dark:hover:border-slate-700"
        >
          <div className="flex items-center justify-between">
            <span className="font-medium text-slate-700 dark:text-slate-300">agy 状态</span>
            {agyStatus?.installed ? (
              <span className="flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
                <CheckCircle2 className="h-3.5 w-3.5" />
                已就绪
              </span>
            ) : (
              <span className="flex items-center gap-1 text-amber-600 dark:text-amber-400">
                <AlertCircle className="h-3.5 w-3.5" />
                未安装
              </span>
            )}
          </div>
          <div className="mt-1 flex items-center justify-between text-slate-500">
            <span className="truncate">
              {agyStatus?.authenticated ? '已检测到登录凭据' : '请至终端完成登录'}
            </span>
            {agyStatus?.proxy_active && (
              <span title="已启用代理" className="text-indigo-500 dark:text-indigo-400">
                <Globe className="h-3 w-3" />
              </span>
            )}
          </div>
        </div>

        <div className={`grid gap-1.5 ${authDisabled ? 'grid-cols-1' : 'grid-cols-2'}`}>
          <button
            onClick={onToggleTheme}
            className="flex items-center justify-center gap-1.5 rounded-xl border border-slate-200 bg-white py-2 text-xs text-slate-600 transition hover:bg-slate-100 hover:text-slate-900 md:py-1.5 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200"
          >
            {theme === 'light' ? (
              <>
                <Moon className="h-3.5 w-3.5" />
                <span>深色模式</span>
              </>
            ) : (
              <>
                <Sun className="h-3.5 w-3.5 text-amber-400" />
                <span>浅色模式</span>
              </>
            )}
          </button>

          {!authDisabled && (
            <button
              onClick={onLogout}
              className="flex items-center justify-center gap-1.5 rounded-xl border border-slate-200 bg-white py-2 text-xs text-slate-600 transition hover:bg-slate-100 hover:text-slate-900 md:py-1.5 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200"
            >
              <LogOut className="h-3.5 w-3.5" />
              <span>锁定</span>
            </button>
          )}
        </div>
      </div>
    </aside>
  );

  return (
    <>
      {/* Desktop persistent sidebar (>= 1024px) */}
      <div className="hidden h-full lg:flex">{sidebarBody}</div>

      {/* Mobile / narrow-window slide-over drawer (< 1024px) */}
      {mobileOpen && (
        <div className="fixed inset-0 z-40 flex lg:hidden">
          <div
            className="fixed inset-0 bg-black/50 backdrop-blur-xs"
            onClick={onCloseMobile}
          />
          <div className="relative z-50 flex h-full">{sidebarBody}</div>
        </div>
      )}
    </>
  );
};
