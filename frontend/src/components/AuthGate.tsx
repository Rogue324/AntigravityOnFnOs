import React, { useState } from 'react';
import { Lock, ShieldCheck, Sparkles, ArrowRight, Sun, Moon } from 'lucide-react';
import { api } from '../api/client';
import type { ThemeMode } from '../types';

interface AuthGateProps {
  initialized: boolean;
  theme: ThemeMode;
  onToggleTheme: () => void;
  onAuthenticated: () => void;
}

export const AuthGate: React.FC<AuthGateProps> = ({
  initialized,
  theme,
  onToggleTheme,
  onAuthenticated,
}) => {
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    if (!initialized) {
      if (password.length < 6) {
        setError('访问密码至少需要 6 位字符');
        return;
      }
      if (password !== confirm) {
        setError('两次输入的密码不一致');
        return;
      }
    }

    setLoading(true);
    try {
      if (initialized) {
        await api.login(password);
      } else {
        await api.setupPassword(password);
      }
      onAuthenticated();
    } catch (err) {
      setError(err instanceof Error ? err.message : '认证失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="relative flex h-full w-full items-center justify-center bg-slate-50 p-4 dark:bg-slate-950">
      <button
        onClick={onToggleTheme}
        title={theme === 'light' ? '切换到深色模式' : '切换到浅色模式'}
        className="absolute right-4 top-4 flex items-center gap-1.5 rounded-xl border border-slate-200 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 shadow-sm transition hover:bg-slate-100 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-300 dark:hover:bg-slate-800"
      >
        {theme === 'light' ? <Moon className="h-3.5 w-3.5" /> : <Sun className="h-3.5 w-3.5 text-amber-400" />}
        <span>{theme === 'light' ? '深色模式' : '浅色模式'}</span>
      </button>

      <div className="w-full max-w-md rounded-2xl border border-slate-200 bg-white p-8 shadow-xl dark:border-slate-800 dark:bg-slate-900/90 dark:shadow-2xl">
        <div className="mb-6 flex items-center gap-3">
          <div className="flex h-12 w-12 items-center justify-center rounded-xl border border-slate-200 bg-white p-2 shadow-sm dark:border-slate-700 dark:bg-slate-800">
            <img src="./icon.png" alt="Antigravity" className="h-full w-full object-contain" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-slate-900 dark:text-white">Antigravity for fnOS</h1>
            <p className="text-xs text-slate-500 dark:text-slate-400">飞牛 NAS 原生 AI 助手与终端工作台</p>
          </div>
        </div>

        <div className="mb-6 rounded-xl border border-slate-200 bg-slate-50 p-3.5 text-xs text-slate-600 dark:border-slate-800 dark:bg-slate-950/60 dark:text-slate-300">
          {initialized ? (
            <div className="flex items-start gap-2.5">
              <Lock className="mt-0.5 h-4 w-4 shrink-0 text-indigo-500 dark:text-indigo-400" />
              <span>请输入应用访问密码以进入 Web Chat 与内置终端。</span>
            </div>
          ) : (
            <div className="flex items-start gap-2.5">
              <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-emerald-500 dark:text-emerald-400" />
              <span>
                首次启动请先设置<strong>应用访问密码</strong>。该密码用于保护 NAS 上的 AI 对话与 Web 终端接口。
              </span>
            </div>
          )}
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="mb-1.5 block text-xs font-medium text-slate-700 dark:text-slate-300">
              {initialized ? '访问密码' : '设置访问密码（至少 6 位）'}
            </label>
            <input
              type="password"
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="请输入密码"
              className="w-full rounded-xl border border-slate-300 bg-white px-3.5 py-2.5 text-sm text-slate-900 placeholder-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/25 dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:placeholder-slate-500"
            />
          </div>

          {!initialized && (
            <div>
              <label className="mb-1.5 block text-xs font-medium text-slate-700 dark:text-slate-300">
                确认密码
              </label>
              <input
                type="password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                placeholder="请再次输入密码"
                className="w-full rounded-xl border border-slate-300 bg-white px-3.5 py-2.5 text-sm text-slate-900 placeholder-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/25 dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:placeholder-slate-500"
              />
            </div>
          )}

          {error && (
            <div className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-600 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-300">
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading || !password}
            className="flex w-full items-center justify-center gap-2 rounded-xl bg-indigo-600 px-4 py-2.5 text-sm font-medium text-white transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span>{loading ? '请稍候...' : initialized ? '解锁进入' : '设置密码并进入'}</span>
            <ArrowRight className="h-4 w-4" />
          </button>
        </form>
      </div>
    </div>
  );
};
