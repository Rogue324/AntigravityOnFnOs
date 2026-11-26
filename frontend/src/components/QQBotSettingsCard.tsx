import React, { useCallback, useEffect, useState } from 'react';
import QRCode from 'qrcode';
import {
  MessageSquare,
  QrCode,
  KeyRound,
  RefreshCw,
  RotateCw,
  Send,
  Trash2,
  Loader2,
  Save,
  FolderOpen,
  ShieldCheck,
  CheckCircle2,
  AlertCircle,
  X,
} from 'lucide-react';
import { api } from '../api/client';
import type { ModelOption, QQBotQRStatus, QQBotStatus } from '../types';
import { DirPickerModal } from './DirPickerModal';

interface QQBotSettingsCardProps {
  models: ModelOption[];
  defaultWorkspace?: string;
}

function formatRemaining(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000));
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

function splitList(raw: string): string[] {
  return raw
    .split(/[\n,，]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}

export const QQBotSettingsCard: React.FC<QQBotSettingsCardProps> = ({
  models,
  defaultWorkspace,
}) => {
  const [status, setStatus] = useState<QQBotStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [showLogs, setShowLogs] = useState(false);

  // QR provisioning state
  const [qr, setQr] = useState<QQBotQRStatus | null>(null);
  const [qrDataUrl, setQrDataUrl] = useState<string>('');
  const [now, setNow] = useState<number>(() => Date.now());

  // Manual bind form
  const [showManual, setShowManual] = useState(false);
  const [manualAppId, setManualAppId] = useState('');
  const [manualAppSecret, setManualAppSecret] = useState('');
  const [manualOwnerOpenId, setManualOwnerOpenId] = useState('');
  const [confirmUnbind, setConfirmUnbind] = useState(false);

  // Bot runtime & access control form
  const [botForm, setBotForm] = useState({
    owner_user_openid: '',
    workspace: '',
    model: '',
    permission_mode: '',
    allow_c2c: true,
    allow_group: true,
    restrict_to_owner: false,
    allowed_users_text: '',
    allowed_groups_text: '',
  });
  const [pickerOpen, setPickerOpen] = useState(false);
  const [savingCfg, setSavingCfg] = useState(false);

  const syncFormFromStatus = useCallback((st: QQBotStatus) => {
    setBotForm({
      owner_user_openid: st.config.owner_user_openid || '',
      workspace: st.config.workspace || '',
      model: st.config.model || '',
      permission_mode: st.config.permission_mode || '',
      allow_c2c: st.config.allow_c2c,
      allow_group: st.config.allow_group,
      restrict_to_owner: st.config.restrict_to_owner,
      allowed_users_text: (st.config.allowed_users || []).join('\n'),
      allowed_groups_text: (st.config.allowed_groups || []).join('\n'),
    });
  }, []);

  const fetchStatus = useCallback(
    async (syncForm = false) => {
      try {
        const st = await api.qqBotStatus();
        setStatus(st);
        if (syncForm) {
          syncFormFromStatus(st);
        }
        if (st.qr && ['starting', 'pending', 'refreshing', 'connecting'].includes(st.qr.status)) {
          setQr(st.qr);
        }
      } catch {
        // ignore transient errors
      }
    },
    [syncFormFromStatus]
  );

  useEffect(() => {
    fetchStatus(true);
  }, [fetchStatus]);

  // Poll status while connecting / reconnecting / active
  useEffect(() => {
    const active =
      status?.config.enabled ||
      status?.connection_state === 'connecting' ||
      status?.connection_state === 'reconnecting';
    if (!active) return;
    const timer = window.setInterval(() => {
      fetchStatus(false);
    }, 5000);
    return () => window.clearInterval(timer);
  }, [status?.config.enabled, status?.connection_state, fetchStatus]);

  // Render QR code data URL whenever verification_url changes
  useEffect(() => {
    const url = qr?.verification_url;
    if (!url) {
      setQrDataUrl('');
      return;
    }
    let cancelled = false;
    QRCode.toDataURL(url, { width: 220, margin: 2 })
      .then((dataUrl) => {
        if (!cancelled) setQrDataUrl(dataUrl);
      })
      .catch(() => {
        if (!cancelled) setQrDataUrl('');
      });
    return () => {
      cancelled = true;
    };
  }, [qr?.verification_url]);

  // Poll QR status while QR flow is active
  const qrActive =
    qr != null && ['starting', 'pending', 'refreshing', 'connecting'].includes(qr.status);

  useEffect(() => {
    if (!qrActive) return;
    const tickTimer = window.setInterval(() => setNow(Date.now()), 1000);
    const pollTimer = window.setInterval(async () => {
      try {
        const next = await api.getQQBotQRStatus();
        setQr(next);
        if (next.status === 'connected') {
          setMsg({ ok: true, text: 'QQ 扫码绑定成功，机器人正在建立 WebSocket 连接' });
          setShowLogs(true);
          await fetchStatus(true);
        } else if (next.status === 'failed') {
          setMsg({ ok: false, text: next.error || 'QQ 扫码绑定失败，请重试' });
        }
      } catch {
        // ignore transient poll errors
      }
    }, qr?.poll_interval_ms || 1500);

    return () => {
      window.clearInterval(tickTimer);
      window.clearInterval(pollTimer);
    };
  }, [qrActive, qr?.poll_interval_ms, fetchStatus]);

  const handleStartQR = async () => {
    setBusy(true);
    setMsg(null);
    setShowManual(false);
    try {
      const attempt = await api.startQQBotQR();
      setQr(attempt);
      setNow(Date.now());
    } catch (err) {
      setMsg({ ok: false, text: err instanceof Error ? err.message : '生成二维码失败' });
    } finally {
      setBusy(false);
    }
  };

  const handleCancelQR = async () => {
    setBusy(true);
    try {
      await api.cancelQQBotQR();
      setQr(null);
      setQrDataUrl('');
    } finally {
      setBusy(false);
    }
  };

  const handleManualBind = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!manualAppId.trim() || !manualAppSecret.trim()) {
      setMsg({ ok: false, text: '请填写完整的 AppID 和 AppSecret' });
      return;
    }
    setBusy(true);
    setMsg(null);
    try {
      const st = await api.bindQQBot({
        app_id: manualAppId.trim(),
        app_secret: manualAppSecret.trim(),
        owner_user_openid: manualOwnerOpenId.trim() || undefined,
      });
      setStatus(st);
      syncFormFromStatus(st);
      setManualAppSecret('');
      setShowManual(false);
      setQr(null);
      setShowLogs(true);
      setMsg({ ok: true, text: 'QQ 机器人凭据校验通过并已启动连接' });
    } catch (err) {
      setMsg({ ok: false, text: err instanceof Error ? err.message : '绑定失败' });
    } finally {
      setBusy(false);
    }
  };

  const handleToggleEnabled = async (enabled: boolean) => {
    setBusy(true);
    setMsg(null);
    try {
      const st = await api.updateQQBotConfig({ enabled });
      setStatus(st);
      setMsg({
        ok: true,
        text: enabled ? '已启动 QQ 机器人网关连接' : '已暂停 QQ 机器人连接',
      });
    } catch (err) {
      setMsg({ ok: false, text: err instanceof Error ? err.message : '操作失败' });
    } finally {
      setBusy(false);
    }
  };

  const handleRestart = async () => {
    setBusy(true);
    setMsg(null);
    try {
      const st = await api.restartQQBot();
      setStatus(st);
      setShowLogs(true);
      setMsg({ ok: true, text: '已触发 QQ 机器人重新连接' });
    } catch (err) {
      setMsg({ ok: false, text: err instanceof Error ? err.message : '重连失败' });
    } finally {
      setBusy(false);
    }
  };

  const handleTestMessage = async () => {
    setBusy(true);
    setMsg(null);
    try {
      await api.testQQBot();
      setMsg({ ok: true, text: '测试消息已发送，请在手机 QQ 中查收' });
    } catch (err) {
      setMsg({ ok: false, text: err instanceof Error ? err.message : '发送测试消息失败' });
    } finally {
      setBusy(false);
    }
  };

  const handleUnbind = async () => {
    setBusy(true);
    setMsg(null);
    try {
      const st = await api.unbindQQBot();
      setStatus(st);
      syncFormFromStatus(st);
      setConfirmUnbind(false);
      setQr(null);
      setMsg({ ok: true, text: '已移除本地 QQ 机器人绑定与凭据' });
    } catch (err) {
      setMsg({ ok: false, text: err instanceof Error ? err.message : '移除失败' });
    } finally {
      setBusy(false);
    }
  };

  const handleSaveBotConfig = async () => {
    setSavingCfg(true);
    setMsg(null);
    try {
      const st = await api.updateQQBotConfig({
        owner_user_openid: botForm.owner_user_openid.trim(),
        workspace: botForm.workspace.trim(),
        model: botForm.model,
        permission_mode: botForm.permission_mode,
        allow_c2c: botForm.allow_c2c,
        allow_group: botForm.allow_group,
        restrict_to_owner: botForm.restrict_to_owner,
        allowed_users: splitList(botForm.allowed_users_text),
        allowed_groups: splitList(botForm.allowed_groups_text),
      });
      setStatus(st);
      syncFormFromStatus(st);
      setMsg({ ok: true, text: 'QQ 机器人运行与权限配置已保存' });
    } catch (err) {
      setMsg({ ok: false, text: err instanceof Error ? err.message : '保存失败' });
    } finally {
      setSavingCfg(false);
    }
  };

  const configured = !!status?.config.configured;
  const connState = status?.connection_state || 'idle';

  const badgeClass =
    connState === 'connected'
      ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
      : connState === 'connecting' || connState === 'reconnecting'
        ? 'bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
        : connState === 'error'
          ? 'bg-rose-50 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300'
          : 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300';

  const badgeLabel =
    connState === 'connected'
      ? `已连接${status?.bot_username ? ` (${status.bot_username})` : ''}`
      : connState === 'connecting'
        ? '连接中...'
        : connState === 'reconnecting'
          ? '自动重连中...'
          : connState === 'error'
            ? '连接异常'
            : configured
              ? '已停用'
              : '未绑定';

  return (
    <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-xs dark:border-slate-800 dark:bg-slate-900/70">
      {/* Header */}
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <MessageSquare className="h-4 w-4 text-sky-500 dark:text-sky-400" />
          <h2 className="text-sm font-semibold text-slate-900 dark:text-white">
            QQ 机器人接入（私聊 / 群聊 @）
          </h2>
        </div>
        <div className="flex items-center gap-2">
          <span className={`rounded-full px-2.5 py-0.5 text-[11px] font-medium ${badgeClass}`}>
            {badgeLabel}
          </span>
          <button
            type="button"
            onClick={() => fetchStatus(false)}
            className="flex items-center gap-1 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:bg-transparent dark:text-slate-300 dark:hover:bg-slate-800"
          >
            <RefreshCw className="h-3 w-3" />
            <span>刷新</span>
          </button>
        </div>
      </div>

      <p className="mb-4 text-xs text-slate-500 dark:text-slate-400">
        使用纯 Go 原生接入腾讯 QQ 开放平台 WebSocket 网关，无需公网域名与 Node.js。支持手机 QQ
        扫码一键创建绑定或手动填写 AppID / AppSecret，会话与 Web Chat 实时共享，支持斜杠命令、图片/文件附件与 QQ
        端权限审批。
      </p>

      {/* Action Bar for QR / Manual Bind when unbound */}
      {!configured && !qrActive && (
        <div className="flex flex-col gap-3 rounded-xl border border-slate-200 bg-slate-50/80 p-4 sm:flex-row sm:items-center sm:justify-between dark:border-slate-800 dark:bg-slate-950/60">
          <div className="text-xs">
            <div className="font-medium text-slate-800 dark:text-slate-200">
              尚未绑定 QQ 机器人
            </div>
            <div className="mt-0.5 text-slate-500 dark:text-slate-400">
              推荐直接点击「扫码一键接入」，使用手机 QQ 扫描腾讯官方二维码即可自动下发凭据并上线。
            </div>
          </div>
          <div className="flex shrink-0 flex-wrap items-center gap-2">
            <button
              type="button"
              disabled={busy}
              onClick={handleStartQR}
              className="flex items-center gap-1.5 rounded-xl bg-sky-600 px-3.5 py-2 text-xs font-medium text-white transition hover:bg-sky-500 disabled:opacity-50"
            >
              {busy ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <QrCode className="h-3.5 w-3.5" />
              )}
              <span>扫码一键接入</span>
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={() => setShowManual((v) => !v)}
              className="flex items-center gap-1.5 rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs font-medium text-slate-700 transition hover:bg-slate-100 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
            >
              <KeyRound className="h-3.5 w-3.5" />
              <span>{showManual ? '收起手动接入' : '手动 AppID 接入'}</span>
            </button>
          </div>
        </div>
      )}

      {/* Active QR Code Panel */}
      {qrActive && qr && (
        <div className="mb-4 rounded-xl border border-sky-200 bg-sky-50/60 p-4 dark:border-sky-500/30 dark:bg-sky-950/20">
          <div className="flex flex-col items-center gap-4 sm:flex-row sm:items-start">
            <div className="flex flex-col items-center">
              <div className="flex h-[200px] w-[200px] items-center justify-center rounded-xl border border-slate-200 bg-white p-2 shadow-xs dark:border-slate-700">
                {qr.status === 'connecting' ? (
                  <div className="flex flex-col items-center gap-2 text-xs text-slate-600">
                    <Loader2 className="h-7 w-7 animate-spin text-sky-600" />
                    <span>授权成功，连接中...</span>
                  </div>
                ) : qrDataUrl ? (
                  <img
                    src={qrDataUrl}
                    alt="QQ 机器人绑定二维码"
                    className="h-full w-full rounded-lg object-contain"
                  />
                ) : (
                  <div className="flex flex-col items-center gap-2 text-xs text-slate-500">
                    <Loader2 className="h-6 w-6 animate-spin text-sky-500" />
                    <span>{qr.status === 'refreshing' ? '正在刷新二维码...' : '正在生成二维码...'}</span>
                  </div>
                )}
              </div>
              {qr.expires_at && qr.status === 'pending' && (
                <div className="mt-2 text-[11px] text-slate-600 dark:text-slate-400">
                  有效倒计时：
                  <span className="font-mono font-medium">
                    {formatRemaining(qr.expires_at - now)}
                  </span>
                </div>
              )}
            </div>

            <div className="flex-1 text-xs">
              <div className="mb-1.5 font-semibold text-slate-900 dark:text-white">
                请使用手机 QQ 扫描左侧二维码
              </div>
              <ol className="list-inside list-decimal space-y-1 text-slate-600 dark:text-slate-300">
                <li>打开手机 QQ，点击右上角「+」→「扫一扫」</li>
                <li>在腾讯官方授权页面确认创建或绑定 QQ 机器人</li>
                <li>授权完成后，本页将自动解密并保存凭据、启动 WebSocket 网关</li>
              </ol>
              <div className="mt-4 flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  disabled={busy}
                  onClick={handleStartQR}
                  className="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                >
                  重新生成
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={handleCancelQR}
                  className="flex items-center gap-1 rounded-lg px-3 py-1.5 text-xs text-slate-500 hover:bg-slate-200/60 dark:text-slate-400 dark:hover:bg-slate-800"
                >
                  <X className="h-3.5 w-3.5" />
                  <span>取消扫码</span>
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Manual Credential Binding Form */}
      {showManual && (
        <form
          onSubmit={handleManualBind}
          className="mt-3 rounded-xl border border-slate-200 bg-slate-50/70 p-4 dark:border-slate-800 dark:bg-slate-950/60"
        >
          <div className="mb-3 text-xs font-medium text-slate-800 dark:text-slate-200">
            手动填写 QQ 开放平台凭据（q.qq.com）
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs text-slate-600 dark:text-slate-400">
                AppID <span className="text-rose-500">*</span>
              </label>
              <input
                type="text"
                value={manualAppId}
                onChange={(e) => setManualAppId(e.target.value)}
                placeholder="例如：102030405"
                className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-800 focus:border-sky-500 focus:outline-none dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-600 dark:text-slate-400">
                AppSecret <span className="text-rose-500">*</span>
              </label>
              <input
                type="password"
                value={manualAppSecret}
                onChange={(e) => setManualAppSecret(e.target.value)}
                placeholder="机器人的 AppSecret"
                className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-800 focus:border-sky-500 focus:outline-none dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
              />
            </div>
            <div className="sm:col-span-2">
              <label className="mb-1 block text-xs text-slate-600 dark:text-slate-400">
                绑定主人 User OpenID（可选，也可绑定后发送 /status 获取）
              </label>
              <input
                type="text"
                value={manualOwnerOpenId}
                onChange={(e) => setManualOwnerOpenId(e.target.value)}
                placeholder="留空则默认允许私聊"
                className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-800 focus:border-sky-500 focus:outline-none dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
              />
            </div>
          </div>
          <div className="mt-3 flex items-center gap-2">
            <button
              type="submit"
              disabled={busy}
              className="flex items-center gap-1.5 rounded-xl bg-sky-600 px-3.5 py-2 text-xs font-medium text-white hover:bg-sky-500 disabled:opacity-50"
            >
              {busy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>校验并绑定</span>
            </button>
            <button
              type="button"
              onClick={() => setShowManual(false)}
              className="rounded-xl px-3 py-2 text-xs text-slate-500 hover:bg-slate-200/70 dark:text-slate-400 dark:hover:bg-slate-800"
            >
              取消
            </button>
          </div>
        </form>
      )}

      {/* Bound Bot Control & Settings */}
      {configured && status && (
        <div className="space-y-4">
          <div className="flex flex-col gap-3 rounded-xl border border-slate-200 bg-slate-50/80 p-3.5 sm:flex-row sm:items-center sm:justify-between dark:border-slate-800 dark:bg-slate-950/60">
            <div className="space-y-0.5 text-xs">
              <div className="flex items-center gap-2 font-medium text-slate-800 dark:text-slate-200">
                <span>AppID：</span>
                <code className="font-mono text-slate-700 dark:text-slate-300">
                  {status.config.app_id_masked || status.config.app_id}
                </code>
                {status.bot_username && (
                  <span className="text-slate-500 dark:text-slate-400">
                    ({status.bot_username})
                  </span>
                )}
              </div>
              {status.config.owner_user_openid && (
                <div className="text-[11px] text-slate-500 dark:text-slate-400">
                  主人 OpenID：
                  <code className="font-mono">{status.config.owner_user_openid}</code>
                </div>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {status.config.enabled ? (
                <>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={handleRestart}
                    className="flex items-center gap-1 rounded-xl border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                  >
                    <RotateCw className={`h-3.5 w-3.5 ${busy ? 'animate-spin' : ''}`} />
                    <span>重连</span>
                  </button>
                  <button
                    type="button"
                    disabled={busy || !status.connected}
                    onClick={handleTestMessage}
                    title={
                      !status.connected
                        ? '等待机器人连接就绪'
                        : !status.can_send_test
                          ? '请先在 QQ 私聊机器人发一条消息，或在下方填写主人 OpenID'
                          : '向主人/最近私聊用户发送测试消息'
                    }
                    className="flex items-center gap-1 rounded-xl border border-sky-300 bg-white px-3 py-1.5 text-xs font-medium text-sky-700 hover:bg-sky-50 disabled:opacity-40 dark:border-sky-500/40 dark:bg-slate-900 dark:text-sky-300 dark:hover:bg-sky-500/10"
                  >
                    <Send className="h-3.5 w-3.5" />
                    <span>发送测试</span>
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => handleToggleEnabled(false)}
                    className="rounded-xl border border-amber-300 bg-white px-3 py-1.5 text-xs font-medium text-amber-700 hover:bg-amber-50 disabled:opacity-40 dark:border-amber-500/40 dark:bg-slate-900 dark:text-amber-300 dark:hover:bg-amber-500/10"
                  >
                    暂停连接
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => handleToggleEnabled(true)}
                  className="rounded-xl bg-sky-600 px-3.5 py-1.5 text-xs font-medium text-white hover:bg-sky-500 disabled:opacity-40"
                >
                  启用连接
                </button>
              )}

              {!confirmUnbind ? (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => setConfirmUnbind(true)}
                  className="flex items-center gap-1 rounded-xl border border-rose-300 bg-white px-2.5 py-1.5 text-xs font-medium text-rose-700 hover:bg-rose-50 disabled:opacity-40 dark:border-rose-500/40 dark:bg-slate-900 dark:text-rose-300 dark:hover:bg-rose-500/10"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  <span>解绑</span>
                </button>
              ) : (
                <div className="flex items-center gap-1.5">
                  <button
                    type="button"
                    disabled={busy}
                    onClick={handleUnbind}
                    className="rounded-xl bg-rose-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-rose-500"
                  >
                    确认解绑
                  </button>
                  <button
                    type="button"
                    onClick={() => setConfirmUnbind(false)}
                    className="rounded-xl px-2 py-1.5 text-xs text-slate-500 hover:bg-slate-200/70 dark:text-slate-400"
                  >
                    取消
                  </button>
                </div>
              )}
            </div>
          </div>

          {/* Bot Runtime & Access Control Settings */}
          <div className="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
            <div className="mb-3 flex items-center gap-1.5 text-xs font-semibold text-slate-800 dark:text-slate-200">
              <ShieldCheck className="h-3.5 w-3.5 text-indigo-500" />
              <span>机器人会话与访问控制</span>
            </div>

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="sm:col-span-2">
                <label className="mb-1 block text-xs text-slate-600 dark:text-slate-400">
                  QQ 专属默认工作区（留空则跟随全局默认：{defaultWorkspace || '未设置'}）
                </label>
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={botForm.workspace}
                    onChange={(e) => setBotForm({ ...botForm, workspace: e.target.value })}
                    placeholder="留空跟随全局默认工作区"
                    className="flex-1 rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-800 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
                  />
                  <button
                    type="button"
                    onClick={() => setPickerOpen(true)}
                    className="flex items-center gap-1 rounded-xl border border-slate-300 bg-slate-50 px-3 py-2 text-xs text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                  >
                    <FolderOpen className="h-3.5 w-3.5" />
                    <span>浏览</span>
                  </button>
                </div>
              </div>

              <div>
                <label className="mb-1 block text-xs text-slate-600 dark:text-slate-400">
                  QQ 专属模型（可选）
                </label>
                <select
                  value={botForm.model}
                  onChange={(e) => setBotForm({ ...botForm, model: e.target.value })}
                  className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs text-slate-800 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
                >
                  <option value="">跟随全局默认模型</option>
                  {models.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.label || m.id}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label className="mb-1 block text-xs text-slate-600 dark:text-slate-400">
                  QQ 专属权限模式
                </label>
                <select
                  value={botForm.permission_mode}
                  onChange={(e) => setBotForm({ ...botForm, permission_mode: e.target.value })}
                  className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-xs text-slate-800 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
                >
                  <option value="">跟随全局默认</option>
                  <option value="default">default（触发审批时在 QQ 回复序号确认）</option>
                  <option value="accept-edits">accept-edits（自动允许文件编辑）</option>
                  <option value="plan">plan（只读规划模式）</option>
                  <option value="bypass">bypass（跳过全部确认，需开启全局允许）</option>
                </select>
              </div>

              <div className="sm:col-span-2">
                <label className="mb-1 block text-xs text-slate-600 dark:text-slate-400">
                  绑定主人 User OpenID（发送 /status 可查看自己的 OpenID）
                </label>
                <input
                  type="text"
                  value={botForm.owner_user_openid}
                  onChange={(e) => setBotForm({ ...botForm, owner_user_openid: e.target.value })}
                  placeholder="扫码绑定时会自动填入"
                  className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-800 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
                />
              </div>

              <div className="flex flex-wrap items-center gap-4 sm:col-span-2">
                <label className="flex cursor-pointer items-center gap-2 text-xs text-slate-700 dark:text-slate-300">
                  <input
                    type="checkbox"
                    checked={botForm.allow_c2c}
                    onChange={(e) => setBotForm({ ...botForm, allow_c2c: e.target.checked })}
                    className="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                  />
                  <span>允许私聊 (C2C)</span>
                </label>
                <label className="flex cursor-pointer items-center gap-2 text-xs text-slate-700 dark:text-slate-300">
                  <input
                    type="checkbox"
                    checked={botForm.allow_group}
                    onChange={(e) => setBotForm({ ...botForm, allow_group: e.target.checked })}
                    className="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                  />
                  <span>允许群聊 @ 机器人</span>
                </label>
                <label className="flex cursor-pointer items-center gap-2 text-xs text-slate-700 dark:text-slate-300">
                  <input
                    type="checkbox"
                    checked={botForm.restrict_to_owner}
                    onChange={(e) =>
                      setBotForm({ ...botForm, restrict_to_owner: e.target.checked })
                    }
                    className="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                  />
                  <span>私聊仅限绑定主人</span>
                </label>
              </div>

              <div>
                <label className="mb-1 block text-xs text-slate-600 dark:text-slate-400">
                  私聊用户 OpenID 白名单（留空不额外限制，逗号或换行分隔）
                </label>
                <textarea
                  rows={2}
                  value={botForm.allowed_users_text}
                  onChange={(e) => setBotForm({ ...botForm, allowed_users_text: e.target.value })}
                  placeholder="每行一个 user_openid"
                  className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-800 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
                />
              </div>

              <div>
                <label className="mb-1 block text-xs text-slate-600 dark:text-slate-400">
                  群聊 Group OpenID 白名单（留空允许全部群，逗号或换行分隔）
                </label>
                <textarea
                  rows={2}
                  value={botForm.allowed_groups_text}
                  onChange={(e) => setBotForm({ ...botForm, allowed_groups_text: e.target.value })}
                  placeholder="每行一个 group_openid"
                  className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-800 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
                />
              </div>
            </div>

            <div className="mt-3 flex items-center justify-between gap-2">
              <div className="text-[11px] text-slate-500 dark:text-slate-400">
                支持命令：<code>/new</code> <code>/status</code> <code>/workspace</code>{' '}
                <code>/model</code> <code>/permission</code> <code>/stop</code> <code>/help</code>
              </div>
              <button
                type="button"
                disabled={savingCfg}
                onClick={handleSaveBotConfig}
                className="flex shrink-0 items-center gap-1.5 rounded-xl bg-indigo-600 px-3.5 py-2 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
              >
                {savingCfg ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Save className="h-3.5 w-3.5" />
                )}
                <span>保存 QQ 配置</span>
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Last error & feedback */}
      {status?.last_error && (
        <div className="mt-3 flex items-start gap-1.5 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>最近异常：{status.last_error}</span>
        </div>
      )}

      {msg && (
        <div
          className={`mt-2 flex items-center gap-1.5 text-xs ${
            msg.ok ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-600 dark:text-rose-400'
          }`}
        >
          {msg.ok ? (
            <CheckCircle2 className="h-3.5 w-3.5 shrink-0" />
          ) : (
            <AlertCircle className="h-3.5 w-3.5 shrink-0" />
          )}
          <span>{msg.text}</span>
        </div>
      )}

      {/* Collapsible runtime logs */}
      {status && status.logs && status.logs.length > 0 && (
        <div className="mt-3">
          <button
            type="button"
            onClick={() => setShowLogs((v) => !v)}
            className="text-xs font-medium text-indigo-600 hover:underline dark:text-indigo-400"
          >
            {showLogs ? '隐藏运行日志' : `查看运行日志（最近 ${status.logs.length} 行）`}
          </button>
          {showLogs && (
            <div className="mt-2 max-h-48 overflow-y-auto rounded-xl border border-slate-200 bg-slate-900 p-3 font-mono text-[11px] text-slate-200 dark:border-slate-800 dark:bg-black/70">
              {status.logs.map((line, idx) => (
                <div key={idx} className="whitespace-pre-wrap break-all">
                  {line}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      <DirPickerModal
        open={pickerOpen}
        initialPath={botForm.workspace || defaultWorkspace || ''}
        title="选择 QQ 机器人默认工作区"
        onClose={() => setPickerOpen(false)}
        onSelect={(dir) => {
          setBotForm((prev) => ({ ...prev, workspace: dir }));
          setPickerOpen(false);
        }}
      />
    </section>
  );
};
