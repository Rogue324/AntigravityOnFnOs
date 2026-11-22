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
  Radio,
  ExternalLink,
  RotateCw,
} from 'lucide-react';
import { api, streamAgyInstall } from '../api/client';
import type {
  AgyStatus,
  AppSettings,
  ModelOption,
  PermissionMode,
  ProxyTestResult,
  RemoteControlStatus,
} from '../types';
import { DirPickerModal } from './DirPickerModal';

interface SettingsViewProps {
  settings: AppSettings | null;
  agyStatus: AgyStatus | null;
  models: ModelOption[];
  onSettingsSaved: (s: AppSettings) => void;
  onAgyStatusUpdated: (st: AgyStatus) => void;
  onGoToTerminal: () => void;
}

export const SettingsView: React.FC<SettingsViewProps> = ({
  settings,
  agyStatus,
  models,
  onSettingsSaved,
  onAgyStatusUpdated,
  onGoToTerminal,
}) => {
  const [form, setForm] = useState({
    http_proxy: '',
    https_proxy: '',
    no_proxy: 'localhost,127.0.0.1,::1',
    default_model: '',
    default_effort: '',
    default_permission_mode: 'default' as PermissionMode,
    allow_dangerous_skip: false,
    default_workspace: '',
    agy_binary_path: '',
    persistent_idle_minutes: 10,
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

  const [rcStatus, setRcStatus] = useState<RemoteControlStatus | null>(null);
  const [rcBusy, setRcBusy] = useState(false);
  const [rcMsg, setRcMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [showRcLogs, setShowRcLogs] = useState(false);

  const fetchRcStatus = async () => {
    try {
      const st = await api.remoteControlStatus();
      setRcStatus(st);
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    fetchRcStatus();
  }, []);

  useEffect(() => {
    const active = settings?.remote_control_enabled || rcStatus?.enabled;
    if (!active) return;
    const timer = window.setInterval(() => {
      fetchRcStatus();
    }, 4000);
    return () => window.clearInterval(timer);
  }, [settings?.remote_control_enabled, rcStatus?.enabled]);

  useEffect(() => {
    if (!settings) return;
    setForm({
      http_proxy: settings.http_proxy || '',
      https_proxy: settings.https_proxy || '',
      no_proxy: settings.no_proxy || 'localhost,127.0.0.1,::1',
      default_model: settings.default_model || '',
      default_effort: settings.default_effort || '',
      default_permission_mode: settings.default_permission_mode || 'default',
      allow_dangerous_skip: !!settings.allow_dangerous_skip,
      default_workspace: settings.default_workspace || '',
      agy_binary_path: settings.agy_binary_path || '',
      persistent_idle_minutes: settings.persistent_idle_minutes ?? 10,
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
      await fetchRcStatus();
      setSaveMsg('设置已保存并生效');
      setTimeout(() => setSaveMsg(''), 3000);
    } catch (err) {
      setSaveMsg(err instanceof Error ? err.message : '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const handleToggleRemoteControl = async (enabled: boolean) => {
    setRcBusy(true);
    setRcMsg(null);
    try {
      const updated = await api.updateSettings({ remote_control_enabled: enabled });
      onSettingsSaved(updated);
      const st = await api.remoteControlStatus();
      setRcStatus(st);
      if (enabled) setShowRcLogs(true);
      setRcMsg({
        ok: true,
        text: enabled
          ? '已开启后台常驻远程控制，关闭网页也不会断开'
          : '已停止后台远程控制守护进程',
      });
    } catch (err) {
      setRcMsg({ ok: false, text: err instanceof Error ? err.message : '操作失败' });
    } finally {
      setRcBusy(false);
    }
  };

  const handleRestartRemoteControl = async () => {
    setRcBusy(true);
    setRcMsg(null);
    try {
      const st = await api.restartRemoteControl();
      setRcStatus(st);
      setRcMsg({ ok: true, text: '已重启后台远程控制服务' });
    } catch (err) {
      setRcMsg({ ok: false, text: err instanceof Error ? err.message : '重启失败' });
    } finally {
      setRcBusy(false);
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

  const [authToggling, setAuthToggling] = useState(false);

  const handleToggleAuthDisabled = async (disabled: boolean) => {
    setPwdMsg(null);
    setAuthToggling(true);
    try {
      const updated = await api.updateSettings({ auth_disabled: disabled });
      onSettingsSaved(updated);
      setPwdMsg({
        ok: true,
        text: disabled ? '已关闭启动密码，下次打开将直接进入' : '已开启启动密码保护',
      });
    } catch (err) {
      setPwdMsg({ ok: false, text: err instanceof Error ? err.message : '切换失败' });
    } finally {
      setAuthToggling(false);
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
      const refreshed = await api.getSettings();
      onSettingsSaved(refreshed);
      setPwdMsg({
        ok: true,
        text: settings?.password_set ? '访问密码已更新' : '启动密码已设置并自动开启保护',
      });
    } catch (err) {
      setPwdMsg({ ok: false, text: err instanceof Error ? err.message : '修改失败' });
    }
  };

  return (
    <div className="h-full overflow-y-auto bg-slate-50 p-4 sm:p-6 dark:bg-slate-950">
      <div className="mx-auto max-w-3xl space-y-5 pb-12 sm:space-y-6">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-lg font-semibold text-slate-900 sm:text-xl dark:text-white">设置中心</h1>
            <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
              管理 Antigravity CLI 运行环境、网络代理、默认工作区与安全策略
            </p>
          </div>
          <div className="flex items-center justify-end gap-3">
            {saveMsg && (
              <span className="text-xs text-emerald-600 dark:text-emerald-400">{saveMsg}</span>
            )}
            <button
              onClick={handleSave}
              disabled={saving}
              className="flex w-full items-center justify-center gap-1.5 rounded-xl bg-indigo-600 px-4 py-2 text-xs font-medium text-white transition hover:bg-indigo-500 disabled:opacity-50 sm:w-auto"
            >
              {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
              <span>保存全部设置</span>
            </button>
          </div>
        </div>

        {/* 1. agy binary lifecycle */}
        <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-xs dark:border-slate-800 dark:bg-slate-900/70">
          <div className="mb-4 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Download className="h-4 w-4 text-indigo-500 dark:text-indigo-400" />
              <h2 className="text-sm font-semibold text-slate-900 dark:text-white">
                Antigravity CLI (`agy`) 状态与安装
              </h2>
            </div>
            <button
              onClick={handleRefreshStatus}
              className="flex items-center gap-1 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:bg-transparent dark:text-slate-300 dark:hover:bg-slate-800"
            >
              <RefreshCw className="h-3 w-3" />
              <span>刷新检测</span>
            </button>
          </div>

          <div className="grid grid-cols-1 gap-3 rounded-xl border border-slate-200 bg-slate-50 p-4 text-xs sm:grid-cols-2 dark:border-slate-800 dark:bg-slate-950/70">
            <div>
              <div className="text-slate-500">安装状态</div>
              <div className="mt-1 flex items-center gap-1.5 font-medium">
                {agyStatus?.installed ? (
                  <>
                    <CheckCircle2 className="h-4 w-4 text-emerald-500 dark:text-emerald-400" />
                    <span className="text-emerald-700 dark:text-emerald-300">已安装</span>
                  </>
                ) : (
                  <>
                    <AlertCircle className="h-4 w-4 text-amber-500 dark:text-amber-400" />
                    <span className="text-amber-700 dark:text-amber-300">未检测到 agy</span>
                  </>
                )}
              </div>
            </div>

            <div>
              <div className="text-slate-500">登录凭据状态</div>
              <div className="mt-1 flex items-center gap-1.5 font-medium">
                {agyStatus?.authenticated ? (
                  <span className="text-emerald-700 dark:text-emerald-300">已检测到本地凭据</span>
                ) : (
                  <button
                    onClick={onGoToTerminal}
                    className="flex items-center gap-1 text-indigo-600 hover:underline dark:text-indigo-400"
                  >
                    <Terminal className="h-3.5 w-3.5" />
                    <span>前往终端完成首次登录</span>
                  </button>
                )}
              </div>
            </div>

            <div className="sm:col-span-2">
              <div className="text-slate-500">可执行文件路径 / 版本</div>
              <div className="mt-1 font-mono text-slate-700 dark:text-slate-300">
                {agyStatus?.binary_path || '—'} {agyStatus?.version ? `(${agyStatus.version})` : ''}
              </div>
            </div>

            <div className="sm:col-span-2">
              <div className="text-slate-500">持久化隔离目录 (TRIM_PKGVAR / HOME)</div>
              <div className="mt-1 font-mono text-slate-500 dark:text-slate-400">
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
              <span>
                {installing ? '正在安装 / 升级...' : agyStatus?.installed ? '升级 agy' : '一键安装 agy'}
              </span>
            </button>

            <div className="flex flex-1 items-center gap-2">
              <input
                type="text"
                value={form.agy_binary_path}
                onChange={(e) => setForm({ ...form, agy_binary_path: e.target.value })}
                placeholder="可选：手动指定 agy 二进制路径（留空自动探测）"
                className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-800 placeholder-slate-400 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200 dark:placeholder-slate-500"
              />
            </div>
          </div>

          {(installLogs.length > 0 || installErr) && (
            <div className="mt-4 rounded-xl border border-slate-200 bg-slate-900 p-3 font-mono text-[11px] dark:border-slate-800 dark:bg-black/70">
              <div className="max-h-48 space-y-0.5 overflow-y-auto text-slate-200">
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

        {/* 1.5. Official Remote Control Daemon */}
        <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-xs dark:border-slate-800 dark:bg-slate-900/70">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Radio className="h-4 w-4 text-indigo-500 dark:text-indigo-400" />
              <h2 className="text-sm font-semibold text-slate-900 dark:text-white">
                官方远程控制（Remote Control 常驻服务）
              </h2>
            </div>
            <div className="flex items-center gap-2">
              <span
                className={`rounded-full px-2.5 py-0.5 text-[11px] font-medium ${
                  rcStatus?.running
                    ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
                    : rcStatus?.state === 'starting' || rcStatus?.state === 'restarting'
                      ? 'bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
                      : rcStatus?.state === 'error'
                        ? 'bg-rose-50 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300'
                        : 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300'
                }`}
              >
                {rcStatus?.running
                  ? `运行中${rcStatus.pid ? ` (PID ${rcStatus.pid})` : ''}`
                  : rcStatus?.state === 'starting'
                    ? '启动中...'
                    : rcStatus?.state === 'restarting'
                      ? `自动重连中（已重试 ${rcStatus.restart_count} 次）`
                      : rcStatus?.state === 'error'
                        ? '运行异常'
                        : '未开启'}
              </span>
              <button
                type="button"
                onClick={fetchRcStatus}
                className="flex items-center gap-1 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:bg-transparent dark:text-slate-300 dark:hover:bg-slate-800"
              >
                <RefreshCw className="h-3 w-3" />
                <span>刷新</span>
              </button>
            </div>
          </div>

          <p className="mb-4 text-xs text-slate-500 dark:text-slate-400">
            由后端直接托管常驻 <code className="font-mono">agy remote-control serve</code>
            ，解决在 Web 终端里执行 <code className="font-mono">/remote-control</code>{' '}
            切页面或关浏览器即断开的问题。自动继承本页的代理配置与登录凭据，异常退出自动拉起，开机随应用自启。
          </p>

          <div className="flex flex-col gap-3 rounded-xl border border-slate-200 bg-slate-50/80 p-3.5 sm:flex-row sm:items-center sm:justify-between dark:border-slate-800 dark:bg-slate-950/60">
            <div className="text-xs">
              <div className="font-medium text-slate-800 dark:text-slate-200">
                后台常驻远程控制守护进程
              </div>
              <div className="mt-0.5 text-slate-500 dark:text-slate-400">
                开启后，在任意电脑或手机浏览器打开{' '}
                <a
                  href="https://antigravity.google.com"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-0.5 font-medium text-indigo-600 hover:underline dark:text-indigo-400"
                >
                  <span>antigravity.google.com</span>
                  <ExternalLink className="h-3 w-3" />
                </a>{' '}
                使用同一 Google 账号登录即可连接本台飞牛 NAS。
              </div>
            </div>
            <div className="flex shrink-0 flex-wrap items-center gap-2">
              {settings?.remote_control_enabled ? (
                <>
                  <button
                    type="button"
                    disabled={rcBusy}
                    onClick={handleRestartRemoteControl}
                    className="flex items-center gap-1 rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs font-medium text-slate-700 transition hover:bg-slate-100 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                  >
                    <RotateCw className={`h-3.5 w-3.5 ${rcBusy ? 'animate-spin' : ''}`} />
                    <span>重启服务</span>
                  </button>
                  <button
                    type="button"
                    disabled={rcBusy}
                    onClick={() => handleToggleRemoteControl(false)}
                    className="rounded-xl border border-rose-300 bg-white px-3.5 py-2 text-xs font-medium text-rose-700 transition hover:bg-rose-50 disabled:opacity-40 dark:border-rose-500/40 dark:bg-slate-900 dark:text-rose-300 dark:hover:bg-rose-500/10"
                  >
                    {rcBusy ? '处理中...' : '关闭远程控制'}
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  disabled={rcBusy || !agyStatus?.installed}
                  onClick={() => handleToggleRemoteControl(true)}
                  title={!agyStatus?.installed ? '请先在上方安装 agy CLI' : '开启后台常驻远程控制'}
                  className="flex items-center gap-1.5 rounded-xl bg-indigo-600 px-3.5 py-2 text-xs font-medium text-white transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {rcBusy ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Radio className="h-3.5 w-3.5" />
                  )}
                  <span>{rcBusy ? '启动中...' : '开启远程控制'}</span>
                </button>
              )}
            </div>
          </div>

          {rcStatus?.last_error && (
            <div className="mt-3 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
              最近异常：{rcStatus.last_error}
            </div>
          )}

          {rcMsg && (
            <div
              className={`mt-2 text-xs ${
                rcMsg.ok
                  ? 'text-emerald-600 dark:text-emerald-400'
                  : 'text-rose-600 dark:text-rose-400'
              }`}
            >
              {rcMsg.text}
            </div>
          )}

          {rcStatus && rcStatus.logs && rcStatus.logs.length > 0 && (
            <div className="mt-3">
              <button
                type="button"
                onClick={() => setShowRcLogs((v) => !v)}
                className="text-xs font-medium text-slate-600 hover:text-indigo-600 dark:text-slate-400 dark:hover:text-indigo-400"
              >
                {showRcLogs ? '隐藏运行日志 ▲' : `查看运行日志（最近 ${rcStatus.logs.length} 行）▼`}
              </button>
              {showRcLogs && (
                <div className="mt-2 rounded-xl border border-slate-200 bg-slate-900 p-3 font-mono text-[11px] dark:border-slate-800 dark:bg-black/70">
                  <div className="max-h-48 space-y-0.5 overflow-y-auto text-slate-200">
                    {rcStatus.logs.map((l, i) => (
                      <div key={i} className="whitespace-pre-wrap break-all">
                        {l}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </section>

        {/* 2. Network Proxy */}
        <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-xs dark:border-slate-800 dark:bg-slate-900/70">
          <div className="mb-4 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Globe className="h-4 w-4 text-indigo-500 dark:text-indigo-400" />
              <h2 className="text-sm font-semibold text-slate-900 dark:text-white">网络代理配置</h2>
            </div>
            <button
              onClick={handleTestProxy}
              disabled={testingProxy}
              className="flex items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-3 py-1 text-xs text-slate-700 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
            >
              {testingProxy && <Loader2 className="h-3 w-3 animate-spin" />}
              <span>测试连通性</span>
            </button>
          </div>

          <p className="mb-4 text-xs text-slate-500 dark:text-slate-400">
            仅注入到本应用拉起的 `agy` 安装脚本、Web Chat 子进程与内置终端，不影响飞牛宿主机全局网络。
          </p>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs text-slate-700 dark:text-slate-300">HTTP_PROXY</label>
              <input
                type="text"
                value={form.http_proxy}
                onChange={(e) => setForm({ ...form, http_proxy: e.target.value })}
                placeholder="例如 http://192.168.1.10:7890"
                className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-900 placeholder-slate-400 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:placeholder-slate-500"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-700 dark:text-slate-300">
                HTTPS_PROXY（留空默认同 HTTP）
              </label>
              <input
                type="text"
                value={form.https_proxy}
                onChange={(e) => setForm({ ...form, https_proxy: e.target.value })}
                placeholder="例如 http://192.168.1.10:7890"
                className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-900 placeholder-slate-400 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:placeholder-slate-500"
              />
            </div>
            <div className="sm:col-span-2">
              <label className="mb-1 block text-xs text-slate-700 dark:text-slate-300">NO_PROXY</label>
              <input
                type="text"
                value={form.no_proxy}
                onChange={(e) => setForm({ ...form, no_proxy: e.target.value })}
                placeholder="localhost,127.0.0.1,::1,192.168.0.0/16"
                className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-900 placeholder-slate-400 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:placeholder-slate-500"
              />
            </div>
          </div>

          {proxyResult && (
            <div
              className={`mt-3 rounded-xl border px-3 py-2 text-xs ${
                proxyResult.ok
                  ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'
                  : 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-300'
              }`}
            >
              {proxyResult.ok
                ? `连接 ${proxyResult.target_url} 成功（HTTP ${proxyResult.status_code}，延迟 ${proxyResult.latency_ms}ms）`
                : `连接失败（${proxyResult.latency_ms}ms）：${proxyResult.error}`}
            </div>
          )}
        </section>

        {/* 3. Defaults & Safety */}
        <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-xs dark:border-slate-800 dark:bg-slate-900/70">
          <div className="mb-4 flex items-center gap-2">
            <ShieldAlert className="h-4 w-4 text-indigo-500 dark:text-indigo-400" />
            <h2 className="text-sm font-semibold text-slate-900 dark:text-white">默认对话与权限策略</h2>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="sm:col-span-2">
              <label className="mb-1 block text-xs text-slate-700 dark:text-slate-300">默认工作区目录</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={form.default_workspace}
                  onChange={(e) => setForm({ ...form, default_workspace: e.target.value })}
                  className="flex-1 rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-900 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white"
                />
                <button
                  onClick={() => setPickerOpen(true)}
                  className="flex items-center gap-1.5 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
                >
                  <FolderOpen className="h-3.5 w-3.5 text-amber-500 dark:text-amber-400" />
                  <span>浏览</span>
                </button>
              </div>
            </div>

            <div>
              <label className="mb-1 block text-xs text-slate-700 dark:text-slate-300">
                默认模型（留空使用 CLI 默认）
              </label>
              {models.length > 0 ? (
                <select
                  value={form.default_model}
                  onChange={(e) => setForm({ ...form, default_model: e.target.value })}
                  className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs text-slate-900 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white"
                >
                  <option value="">默认模型 (CLI 自动)</option>
                  {models.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.label}
                    </option>
                  ))}
                  {form.default_model && !models.some((m) => m.id === form.default_model) && (
                    <option value={form.default_model}>{form.default_model}</option>
                  )}
                </select>
              ) : (
                <input
                  type="text"
                  value={form.default_model}
                  onChange={(e) => setForm({ ...form, default_model: e.target.value })}
                  placeholder="例如 gemini-3.8-flash-medium"
                  className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs text-slate-900 placeholder-slate-400 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:placeholder-slate-500"
                />
              )}
            </div>

            <div>
              <label className="mb-1 block text-xs text-slate-700 dark:text-slate-300">
                默认思考强度 (--effort)
              </label>
              <select
                value={form.default_effort}
                onChange={(e) => setForm({ ...form, default_effort: e.target.value })}
                className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs text-slate-900 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white"
              >
                <option value="">默认 (CLI 自动)</option>
                <option value="low">低 (low)</option>
                <option value="medium">中 (medium)</option>
                <option value="high">高 (high)</option>
                <option value="xhigh">极高 (xhigh)</option>
                <option value="max">最大 (max)</option>
              </select>
            </div>

            <div>
              <label className="mb-1 block text-xs text-slate-700 dark:text-slate-300">默认执行模式</label>
              <select
                value={form.default_permission_mode}
                onChange={(e) =>
                  setForm({ ...form, default_permission_mode: e.target.value as PermissionMode })
                }
                className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs text-slate-900 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white"
              >
                <option value="default">标准模式 (default)</option>
                <option value="accept-edits">自动接受编辑 (accept-edits)</option>
                <option value="plan">仅规划 (plan)</option>
                {form.allow_dangerous_skip && <option value="bypass">跳过权限确认 (危险)</option>}
              </select>
            </div>

            <div>
              <label className="mb-1 block text-xs text-slate-700 dark:text-slate-300">
                对话长连接空闲自动关闭时间
              </label>
              <select
                value={form.persistent_idle_minutes}
                onChange={(e) =>
                  setForm({ ...form, persistent_idle_minutes: Number(e.target.value) })
                }
                className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs text-slate-900 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white"
              >
                <option value={0}>关闭长连接（每轮按需启动）</option>
                <option value={5}>空闲 5 分钟后关闭</option>
                <option value={10}>空闲 10 分钟后关闭（推荐）</option>
                <option value={20}>空闲 20 分钟后关闭</option>
                <option value={30}>空闲 30 分钟后关闭</option>
                <option value={60}>空闲 60 分钟后关闭</option>
              </select>
              <p className="mt-1 text-[11px] text-slate-400">
                保持进程常驻可显著降低多轮对话延迟；超时后自动关闭释放 NAS 内存，下次发消息自动重连。
              </p>
            </div>
          </div>

          <label className="mt-4 flex cursor-pointer items-start gap-2.5 rounded-xl border border-amber-200 bg-amber-50/70 p-3 text-xs dark:border-amber-500/20 dark:bg-amber-500/5">
            <input
              type="checkbox"
              checked={form.allow_dangerous_skip}
              onChange={(e) => setForm({ ...form, allow_dangerous_skip: e.target.checked })}
              className="mt-0.5 rounded border-slate-300 text-amber-600 dark:border-slate-600 dark:bg-slate-900"
            />
            <div>
              <div className="font-medium text-amber-800 dark:text-amber-300">
                允许在 Web Chat 中启用跳过权限确认（`--dangerously-skip-permissions`）
              </div>
              <div className="mt-0.5 text-slate-600 dark:text-slate-400">
                高风险选项：开启后 Agent 在授权目录内执行命令和修改文件将不再逐项确认，请确保已在飞牛应用设置中严格限定授权目录。
              </div>
            </div>
          </label>
        </section>

        {/* 4. Startup Password & Access Security */}
        <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-xs dark:border-slate-800 dark:bg-slate-900/70">
          <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <KeyRound className="h-4 w-4 text-indigo-500 dark:text-indigo-400" />
              <h2 className="text-sm font-semibold text-slate-900 dark:text-white">启动密码与访问安全</h2>
            </div>
            <span
              className={`rounded-full px-2.5 py-0.5 text-[11px] font-medium ${
                settings?.auth_disabled
                  ? 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300'
                  : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
              }`}
            >
              {settings?.auth_disabled ? '无密码直接进入' : '已开启启动密码'}
            </span>
          </div>

          <div className="mb-4 flex flex-col gap-3 rounded-xl border border-slate-200 bg-slate-50/80 p-3.5 sm:flex-row sm:items-center sm:justify-between dark:border-slate-800 dark:bg-slate-950/60">
            <div className="text-xs">
              <div className="font-medium text-slate-800 dark:text-slate-200">
                应用启动密码保护
              </div>
              <div className="mt-0.5 text-slate-500 dark:text-slate-400">
                关闭后打开应用将直接进入工作台（依赖飞牛 NAS 系统本身登录鉴权）；如需二次密码保护可随时重新开启。
              </div>
            </div>
            <div className="shrink-0">
              {settings?.auth_disabled ? (
                <button
                  type="button"
                  disabled={authToggling || !settings?.password_set}
                  onClick={() => handleToggleAuthDisabled(false)}
                  title={!settings?.password_set ? '请先在下方设置启动密码' : '开启启动密码保护'}
                  className="rounded-xl bg-indigo-600 px-3.5 py-2 text-xs font-medium text-white transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {authToggling ? '处理中...' : '开启启动密码'}
                </button>
              ) : (
                <button
                  type="button"
                  disabled={authToggling}
                  onClick={() => handleToggleAuthDisabled(true)}
                  className="rounded-xl border border-slate-300 bg-white px-3.5 py-2 text-xs font-medium text-slate-700 transition hover:border-amber-400 hover:bg-amber-50 hover:text-amber-800 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                >
                  {authToggling ? '处理中...' : '关闭密码（无密码直进）'}
                </button>
              )}
            </div>
          </div>

          <form
            onSubmit={handleChangePassword}
            className={`grid grid-cols-1 gap-3 ${
              settings?.password_set ? 'sm:grid-cols-3' : 'sm:grid-cols-2'
            }`}
          >
            {settings?.password_set && (
              <input
                type="password"
                value={curPwd}
                onChange={(e) => setCurPwd(e.target.value)}
                placeholder="当前密码"
                className="rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs text-slate-900 placeholder-slate-400 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:placeholder-slate-500"
              />
            )}
            <input
              type="password"
              value={newPwd}
              onChange={(e) => setNewPwd(e.target.value)}
              placeholder={settings?.password_set ? '新密码（至少 6 位）' : '设置启动密码（至少 6 位）'}
              className="rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs text-slate-900 placeholder-slate-400 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:placeholder-slate-500"
            />
            <button
              type="submit"
              disabled={(settings?.password_set ? !curPwd : false) || !newPwd}
              className="rounded-xl border border-slate-200 bg-slate-100 px-4 py-2 text-xs font-medium text-slate-800 transition hover:bg-indigo-600 hover:text-white disabled:opacity-40 dark:border-slate-700 dark:bg-slate-800 dark:text-white"
            >
              {settings?.password_set ? '更新密码' : '设置密码并开启保护'}
            </button>
          </form>

          {pwdMsg && (
            <div
              className={`mt-2 text-xs ${
                pwdMsg.ok
                  ? 'text-emerald-600 dark:text-emerald-400'
                  : 'text-rose-600 dark:text-rose-400'
              }`}
            >
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
