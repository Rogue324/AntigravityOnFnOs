import React, { useEffect, useState } from 'react';
import {
  Download,
  Globe,
  ShieldAlert,
  CheckCircle2,
  AlertCircle,
  Loader2,
  Save,
  KeyRound,
  FolderOpen,
  RefreshCw,
  Terminal,
} from 'lucide-react';
import { api, streamAgyInstall } from '../api/client';
import type { AgyStatus, AppSettings, PermissionMode, ProxyTestResult } from '../types';
import { DirPickerModal } from './DirPickerModal';

interface SettingsViewProps {
  settings: AppSettings | null;
  agyStatus: AgyStatus | null;
  onSettingsSaved: (s: AppSettings) => void;
  onAgyStatusUpdated: (st: AgyStatus) => void;
  onGoToTerminal: () => void;
}

export const SettingsView: React.FC<SettingsViewProps> = ({
  settings,
  agyStatus,
  onSettingsSaved,
  onAgyStatusUpdated,
  onGoToTerminal,
}) => {
  const [form, setForm] = useState({
    http_proxy: '',
    https_proxy: '',
    no_proxy: 'localhost,127.0.0.1,::1',
    default_model: '',
    default_permission_mode: 'default' as PermissionMode,
    allow_dangerous_skip: false,
    default_workspace: '',
    agy_binary_path: '',
  });

  const [saving, setSaving] = useState(false);
  const [saveMsg, setSaveMsg] = useState('');

  const [testingProxy, setTestingProxy] = useState(false);
  const [proxyResult, setProxyResult] = useState<ProxyTestResult | null>(null);

  const [installing, setInstalling] = useState(false);
  const [installLogs, setInstallLogs] = useState<string[]>([]);
  const [installErr, setInstallErr] = useState('');

  const [curPwd, setCurPwd] = useState('');
  const [newPwd, setNewPwd] = useState('');
  const [pwdMsg, setPwdMsg] = useState<{ ok: boolean; text: string } | null>(null);

  const [pickerOpen, setPickerOpen] = useState(false);

  useEffect(() => {
    if (!settings) return;
    setForm({
      http_proxy: settings.http_proxy || '',
      https_proxy: settings.https_proxy || '',
      no_proxy: settings.no_proxy || 'localhost,127.0.0.1,::1',
      default_model: settings.default_model || '',
      default_permission_mode: settings.default_permission_mode || 'default',
      allow_dangerous_skip: !!settings.allow_dangerous_skip,
      default_workspace: settings.default_workspace || '',
      agy_binary_path: settings.agy_binary_path || '',
    });
  }, [settings]);

  const handleSave = async () => {
    setSaving(true);
    setSaveMsg('');
    try {
      const updated = await api.updateSettings(form);
      onSettingsSaved(updated);
      const st = await api.agyStatus();
      onAgyStatusUpdated(st);
      setSaveMsg('设置已保存并生效');
      setTimeout(() => setSaveMsg(''), 3000);
    } catch (err) {
      setSaveMsg(err instanceof Error ? err.message : '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const handleTestProxy = async () => {
    setTestingProxy(true);
    setProxyResult(null);
    try {
      const res = await api.testProxy(form.http_proxy, form.https_proxy);
      setProxyResult(res);
    } catch (err) {
      setProxyResult({
        ok: false,
        latency_ms: 0,
        target_url: 'https://antigravity.google',
        error: err instanceof Error ? err.message : '测试失败',
      });
    } finally {
      setTestingProxy(false);
    }
  };

  const handleInstallAgy = async () => {
    if (installing) return;
    setInstalling(true);
    setInstallLogs([]);
    setInstallErr('');
    try {
      await streamAgyInstall(
        (line) => setInstallLogs((prev) => [...prev, line]),
        (done) => {
          if (done.status) onAgyStatusUpdated(done.status);
          if (!done.ok && done.error) setInstallErr(done.error);
        }
      );
    } catch (err) {
      setInstallErr(err instanceof Error ? err.message : '安装中断');
    } finally {
      setInstalling(false);
    }
  };

  const handleRefreshStatus = async () => {
    try {
      const st = await api.agyStatus();
      onAgyStatusUpdated(st);
    } catch {
      // ignore
    }
  };

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    setPwdMsg(null);
    if (newPwd.length < 6) {
      setPwdMsg({ ok: false, text: '新密码至少需要 6 位字符' });
      return;
    }
    try {
      await api.changePassword(curPwd, newPwd);
      setCurPwd('');
      setNewPwd('');
      setPwdMsg({ ok: true, text: '访问密码已更新' });
    } catch (err) {
      setPwdMsg({ ok: false, text: err instanceof Error ? err.message : '修改失败' });
    }
  };

  return (
    <div className="h-full overflow-y-auto bg-slate-950 p-6">
      <div className="mx-auto max-w-3xl space-y-6 pb-12">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold text-white">设置中心</h1>
            <p className="mt-0.5 text-xs text-slate-400">
              管理 Antigravity CLI 运行环境、网络代理、默认工作区与安全策略
            </p>
          </div>
          <div className="flex items-center gap-3">
            {saveMsg && <span className="text-xs text-emerald-400">{saveMsg}</span>}
            <button
              onClick={handleSave}
              disabled={saving}
              className="flex items-center gap-1.5 rounded-xl bg-indigo-600 px-4 py-2 text-xs font-medium text-white transition hover:bg-indigo-500 disabled:opacity-50"
            >
              {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
              <span>保存全部设置</span>
            </button>
          </div>
        </div>

        {/* 1. agy binary lifecycle */}
        <section className="rounded-2xl border border-slate-800 bg-slate-900/70 p-5">
          <div className="mb-4 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Download className="h-4 w-4 text-indigo-400" />
              <h2 className="text-sm font-semibold text-white">Antigravity CLI (`agy`) 状态与安装</h2>
            </div>
            <button
              onClick={handleRefreshStatus}
              className="flex items-center gap-1 rounded-lg border border-slate-700 px-2.5 py-1 text-xs text-slate-300 hover:bg-slate-800"
            >
              <RefreshCw className="h-3 w-3" />
              <span>刷新检测</span>
            </button>
          </div>

          <div className="grid grid-cols-1 gap-3 rounded-xl border border-slate-800 bg-slate-950/70 p-4 text-xs sm:grid-cols-2">
            <div>
              <div className="text-slate-500">安装状态</div>
              <div className="mt-1 flex items-center gap-1.5 font-medium">
                {agyStatus?.installed ? (
                  <>
                    <CheckCircle2 className="h-4 w-4 text-emerald-400" />
                    <span className="text-emerald-300">已安装</span>
                  </>
                ) : (
                  <>
                    <AlertCircle className="h-4 w-4 text-amber-400" />
                    <span className="text-amber-300">未检测到 agy</span>
                  </>
                )}
              </div>
            </div>

            <div>
              <div className="text-slate-500">登录凭据状态</div>
              <div className="mt-1 flex items-center gap-1.5 font-medium">
                {agyStatus?.authenticated ? (
                  <span className="text-emerald-300">已检测到本地凭据</span>
                ) : (
                  <button
                    onClick={onGoToTerminal}
                    className="flex items-center gap-1 text-indigo-400 hover:underline"
                  >
                    <Terminal className="h-3.5 w-3.5" />
                    <span>前往终端完成首次登录</span>
                  </button>
                )}
              </div>
            </div>

            <div className="sm:col-span-2">
              <div className="text-slate-500">可执行文件路径 / 版本</div>
              <div className="mt-1 font-mono text-slate-300">
                {agyStatus?.binary_path || '—'} {agyStatus?.version ? `(${agyStatus.version})` : ''}
              </div>
            </div>

            <div className="sm:col-span-2">
              <div className="text-slate-500">持久化隔离目录 (TRIM_PKGVAR / HOME)</div>
              <div className="mt-1 font-mono text-slate-400">
                {settings?.home_dir || agyStatus?.home_dir || '—'}
              </div>
            </div>
          </div>

          <div className="mt-4 flex flex-wrap items-center gap-3">
            <button
              onClick={handleInstallAgy}
              disabled={installing}
              className="flex items-center gap-1.5 rounded-xl bg-emerald-600 px-4 py-2 text-xs font-medium text-white transition hover:bg-emerald-500 disabled:opacity-50"
            >
              {installing ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Download className="h-3.5 w-3.5" />
              )}
              <span>{installing ? '正在安装 / 升级...' : agyStatus?.installed ? '升级 agy' : '一键安装 agy'}</span>
            </button>

            <div className="flex flex-1 items-center gap-2">
              <input
                type="text"
                value={form.agy_binary_path}
                onChange={(e) => setForm({ ...form, agy_binary_path: e.target.value })}
                placeholder="可选：手动指定 agy 二进制路径（留空自动探测）"
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-xs text-slate-200 placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
              />
            </div>
          </div>

          {(installLogs.length > 0 || installErr) && (
            <div className="mt-4 rounded-xl border border-slate-800 bg-black/70 p-3 font-mono text-[11px]">
              <div className="max-h-48 overflow-y-auto space-y-0.5 text-slate-300">
                {installLogs.map((l, i) => (
                  <div key={i} className="whitespace-pre-wrap break-all">
                    {l}
                  </div>
                ))}
              </div>
              {installErr && <div className="mt-2 text-rose-400">{installErr}</div>}
            </div>
          )}
        </section>

        {/* 2. Network Proxy */}
        <section className="rounded-2xl border border-slate-800 bg-slate-900/70 p-5">
          <div className="mb-4 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Globe className="h-4 w-4 text-indigo-400" />
              <h2 className="text-sm font-semibold text-white">网络代理配置</h2>
            </div>
            <button
              onClick={handleTestProxy}
              disabled={testingProxy}
              className="flex items-center gap-1.5 rounded-lg border border-slate-700 bg-slate-800 px-3 py-1 text-xs text-slate-200 hover:bg-slate-700 disabled:opacity-50"
            >
              {testingProxy && <Loader2 className="h-3 w-3 animate-spin" />}
              <span>测试连通性</span>
            </button>
          </div>

          <p className="mb-4 text-xs text-slate-400">
            仅注入到本应用拉起的 `agy` 安装脚本、Web Chat 子进程与内置终端，不影响飞牛宿主机全局网络。
          </p>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs text-slate-300">HTTP_PROXY</label>
              <input
                type="text"
                value={form.http_proxy}
                onChange={(e) => setForm({ ...form, http_proxy: e.target.value })}
                placeholder="例如 http://192.168.1.10:7890"
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-xs text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-300">HTTPS_PROXY（留空默认同 HTTP）</label>
              <input
                type="text"
                value={form.https_proxy}
                onChange={(e) => setForm({ ...form, https_proxy: e.target.value })}
                placeholder="例如 http://192.168.1.10:7890"
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-xs text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
              />
            </div>
            <div className="sm:col-span-2">
              <label className="mb-1 block text-xs text-slate-300">NO_PROXY</label>
              <input
                type="text"
                value={form.no_proxy}
                onChange={(e) => setForm({ ...form, no_proxy: e.target.value })}
                placeholder="localhost,127.0.0.1,::1,192.168.0.0/16"
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-xs text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
              />
            </div>
          </div>

          {proxyResult && (
            <div
              className={`mt-3 rounded-xl border px-3 py-2 text-xs ${
                proxyResult.ok
                  ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                  : 'border-rose-500/30 bg-rose-500/10 text-rose-300'
              }`}
            >
              {proxyResult.ok
                ? `连接 ${proxyResult.target_url} 成功（HTTP ${proxyResult.status_code}，延迟 ${proxyResult.latency_ms}ms）`
                : `连接失败（${proxyResult.latency_ms}ms）：${proxyResult.error}`}
            </div>
          )}
        </section>

        {/* 3. Defaults & Safety */}
        <section className="rounded-2xl border border-slate-800 bg-slate-900/70 p-5">
          <div className="mb-4 flex items-center gap-2">
            <ShieldAlert className="h-4 w-4 text-indigo-400" />
            <h2 className="text-sm font-semibold text-white">默认对话与权限策略</h2>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="sm:col-span-2">
              <label className="mb-1 block text-xs text-slate-300">默认工作区目录</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={form.default_workspace}
                  onChange={(e) => setForm({ ...form, default_workspace: e.target.value })}
                  className="flex-1 rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-xs text-white focus:border-indigo-500 focus:outline-none"
                />
                <button
                  onClick={() => setPickerOpen(true)}
                  className="flex items-center gap-1.5 rounded-xl border border-slate-700 bg-slate-800 px-3 py-2 text-xs text-slate-200 hover:bg-slate-700"
                >
                  <FolderOpen className="h-3.5 w-3.5 text-amber-400" />
                  <span>浏览</span>
                </button>
              </div>
            </div>

            <div>
              <label className="mb-1 block text-xs text-slate-300">默认模型（留空使用 CLI 默认）</label>
              <input
                type="text"
                value={form.default_model}
                onChange={(e) => setForm({ ...form, default_model: e.target.value })}
                placeholder="例如 gemini-2.5-pro"
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs text-slate-300">默认执行模式</label>
              <select
                value={form.default_permission_mode}
                onChange={(e) =>
                  setForm({ ...form, default_permission_mode: e.target.value as PermissionMode })
                }
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-white focus:border-indigo-500 focus:outline-none"
              >
                <option value="default">标准模式 (default)</option>
                <option value="accept-edits">自动接受编辑 (accept-edits)</option>
                <option value="plan">仅规划 (plan)</option>
                {form.allow_dangerous_skip && <option value="bypass">跳过权限确认 (危险)</option>}
              </select>
            </div>
          </div>

          <label className="mt-4 flex cursor-pointer items-start gap-2.5 rounded-xl border border-amber-500/20 bg-amber-500/5 p-3 text-xs">
            <input
              type="checkbox"
              checked={form.allow_dangerous_skip}
              onChange={(e) => setForm({ ...form, allow_dangerous_skip: e.target.checked })}
              className="mt-0.5 rounded border-slate-600 bg-slate-900 text-amber-500"
            />
            <div>
              <div className="font-medium text-amber-300">
                允许在 Web Chat 中启用跳过权限确认（`--dangerously-skip-permissions`）
              </div>
              <div className="mt-0.5 text-slate-400">
                高风险选项：开启后 Agent 在授权目录内执行命令和修改文件将不再逐项确认，请确保已在飞牛应用设置中严格限定授权目录。
              </div>
            </div>
          </label>
        </section>

        {/* 4. Change Password */}
        <section className="rounded-2xl border border-slate-800 bg-slate-900/70 p-5">
          <div className="mb-4 flex items-center gap-2">
            <KeyRound className="h-4 w-4 text-indigo-400" />
            <h2 className="text-sm font-semibold text-white">修改应用访问密码</h2>
          </div>

          <form onSubmit={handleChangePassword} className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <input
              type="password"
              value={curPwd}
              onChange={(e) => setCurPwd(e.target.value)}
              placeholder="当前密码"
              className="rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
            />
            <input
              type="password"
              value={newPwd}
              onChange={(e) => setNewPwd(e.target.value)}
              placeholder="新密码（至少 6 位）"
              className="rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
            />
            <button
              type="submit"
              disabled={!curPwd || !newPwd}
              className="rounded-xl border border-slate-700 bg-slate-800 px-4 py-2 text-xs font-medium text-white transition hover:bg-indigo-600 disabled:opacity-40"
            >
              更新密码
            </button>
          </form>

          {pwdMsg && (
            <div className={`mt-2 text-xs ${pwdMsg.ok ? 'text-emerald-400' : 'text-rose-400'}`}>
              {pwdMsg.text}
            </div>
          )}
        </section>
      </div>

      <DirPickerModal
        open={pickerOpen}
        title="选择默认 NAS 工作区目录"
        initialPath={form.default_workspace}
        onClose={() => setPickerOpen(false)}
        onSelect={(chosen) => {
          setForm({ ...form, default_workspace: chosen });
          setPickerOpen(false);
        }}
      />
    </div>
  );
};
