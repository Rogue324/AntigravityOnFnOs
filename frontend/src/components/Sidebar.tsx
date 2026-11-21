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
  PanelLeftClose,
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
  mobileOpen: boolean;
  onCloseMobile: () => void;
  desktopCollapsed: boolean;
  onCollapseDesktop: () => void;
}

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
  mobileOpen,
  onCloseMobile,
  desktopCollapsed,
  onCollapseDesktop,
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
        <button
          onClick={onCollapseDesktop}
          title="收起侧边栏"
          className="hidden rounded-lg p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-700 lg:inline-flex dark:hover:bg-slate-800 dark:hover:text-white"
        >
          <PanelLeftClose className="h-4 w-4" />
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
                  <div className="truncate font-medium">{sess.title || '新对话'}</div>
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

        <div className="grid grid-cols-2 gap-1.5">
          <button
            onClick={onToggleTheme}
            className="flex items-center justify-center gap-1.5 rounded-xl border border-slate-200 bg-white py-2 text-xs text-slate-600 transition hover:bg-slate-100 hover:text-slate-900 md:py-1.5 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200"
          >
            {theme === 'light' ? (
              <>
                <Moon className="h-3.5 w-3.5" />
                <span>深色</span>
              </>
            ) : (
              <>
                <Sun className="h-3.5 w-3.5 text-amber-400" />
                <span>浅色</span>
              </>
            )}
          </button>

          <button
            onClick={onLogout}
            className="flex items-center justify-center gap-1.5 rounded-xl border border-slate-200 bg-white py-2 text-xs text-slate-600 transition hover:bg-slate-100 hover:text-slate-900 md:py-1.5 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200"
          >
            <LogOut className="h-3.5 w-3.5" />
            <span>锁定</span>
          </button>
        </div>
      </div>
    </aside>
  );

  return (
    <>
      {/* Desktop sidebar (>= 1024px unless collapsed) */}
      {!desktopCollapsed && <div className="hidden h-full lg:flex">{sidebarBody}</div>}

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
