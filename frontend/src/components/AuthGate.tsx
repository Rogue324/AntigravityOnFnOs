import React, { useState } from 'react';
import { Lock, ShieldCheck, Sparkles, ArrowRight } from 'lucide-react';
import { api } from '../api/client';

interface AuthGateProps {
  initialized: boolean;
  onAuthenticated: () => void;
}

export const AuthGate: React.FC<AuthGateProps> = ({ initialized, onAuthenticated }) => {
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
    <div className="flex h-full w-full items-center justify-center bg-slate-950 p-4">
      <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900/90 p-8 shadow-2xl backdrop-blur">
        <div className="mb-6 flex items-center gap-3">
          <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-gradient-to-br from-indigo-500 to-violet-600 shadow-lg shadow-indigo-500/20">
            <Sparkles className="h-6 w-6 text-white" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-white">Antigravity for fnOS</h1>
            <p className="text-xs text-slate-400">飞牛 NAS 原生 AI 助手与终端工作台</p>
          </div>
        </div>

        <div className="mb-6 rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 text-xs text-slate-300">
          {initialized ? (
            <div className="flex items-start gap-2.5">
              <Lock className="mt-0.5 h-4 w-4 shrink-0 text-indigo-400" />
              <span>请输入应用访问密码以进入 Web Chat 与内置终端。</span>
            </div>
          ) : (
            <div className="flex items-start gap-2.5">
              <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-emerald-400" />
              <span>
                首次启动请先设置<strong>应用访问密码</strong>。该密码用于保护 NAS 上的 AI 对话与 Web 终端接口。
              </span>
            </div>
          )}
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="mb-1.5 block text-xs font-medium text-slate-300">
              {initialized ? '访问密码' : '设置访问密码（至少 6 位）'}
            </label>
            <input
              type="password"
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="请输入密码"
              className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2.5 text-sm text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/30"
            />
          </div>

          {!initialized && (
            <div>
              <label className="mb-1.5 block text-xs font-medium text-slate-300">确认密码</label>
              <input
                type="password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                placeholder="请再次输入密码"
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2.5 text-sm text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/30"
              />
            </div>
          )}

          {error && (
            <div className="rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-xs text-rose-300">
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
