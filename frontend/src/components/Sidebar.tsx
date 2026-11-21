import React from 'react';
import {
  MessageSquare,
  Terminal,
  Settings,
  Plus,
  Trash2,
  Sparkles,
  LogOut,
  CheckCircle2,
  AlertCircle,
  Globe,
} from 'lucide-react';
import type { ActiveTab, AgyStatus, SessionSummary } from '../types';

interface SidebarProps {
  activeTab: ActiveTab;
  onSelectTab: (tab: ActiveTab) => void;
  sessions: SessionSummary[];
  activeSessionId: string | null;
  onSelectSession: (id: string) => void;
  onNewSession: () => void;
  onDeleteSession: (id: string) => void;
  agyStatus: AgyStatus | null;
  onLogout: () => void;
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
  onLogout,
}) => {
  return (
    <aside className="flex h-full w-64 shrink-0 flex-col border-r border-slate-800/90 bg-slate-900/95">
      {/* Brand */}
      <div className="flex items-center gap-2.5 border-b border-slate-800 px-4 py-3.5">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-indigo-500 to-violet-600 shadow shadow-indigo-500/20">
          <Sparkles className="h-4 w-4 text-white" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-white">Antigravity</div>
          <div className="text-[11px] text-slate-400">fnOS 原生工作台</div>
        </div>
      </div>

      {/* Primary navigation */}
      <nav className="space-y-1 p-2.5">
        <button
          onClick={() => onSelectTab('chat')}
          className={`flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm font-medium transition ${
            activeTab === 'chat'
              ? 'bg-indigo-600/20 text-indigo-300'
              : 'text-slate-300 hover:bg-slate-800/70 hover:text-white'
          }`}
        >
          <MessageSquare className="h-4 w-4" />
          <span>Web Chat 对话</span>
        </button>

        <button
          onClick={() => onSelectTab('terminal')}
          className={`flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm font-medium transition ${
            activeTab === 'terminal'
              ? 'bg-indigo-600/20 text-indigo-300'
              : 'text-slate-300 hover:bg-slate-800/70 hover:text-white'
          }`}
        >
          <Terminal className="h-4 w-4" />
          <span>内置 Web 终端</span>
        </button>

        <button
          onClick={() => onSelectTab('settings')}
          className={`flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm font-medium transition ${
            activeTab === 'settings'
              ? 'bg-indigo-600/20 text-indigo-300'
              : 'text-slate-300 hover:bg-slate-800/70 hover:text-white'
          }`}
        >
          <Settings className="h-4 w-4" />
          <span>设置中心</span>
        </button>
      </nav>

      {/* Sessions list */}
      <div className="flex items-center justify-between border-t border-slate-800/80 px-4 pb-1.5 pt-3">
        <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">
          历史会话
        </span>
        <button
          onClick={onNewSession}
          className="flex items-center gap-1 rounded-lg bg-slate-800 px-2 py-1 text-[11px] font-medium text-slate-200 transition hover:bg-indigo-600 hover:text-white"
        >
          <Plus className="h-3 w-3" />
          <span>新对话</span>
        </button>
      </div>

      <div className="flex-1 space-y-0.5 overflow-y-auto px-2.5 py-1.5">
        {sessions.length === 0 ? (
          <div className="px-2 py-6 text-center text-xs text-slate-500">暂无历史会话</div>
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
                    ? 'bg-slate-800 text-white'
                    : 'text-slate-400 hover:bg-slate-800/50 hover:text-slate-200'
                }`}
              >
                <div className="min-w-0 flex-1 pr-2">
                  <div className="truncate font-medium">{sess.title || '新对话'}</div>
                  <div className="truncate text-[10px] text-slate-500">{sess.workspace}</div>
                </div>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onDeleteSession(sess.id);
                  }}
                  title="删除会话"
                  className="rounded p-1 text-slate-500 opacity-0 transition hover:bg-rose-500/20 hover:text-rose-400 group-hover:opacity-100"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            );
          })
        )}
      </div>

      {/* Bottom status & logout */}
      <div className="border-t border-slate-800 p-3">
        <div
          onClick={() => onSelectTab('settings')}
          className="mb-2 cursor-pointer rounded-xl border border-slate-800 bg-slate-950/70 p-2.5 text-[11px] transition hover:border-slate-700"
        >
          <div className="flex items-center justify-between">
            <span className="font-medium text-slate-300">agy 状态</span>
            {agyStatus?.installed ? (
              <span className="flex items-center gap-1 text-emerald-400">
                <CheckCircle2 className="h-3.5 w-3.5" />
                已就绪
              </span>
            ) : (
              <span className="flex items-center gap-1 text-amber-400">
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
              <span title="已启用代理" className="text-indigo-400">
                <Globe className="h-3 w-3" />
              </span>
            )}
          </div>
        </div>

        <button
          onClick={onLogout}
          className="flex w-full items-center justify-center gap-1.5 rounded-xl border border-slate-800 py-1.5 text-xs text-slate-400 transition hover:border-slate-700 hover:bg-slate-800 hover:text-slate-200"
        >
          <LogOut className="h-3.5 w-3.5" />
          <span>锁定 / 退出登录</span>
        </button>
      </div>
    </aside>
  );
};
