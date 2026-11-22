import React, { useEffect, useMemo, useRef, useState } from 'react';
import ReactMarkdown, { defaultUrlTransform } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  Send,
  Square,
  FolderOpen,
  Plus,
  X,
  Terminal,
  Brain,
  Wrench,
  ChevronDown,
  ChevronRight,
  Sparkles,
  AlertTriangle,
  Cpu,
  Shield,
  CheckCircle2,
  Loader2,
  Gauge,
  RefreshCw,
  SlidersHorizontal,
  Paperclip,
  Image as ImageIcon,
  FileText,
  Zap,
  Slash,
} from 'lucide-react';
import { api, apiUrl, streamSessionChat } from '../api/client';
import type {
  AgyStatus,
  AppSettings,
  Attachment,
  ChatMessage,
  ChatSession,
  ModelOption,
  PermissionRequest,
  ToolCall,
} from '../types';
import { DirPickerModal } from './DirPickerModal';
import { extractLocalFilePath, openPathInFnOS } from '../utils/fnosBridge';

const markdownUrlTransform = (url: string): string => {
  if (/^file:\/\//i.test(url)) return url;
  return defaultUrlTransform(url);
};

interface SlashCommandItem {
  cmd: string;
  desc: string;
  category: 'workflow' | 'session' | 'tools';
}

const SLASH_COMMANDS: SlashCommandItem[] = [
  { cmd: '/plan', desc: '先制定详细实现计划再执行', category: 'workflow' },
  { cmd: '/grill-me', desc: '交互式追问并对齐方案细节', category: 'workflow' },
  { cmd: '/goal', desc: '设定长期目标并持续推进', category: 'workflow' },
  { cmd: '/boost', desc: '增强当前任务的推理与规划深度', category: 'workflow' },
  { cmd: '/swarm', desc: '多代理并行协作处理复杂任务', category: 'workflow' },
  { cmd: '/teamwork', desc: '启动团队协作模式拆分执行', category: 'workflow' },
  { cmd: '/learn', desc: '学习当前工作区规范与项目结构', category: 'workflow' },
  { cmd: '/compact', desc: '压缩当前对话上下文释放空间', category: 'session' },
  { cmd: '/btw', desc: '顺带提问而不打断当前主线', category: 'session' },
  { cmd: '/context', desc: '查看当前上下文占用情况', category: 'session' },
  { cmd: '/usage', desc: '查看 Token 与配额使用统计', category: 'session' },
  { cmd: '/model', desc: '查看或切换当前对话模型', category: 'session' },
  { cmd: '/effort', desc: '查看或调整当前思考强度', category: 'session' },
  { cmd: '/agents', desc: '查看可用的子代理列表', category: 'tools' },
  { cmd: '/diff', desc: '查看当前工作区代码变更', category: 'tools' },
  { cmd: '/codesearch', desc: '在工作区中快速检索代码', category: 'tools' },
  { cmd: '/permissions', desc: '查看当前权限与工具执行策略', category: 'tools' },
];

function formatFileSize(bytes: number): string {
  if (!bytes || bytes < 1024) return `${bytes || 0} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

interface ChatViewProps {
  session: ChatSession | null;
  settings: AppSettings | null;
  agyStatus: AgyStatus | null;
  models: ModelOption[];
  onRefreshModels: () => void;
  onRefreshStatus: () => void;
  onSessionUpdated: (sess: ChatSession) => void;
  onCreateSession: () => Promise<ChatSession | null>;
  onOpenInTerminal: (workspace: string, conversationId?: string) => void;
  onGoToSettings: () => void;
  onGoToTerminal: () => void;
}

const ThinkingBlock: React.FC<{ thinking: string; defaultOpen?: boolean }> = ({
  thinking,
  defaultOpen = false,
}) => {
  const [open, setOpen] = useState(defaultOpen);
  if (!thinking) return null;

  return (
    <div className="mb-3 overflow-hidden rounded-xl border border-indigo-200 bg-indigo-50/70 dark:border-indigo-500/20 dark:bg-indigo-950/20">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs font-medium text-indigo-700 hover:bg-indigo-100/70 dark:text-indigo-300 dark:hover:bg-indigo-500/10"
      >
        <Brain className="h-3.5 w-3.5" />
        <span>思考过程</span>
        <span className="ml-auto text-slate-400">
          {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
        </span>
      </button>
      {open && (
        <div className="border-t border-indigo-200/80 px-3 py-2.5 font-mono text-xs whitespace-pre-wrap text-slate-700 dark:border-indigo-500/15 dark:text-slate-300/90">
          {thinking}
        </div>
      )}
    </div>
  );
};

const ToolCallCard: React.FC<{ tool: ToolCall }> = ({ tool }) => {
  const [open, setOpen] = useState(false);
  return (
    <div className="overflow-hidden rounded-xl border border-slate-200 bg-slate-50 text-xs dark:border-slate-700/80 dark:bg-slate-950/70">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-slate-100 dark:hover:bg-slate-800/60"
      >
        {tool.status === 'running' ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin text-amber-500 dark:text-amber-400" />
        ) : tool.status === 'error' ? (
          <AlertTriangle className="h-3.5 w-3.5 text-rose-500 dark:text-rose-400" />
        ) : (
          <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500 dark:text-emerald-400" />
        )}
        <Wrench className="h-3.5 w-3.5 text-slate-400" />
        <span className="font-mono font-medium text-slate-800 dark:text-slate-200">
          {tool.name || 'Tool'}
        </span>
        <span className="ml-auto text-[10px] uppercase text-slate-400 dark:text-slate-500">
          {tool.status}
        </span>
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 text-slate-400" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 text-slate-400" />
        )}
      </button>
      {open && (
        <div className="space-y-2 border-t border-slate-200 p-3 font-mono text-[11px] dark:border-slate-800">
          {tool.input && (
            <div>
              <div className="mb-1 text-slate-500">输入参数</div>
              <pre className="max-h-48 overflow-auto rounded-lg bg-white p-2 text-slate-800 ring-1 ring-slate-200 dark:bg-slate-900 dark:text-slate-300 dark:ring-0">
                {tool.input}
              </pre>
            </div>
          )}
          {tool.output && (
            <div>
              <div className="mb-1 text-slate-500">执行输出</div>
              <pre className="max-h-56 overflow-auto rounded-lg bg-white p-2 text-slate-800 ring-1 ring-slate-200 dark:bg-slate-900 dark:text-slate-300 dark:ring-0">
                {tool.output}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export const ChatView: React.FC<ChatViewProps> = ({
  session,
  settings,
  agyStatus,
  models,
  onRefreshModels,
  onRefreshStatus,
  onSessionUpdated,
  onCreateSession,
  onOpenInTerminal,
  onGoToSettings,
  onGoToTerminal,
}) => {
  const [input, setInput] = useState('');
  const [streaming, setStreaming] = useState(false);
  const [draftMsg, setDraftMsg] = useState<ChatMessage | null>(null);
  const [pickerMode, setPickerMode] = useState<'workspace' | 'extra' | null>(null);
  const [mobileConfigOpen, setMobileConfigOpen] = useState(false);

  // Attachments state
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const [uploadingCount, setUploadingCount] = useState(0);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);

  // Slash command palette state
  const [slashManualOpen, setSlashManualOpen] = useState(false);
  const [slashIndex, setSlashIndex] = useState(0);

  // Interactive ACP permission approval state
  const [pendingPerms, setPendingPerms] = useState<PermissionRequest[]>([]);
  const [submittingPermId, setSubmittingPermId] = useState<string | null>(null);

  const abortRef = useRef<AbortController | null>(null);
  const bottomRef = useRef<HTMLDivElement | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const imageInputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [
    session?.messages.length,
    draftMsg?.content,
    draftMsg?.thinking,
    draftMsg?.tool_calls?.length,
    pendingPerms.length,
  ]);

  // Clear pending attachments & permissions when switching sessions
  useEffect(() => {
    setPendingAttachments([]);
    setUploadError(null);
    setPendingPerms([]);
    setSubmittingPermId(null);
  }, [session?.id]);

  const handlePermissionDecision = async (
    req: PermissionRequest,
    optionId?: string,
    cancelled = false
  ) => {
    if (submittingPermId === req.request_id) return;
    setSubmittingPermId(req.request_id);
    try {
      await api.respondPermission(req.session_id, {
        request_id: req.request_id,
        option_id: optionId,
        cancelled,
      });
      setPendingPerms((prev) => prev.filter((p) => p.request_id !== req.request_id));
    } catch {
      // Remove stale prompt if already expired
      setPendingPerms((prev) => prev.filter((p) => p.request_id !== req.request_id));
    } finally {
      setSubmittingPermId(null);
    }
  };

  const [fileToast, setFileToast] = useState<{ ok: boolean; text: string } | null>(null);
  const fileToastTimerRef = useRef<number | null>(null);

  const showFileToast = (ok: boolean, text: string) => {
    setFileToast({ ok, text });
    if (fileToastTimerRef.current) {
      window.clearTimeout(fileToastTimerRef.current);
    }
    fileToastTimerRef.current = window.setTimeout(() => {
      setFileToast(null);
      fileToastTimerRef.current = null;
    }, 3500);
  };

  useEffect(() => {
    return () => {
      if (fileToastTimerRef.current) {
        window.clearTimeout(fileToastTimerRef.current);
      }
    };
  }, []);

  const handleOpenLocalFile = async (path: string, isDir: boolean) => {
    const res = await openPathInFnOS(path, { isDir });
    showFileToast(res.ok, res.message);
  };

  const markdownComponents = useMemo(
    () => ({
      a: ({
        href,
        children,
        ...props
      }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { children?: React.ReactNode }) => {
        const local = extractLocalFilePath(href);
        if (local) {
          return (
            <button
              type="button"
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                void handleOpenLocalFile(local.path, local.isDir);
              }}
              title={`点击用飞牛系统打开：${local.path}${local.lineRange ? ` (${local.lineRange})` : ''}`}
              className="mx-0.5 inline-flex max-w-full cursor-pointer items-center gap-1 rounded-md border border-indigo-200 bg-indigo-50/80 px-1.5 py-0.5 align-baseline font-mono text-[0.85em] text-indigo-700 transition hover:border-indigo-400 hover:bg-indigo-100 dark:border-indigo-500/30 dark:bg-indigo-500/15 dark:text-indigo-300 dark:hover:border-indigo-400/60 dark:hover:bg-indigo-500/25"
            >
              {local.isDir ? (
                <FolderOpen className="h-3 w-3 shrink-0 text-amber-500 dark:text-amber-400" />
              ) : (
                <FileText className="h-3 w-3 shrink-0 text-indigo-500 dark:text-indigo-400" />
              )}
              <span className="truncate">{children}</span>
            </button>
          );
        }
        return (
          <a href={href} target="_blank" rel="noopener noreferrer" {...props}>
            {children}
          </a>
        );
      },
    }),
    []
  );

  // Determine whether slash command popover should be shown
  const slashQuery = useMemo(() => {
    if (/^\/\S*$/.test(input)) {
      return input.toLowerCase();
    }
    return null;
  }, [input]);

  const slashOpen = slashManualOpen || slashQuery !== null;

  const filteredCommands = useMemo(() => {
    const q = slashQuery ?? '/';
    if (q === '/') return SLASH_COMMANDS;
    return SLASH_COMMANDS.filter(
      (c) => c.cmd.toLowerCase().includes(q) || c.desc.toLowerCase().includes(q.slice(1))
    );
  }, [slashQuery]);

  useEffect(() => {
    setSlashIndex(0);
  }, [slashQuery, slashManualOpen]);

  const applySlashCommand = (cmd: string) => {
    setInput(`${cmd} `);
    setSlashManualOpen(false);
    requestAnimationFrame(() => {
      textareaRef.current?.focus();
    });
  };

  const updateMeta = async (patch: {
    workspace?: string;
    extra_dirs?: string[];
    model?: string;
    effort?: string;
    permission_mode?: string;
  }) => {
    if (!session) return;
    try {
      const updated = await api.updateSession(session.id, patch);
      onSessionUpdated(updated);
    } catch {
      // ignore
    }
  };

  const handleUploadFiles = async (fileList: FileList | File[]) => {
    const files = Array.from(fileList);
    if (files.length === 0) return;

    setUploadError(null);
    let active = session;
    if (!active) {
      active = await onCreateSession();
      if (!active) {
        setUploadError('无法创建会话以保存附件');
        return;
      }
    }

    setUploadingCount((c) => c + files.length);
    for (const file of files) {
      try {
        const uploaded = await api.uploadFile(file, active.id);
        setPendingAttachments((prev) => [...prev, uploaded]);
      } catch (err) {
        setUploadError(err instanceof Error ? err.message : `上传 ${file.name} 失败`);
      } finally {
        setUploadingCount((c) => Math.max(0, c - 1));
      }
    }
  };

  const handleRemoveAttachment = (id: string) => {
    setPendingAttachments((prev) => prev.filter((a) => a.id !== id));
  };

  const handlePaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const items = e.clipboardData?.files;
    if (items && items.length > 0) {
      e.preventDefault();
      handleUploadFiles(items);
    }
  };

  const handleDrop = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setDragOver(false);
    if (e.dataTransfer?.files && e.dataTransfer.files.length > 0) {
      handleUploadFiles(e.dataTransfer.files);
    }
  };

  const handleSend = async (overridePrompt?: string) => {
    const prompt = (overridePrompt ?? input).trim();
    const attachmentsToSend = overridePrompt !== undefined ? [] : pendingAttachments;
    if ((!prompt && attachmentsToSend.length === 0) || streaming || uploadingCount > 0) return;

    let active = session;
    if (!active) {
      active = await onCreateSession();
      if (!active) return;
    }

    setInput('');
    setSlashManualOpen(false);
    if (overridePrompt === undefined) {
      setPendingAttachments([]);
      setUploadError(null);
    }
    setStreaming(true);

    const optimisticUser: ChatMessage = {
      id: `local-${Date.now()}`,
      role: 'user',
      content: prompt,
      attachments: attachmentsToSend.length > 0 ? attachmentsToSend : undefined,
      created_at: new Date().toISOString(),
    };
    const updatedLocal: ChatSession = {
      ...active,
      messages: [...active.messages, optimisticUser],
    };
    onSessionUpdated(updatedLocal);

    const liveAssistant: ChatMessage = {
      id: `stream-${Date.now()}`,
      role: 'assistant',
      content: '',
      thinking: '',
      tool_calls: [],
      created_at: new Date().toISOString(),
    };
    setDraftMsg({ ...liveAssistant });

    const controller = new AbortController();
    abortRef.current = controller;

    try {
      await streamSessionChat(
        active.id,
        {
          prompt,
          workspace: active.workspace,
          extra_dirs: active.extra_dirs,
          model: active.model,
          effort: active.effort,
          permission_mode: active.permission_mode,
          attachments: attachmentsToSend.length > 0 ? attachmentsToSend : undefined,
        },
        (ev) => {
          if (ev.type === 'thinking' && ev.delta) {
            liveAssistant.thinking = (liveAssistant.thinking || '') + ev.delta;
            setDraftMsg({ ...liveAssistant });
          } else if (ev.type === 'text' && ev.delta) {
            liveAssistant.content += ev.delta;
            setDraftMsg({ ...liveAssistant });
          } else if ((ev.type === 'tool_call' || ev.type === 'tool_result') && ev.tool_call) {
            const list = [...(liveAssistant.tool_calls || [])];
            const idx = list.findIndex((t) => t.id === ev.tool_call!.id);
            if (idx >= 0) {
              list[idx] = { ...list[idx], ...ev.tool_call };
            } else {
              list.push(ev.tool_call);
            }
            liveAssistant.tool_calls = list;
            setDraftMsg({ ...liveAssistant });
          } else if (ev.type === 'permission_request' && ev.permission_request) {
            const req = ev.permission_request;
            setPendingPerms((prev) =>
              prev.some((p) => p.request_id === req.request_id) ? prev : [...prev, req]
            );
          } else if (ev.type === 'permission_resolved' && ev.permission_request) {
            const rid = ev.permission_request.request_id;
            setPendingPerms((prev) => prev.filter((p) => p.request_id !== rid));
          } else if (ev.type === 'usage' && ev.usage) {
            liveAssistant.usage = ev.usage;
            setDraftMsg({ ...liveAssistant });
          } else if (ev.type === 'error' && ev.error) {
            liveAssistant.error = ev.error;
            setDraftMsg({ ...liveAssistant });
          }
        },
        controller.signal
      );
    } catch (err) {
      if ((err as Error).name !== 'AbortError') {
        liveAssistant.error = err instanceof Error ? err.message : '请求失败';
        setDraftMsg({ ...liveAssistant });
      }
    } finally {
      abortRef.current = null;
      setStreaming(false);
      setDraftMsg(null);
      setPendingPerms([]);
      setSubmittingPermId(null);
      try {
        const fresh = await api.getSession(active.id);
        onSessionUpdated(fresh);
      } catch {
        // ignore
      }
      onRefreshStatus();
    }
  };

  const handleStop = () => {
    abortRef.current?.abort();
  };

  const workspace = session?.workspace || settings?.default_workspace || '/vol1';
  const extraDirs = session?.extra_dirs || [];
  const model = session?.model ?? settings?.default_model ?? '';
  const effort = session?.effort ?? settings?.default_effort ?? '';
  const permMode = session?.permission_mode || settings?.default_permission_mode || 'default';
  const idleMinutes = settings?.persistent_idle_minutes ?? 10;
  const isWarm = Boolean(session?.id && agyStatus?.warm_sessions?.includes(session.id));

  const messages = session?.messages || [];

  return (
    <div className="flex h-full flex-col bg-slate-50 dark:bg-slate-950">
      {/* Clean 2-row top toolbar */}
      <header className="shrink-0 border-b border-slate-200 bg-white/95 px-3 py-2 backdrop-blur sm:px-4 dark:border-slate-800/90 dark:bg-slate-900/80">
        {/* Row 1: Workspace path + Extra dirs + Actions (always single aligned row) */}
        <div className="flex items-center gap-2">
          <div className="flex min-w-0 flex-1 items-center gap-1.5 overflow-x-auto py-0.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
            <button
              onClick={() => setPickerMode('workspace')}
              className="flex shrink-0 items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1.5 text-xs text-slate-700 transition hover:border-indigo-500/60 dark:border-slate-700/80 dark:bg-slate-950 dark:text-slate-200"
              title={`切换工作区目录 (cwd): ${workspace}`}
            >
              <FolderOpen className="h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
              <span className="max-w-[160px] truncate font-mono sm:max-w-[260px]">
                {workspace}
              </span>
            </button>

            {extraDirs.map((dir) => (
              <span
                key={dir}
                className="flex shrink-0 items-center gap-1 rounded-lg border border-slate-200 bg-slate-100 px-2 py-1 font-mono text-[11px] text-slate-700 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-300"
                title={dir}
              >
                <span className="max-w-[110px] truncate">+ {dir}</span>
                <button
                  onClick={() =>
                    updateMeta({ extra_dirs: extraDirs.filter((d) => d !== dir) })
                  }
                  className="text-slate-400 hover:text-rose-500"
                >
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))}

            <button
              onClick={() => setPickerMode('extra')}
              className="flex shrink-0 items-center gap-1 rounded-lg border border-dashed border-slate-300 px-2 py-1.5 text-[11px] text-slate-500 transition hover:border-indigo-400 hover:text-indigo-600 dark:border-slate-700 dark:text-slate-400 dark:hover:border-slate-500 dark:hover:text-slate-200"
              title="添加附加上下文目录 (--add-dir)"
            >
              <Plus className="h-3 w-3" />
              <span className="hidden sm:inline">附加目录</span>
            </button>
          </div>

          {/* Right actions */}
          <div className="flex shrink-0 items-center gap-1.5">
            <button
              onClick={() => setMobileConfigOpen((v) => !v)}
              className={`flex items-center gap-1 rounded-lg border px-2 py-1.5 text-xs transition lg:hidden ${
                mobileConfigOpen
                  ? 'border-indigo-400 bg-indigo-50 text-indigo-700 dark:border-indigo-500/50 dark:bg-indigo-500/15 dark:text-indigo-300'
                  : 'border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-300'
              }`}
              title="展开/收起模型与运行参数"
            >
              <SlidersHorizontal className="h-3.5 w-3.5" />
              <span>参数</span>
            </button>

            <button
              onClick={() => onOpenInTerminal(workspace, session?.conversation_id)}
              className="flex items-center gap-1.5 rounded-lg border border-indigo-200 bg-indigo-50 px-2.5 py-1.5 text-xs font-medium text-indigo-700 transition hover:bg-indigo-100 dark:border-indigo-500/30 dark:bg-indigo-500/10 dark:text-indigo-300 dark:hover:bg-indigo-500/20"
              title="在内置终端中继续当前工作区或会话"
            >
              <Terminal className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">终端继续</span>
            </button>
          </div>
        </div>

        {/* Row 2: Model, Effort, Permission Mode & Persistent Connection Indicator */}
        <div
          className={`${
            mobileConfigOpen ? 'mt-2 grid grid-cols-1 gap-2 border-t border-slate-100 pt-2 sm:grid-cols-3 dark:border-slate-800/80' : 'hidden'
          } lg:mt-2 lg:flex lg:flex-wrap lg:items-center lg:gap-2 lg:border-t lg:border-slate-100 lg:pt-2 dark:lg:border-slate-800/80`}
        >
          {/* Model selector */}
          <div className="flex min-w-0 items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs dark:border-slate-800 dark:bg-slate-950">
            <Cpu className="h-3.5 w-3.5 shrink-0 text-slate-400" />
            {models.length > 0 ? (
              <select
                value={model}
                onChange={(e) => updateMeta({ model: e.target.value })}
                title="选择模型"
                className="min-w-0 flex-1 bg-transparent text-xs text-slate-700 focus:outline-none lg:max-w-[200px] dark:text-slate-200"
              >
                <option value="" className="bg-white dark:bg-slate-900">
                  默认模型 (CLI 自动)
                </option>
                {models.map((m) => (
                  <option key={m.id} value={m.id} className="bg-white dark:bg-slate-900">
                    {m.label}
                  </option>
                ))}
                {model && !models.some((m) => m.id === model) && (
                  <option value={model} className="bg-white dark:bg-slate-900">
                    {model}
                  </option>
                )}
              </select>
            ) : (
              <input
                type="text"
                value={model}
                onChange={(e) => updateMeta({ model: e.target.value })}
                placeholder="默认模型"
                className="min-w-0 flex-1 bg-transparent text-xs text-slate-700 placeholder-slate-400 focus:outline-none lg:w-32 dark:text-slate-200 dark:placeholder-slate-500"
              />
            )}
            <button
              onClick={onRefreshModels}
              title="刷新可用模型列表 (agy models)"
              className="shrink-0 rounded p-0.5 text-slate-400 hover:text-indigo-500"
            >
              <RefreshCw className="h-3 w-3" />
            </button>
          </div>

          {/* Reasoning Effort selector */}
          <div className="flex min-w-0 items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs dark:border-slate-800 dark:bg-slate-950">
            <Gauge className="h-3.5 w-3.5 shrink-0 text-slate-400" />
            <select
              value={effort}
              onChange={(e) => updateMeta({ effort: e.target.value })}
              title="思考强度 (--effort)"
              className="min-w-0 flex-1 bg-transparent text-xs text-slate-700 focus:outline-none dark:text-slate-200"
            >
              <option value="" className="bg-white dark:bg-slate-900">
                思考: 默认
              </option>
              <option value="low" className="bg-white dark:bg-slate-900">
                思考: 低 (low)
              </option>
              <option value="medium" className="bg-white dark:bg-slate-900">
                思考: 中 (medium)
              </option>
              <option value="high" className="bg-white dark:bg-slate-900">
                思考: 高 (high)
              </option>
              <option value="xhigh" className="bg-white dark:bg-slate-900">
                思考: 极高 (xhigh)
              </option>
              <option value="max" className="bg-white dark:bg-slate-900">
                思考: 最大 (max)
              </option>
            </select>
          </div>

          {/* Permission Mode */}
          <div className="flex min-w-0 items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs dark:border-slate-800 dark:bg-slate-950">
            <Shield className="h-3.5 w-3.5 shrink-0 text-slate-400" />
            <select
              value={permMode}
              onChange={(e) => updateMeta({ permission_mode: e.target.value })}
              className="min-w-0 flex-1 bg-transparent text-xs text-slate-700 focus:outline-none dark:text-slate-200"
            >
              <option value="default" className="bg-white dark:bg-slate-900">
                标准模式 (default)
              </option>
              <option value="accept-edits" className="bg-white dark:bg-slate-900">
                自动接受编辑 (accept-edits)
              </option>
              <option value="plan" className="bg-white dark:bg-slate-900">
                仅规划 (plan)
              </option>
              {settings?.allow_dangerous_skip && (
                <option value="bypass" className="bg-white dark:bg-slate-900">
                  跳过权限确认 (危险)
                </option>
              )}
            </select>
          </div>

          {/* Persistent connection status pill */}
          <button
            onClick={onGoToSettings}
            title="点击前往设置调整长连接空闲超时时间"
            className={`flex items-center justify-center gap-1.5 rounded-lg border px-2.5 py-1 text-[11px] transition sm:col-span-3 lg:ml-auto lg:justify-start ${
              idleMinutes > 0 && isWarm
                ? 'border-emerald-200 bg-emerald-50 text-emerald-700 hover:bg-emerald-100/80 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'
                : 'border-slate-200 bg-slate-50 text-slate-500 hover:border-slate-300 hover:text-slate-700 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-400 dark:hover:text-slate-200'
            }`}
          >
            <Zap
              className={`h-3 w-3 shrink-0 ${
                idleMinutes > 0 && isWarm ? 'text-emerald-500 dark:text-emerald-400' : 'text-slate-400'
              }`}
            />
            {idleMinutes > 0 ? (
              isWarm ? (
                <span>长连接就绪 · {idleMinutes}m 空闲释放</span>
              ) : (
                <span>长连接按需启动 · {idleMinutes}m 超时</span>
              )
            ) : (
              <span>单次执行模式</span>
            )}
          </button>
        </div>
      </header>

      {/* Setup warning banner if agy missing or unauthenticated */}
      {agyStatus && (!agyStatus.installed || !agyStatus.authenticated) && (
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 sm:px-4 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">
          <div className="flex items-center gap-2">
            <AlertTriangle className="h-4 w-4 shrink-0 text-amber-500 dark:text-amber-400" />
            {!agyStatus.installed ? (
              <span>尚未检测到 Antigravity CLI (`agy`)，请先前往设置中心一键安装。</span>
            ) : (
              <span>尚未检测到登录凭据，请先在「内置 Web 终端」中运行一次 `agy` 完成 OAuth 登录。</span>
            )}
          </div>
          {!agyStatus.installed ? (
            <button
              onClick={onGoToSettings}
              className="rounded-lg bg-amber-200/70 px-2.5 py-1 font-medium text-amber-900 hover:bg-amber-200 dark:bg-amber-500/20 dark:text-amber-100 dark:hover:bg-amber-500/30"
            >
              前往安装
            </button>
          ) : (
            <button
              onClick={onGoToTerminal}
              className="rounded-lg bg-amber-200/70 px-2.5 py-1 font-medium text-amber-900 hover:bg-amber-200 dark:bg-amber-500/20 dark:text-amber-100 dark:hover:bg-amber-500/30"
            >
              打开终端登录
            </button>
          )}
        </div>
      )}

      {/* Messages area */}
      <div className="flex-1 overflow-y-auto px-3 py-4 sm:px-4 sm:py-6">
        <div className="mx-auto max-w-4xl space-y-4 sm:space-y-5">
          {messages.length === 0 && !draftMsg ? (
            <div className="flex flex-col items-center justify-center py-10 text-center sm:py-16">
              <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-indigo-50 ring-1 ring-indigo-200 dark:bg-gradient-to-br dark:from-indigo-500/20 dark:to-violet-600/20 dark:ring-indigo-500/30">
                <Sparkles className="h-7 w-7 text-indigo-600 dark:text-indigo-400" />
              </div>
              <h2 className="text-lg font-semibold text-slate-900 dark:text-white">
                开始与 Antigravity 对话
              </h2>
              <p className="mt-1.5 max-w-md text-xs leading-relaxed text-slate-500 dark:text-slate-400">
                当前工作区：<span className="break-all font-mono text-slate-700 dark:text-slate-300">{workspace}</span>
                <br />
                支持粘贴/上传图片与文件，输入 <span className="font-mono text-indigo-600 dark:text-indigo-400">/</span> 可快速选择 CLI 命令。
              </p>

              <div className="mt-6 grid w-full max-w-xl grid-cols-1 gap-2 sm:grid-cols-2">
                {[
                  '总结当前工作区目录的结构和主要文件',
                  '检查当前目录的 Git 状态与最近提交记录',
                  '帮我写一个自动整理当前目录文件的脚本',
                  '分析当前项目中的配置文件并给出优化建议',
                ].map((sample) => (
                  <button
                    key={sample}
                    onClick={() => handleSend(sample)}
                    className="rounded-xl border border-slate-200 bg-white p-3 text-left text-xs text-slate-700 shadow-xs transition hover:border-indigo-400 hover:text-indigo-700 dark:border-slate-800 dark:bg-slate-900/70 dark:text-slate-300 dark:hover:border-indigo-500/40 dark:hover:bg-slate-900 dark:hover:text-white"
                  >
                    {sample}
                  </button>
                ))}
              </div>
            </div>
          ) : (
            [...messages, ...(draftMsg ? [draftMsg] : [])].map((msg) => (
              <div
                key={msg.id}
                className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}
              >
                {msg.role === 'user' ? (
                  <div className="max-w-[88%] space-y-2 rounded-2xl rounded-tr-sm bg-indigo-600 px-3.5 py-2.5 text-sm text-white shadow sm:max-w-[82%] sm:px-4">
                    {msg.attachments && msg.attachments.length > 0 && (
                      <div className="flex flex-wrap gap-2">
                        {msg.attachments.map((att) =>
                          att.is_image ? (
                            <a
                              key={att.id}
                              href={apiUrl(att.url)}
                              target="_blank"
                              rel="noreferrer"
                              className="block overflow-hidden rounded-xl border border-white/20 bg-black/15"
                              title={att.name}
                            >
                              <img
                                src={apiUrl(att.url)}
                                alt={att.name}
                                className="max-h-48 max-w-[220px] object-contain"
                              />
                            </a>
                          ) : (
                            <a
                              key={att.id}
                              href={apiUrl(att.url)}
                              target="_blank"
                              rel="noreferrer"
                              className="flex items-center gap-1.5 rounded-lg bg-white/15 px-2.5 py-1.5 text-xs text-white transition hover:bg-white/25"
                            >
                              <FileText className="h-3.5 w-3.5 shrink-0" />
                              <span className="max-w-[160px] truncate">{att.name}</span>
                              <span className="text-[10px] text-indigo-100/80">
                                {formatFileSize(att.size)}
                              </span>
                            </a>
                          )
                        )}
                      </div>
                    )}
                    {msg.content && <div className="whitespace-pre-wrap">{msg.content}</div>}
                  </div>
                ) : (
                  <div className="w-full max-w-full rounded-2xl border border-slate-200 bg-white p-3.5 text-sm text-slate-800 shadow-xs sm:max-w-[92%] sm:p-4 dark:border-slate-800/90 dark:bg-slate-900/80 dark:text-slate-100">
                    {msg.thinking && (
                      <ThinkingBlock
                        thinking={msg.thinking}
                        defaultOpen={msg.id.startsWith('stream-') && !msg.content}
                      />
                    )}

                    {msg.tool_calls && msg.tool_calls.length > 0 && (
                      <div className="mb-3 space-y-2">
                        {msg.tool_calls.map((tc) => (
                          <ToolCallCard key={tc.id} tool={tc} />
                        ))}
                      </div>
                    )}

                    {msg.content ? (
                      <div className="chat-markdown min-w-0 overflow-x-auto">
                        <ReactMarkdown
                          remarkPlugins={[remarkGfm]}
                          urlTransform={markdownUrlTransform}
                          components={markdownComponents}
                        >
                          {msg.content}
                        </ReactMarkdown>
                      </div>
                    ) : msg.id.startsWith('stream-') && !msg.error ? (
                      <div className="flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                        <Loader2 className="h-3.5 w-3.5 animate-spin text-indigo-500 dark:text-indigo-400" />
                        <span>Antigravity 正在思考与执行...</span>
                      </div>
                    ) : null}

                    {msg.error && (
                      <div className="mt-3 rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 font-mono text-xs whitespace-pre-wrap text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-300">
                        {msg.error}
                      </div>
                    )}

                    {msg.usage && (
                      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-slate-200 pt-2 text-[11px] text-slate-400 dark:border-slate-800/80 dark:text-slate-500">
                        <span>输入: {msg.usage.input_tokens ?? 0}</span>
                        <span>输出: {msg.usage.output_tokens ?? 0}</span>
                        <span>总计: {msg.usage.total_tokens ?? 0} tokens</span>
                      </div>
                    )}
                  </div>
                )}
              </div>
            ))
          )}

          {/* Interactive ACP Permission Approval Cards */}
          {pendingPerms.map((perm) => {
            const busy = submittingPermId === perm.request_id;
            return (
              <div
                key={perm.request_id}
                className="rounded-2xl border-2 border-amber-300 bg-amber-50/90 p-4 shadow-sm dark:border-amber-500/50 dark:bg-amber-950/30"
              >
                <div className="flex items-start gap-2.5">
                  <Shield className="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-xs font-semibold text-amber-900 dark:text-amber-200">
                        请求操作权限确认
                      </span>
                      {perm.kind && (
                        <span className="rounded-md bg-amber-200/70 px-1.5 py-0.5 font-mono text-[10px] font-medium text-amber-900 dark:bg-amber-500/20 dark:text-amber-300">
                          {perm.kind}
                        </span>
                      )}
                    </div>
                    <div className="mt-1 text-xs font-medium text-slate-900 dark:text-slate-100">
                      {perm.title}
                    </div>
                    {perm.input && (
                      <pre className="mt-2 max-h-40 overflow-auto rounded-xl border border-amber-200/80 bg-white/90 p-2.5 font-mono text-[11px] whitespace-pre-wrap text-slate-800 dark:border-amber-500/20 dark:bg-slate-950/80 dark:text-slate-200">
                        {perm.input}
                      </pre>
                    )}
                    <div className="mt-3 flex flex-wrap items-center gap-2">
                      {perm.options && perm.options.length > 0 ? (
                        perm.options.map((opt) => {
                          const isAllowOnce = opt.kind === 'allow_once';
                          const isAllowAlways = opt.kind === 'allow_always';
                          const isReject =
                            opt.kind === 'reject_once' || opt.kind === 'reject_always';
                          const btnClass = isAllowOnce
                            ? 'bg-emerald-600 text-white hover:bg-emerald-500'
                            : isAllowAlways
                              ? 'bg-indigo-600 text-white hover:bg-indigo-500'
                              : isReject
                                ? 'border border-rose-300 bg-white text-rose-700 hover:bg-rose-50 dark:border-rose-500/40 dark:bg-slate-900 dark:text-rose-300 dark:hover:bg-rose-500/10'
                                : 'border border-slate-300 bg-white text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200';
                          return (
                            <button
                              key={opt.option_id}
                              type="button"
                              disabled={busy}
                              onClick={() => handlePermissionDecision(perm, opt.option_id, false)}
                              className={`flex items-center gap-1.5 rounded-xl px-3 py-1.5 text-xs font-medium transition disabled:opacity-50 ${btnClass}`}
                            >
                              {busy && <Loader2 className="h-3 w-3 animate-spin" />}
                              <span>{opt.name || opt.option_id}</span>
                            </button>
                          );
                        })
                      ) : (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => handlePermissionDecision(perm, undefined, true)}
                          className="rounded-xl border border-rose-300 bg-white px-3 py-1.5 text-xs font-medium text-rose-700 hover:bg-rose-50 disabled:opacity-50 dark:border-rose-500/40 dark:bg-slate-900 dark:text-rose-300"
                        >
                          取消 / 拒绝
                        </button>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            );
          })}

          <div ref={bottomRef} />
        </div>
      </div>

      {/* File link open feedback toast */}
      {fileToast && (
        <div className="shrink-0 px-3 pb-1.5 sm:px-4">
          <div
            className={`mx-auto flex max-w-4xl items-center justify-between gap-2 rounded-xl border px-3 py-1.5 text-xs ${
              fileToast.ok
                ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'
                : 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300'
            }`}
          >
            <span className="truncate font-mono">{fileToast.text}</span>
            <button
              type="button"
              onClick={() => setFileToast(null)}
              className="shrink-0 opacity-70 hover:opacity-100"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        </div>
      )}

      {/* Input composer with Slash Command popover + File/Image upload */}
      <div className="shrink-0 border-t border-slate-200 bg-white/95 p-2.5 sm:p-4 dark:border-slate-800/90 dark:bg-slate-900/80">
        <div
          className="relative mx-auto max-w-4xl"
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={handleDrop}
        >
          {/* Floating Slash Command Picker */}
          {slashOpen && filteredCommands.length > 0 && (
            <div className="absolute right-0 bottom-full left-0 z-30 mb-2 max-h-64 overflow-y-auto rounded-2xl border border-slate-200 bg-white p-1.5 shadow-xl dark:border-slate-700 dark:bg-slate-900">
              <div className="flex items-center justify-between px-2.5 py-1 text-[11px] font-medium text-slate-400">
                <span>快捷 CLI 命令（方向键选择，Enter 填入）</span>
                <button
                  onClick={() => setSlashManualOpen(false)}
                  className="rounded p-0.5 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
              <div className="space-y-0.5">
                {filteredCommands.map((item, idx) => (
                  <button
                    key={item.cmd}
                    onMouseDown={(e) => {
                      e.preventDefault();
                      applySlashCommand(item.cmd);
                    }}
                    className={`flex w-full items-center justify-between gap-3 rounded-xl px-2.5 py-1.5 text-left text-xs transition ${
                      idx === slashIndex
                        ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-500/20 dark:text-indigo-200'
                        : 'text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800/70'
                    }`}
                  >
                    <span className="font-mono font-semibold text-indigo-600 dark:text-indigo-400">
                      {item.cmd}
                    </span>
                    <span className="truncate text-[11px] text-slate-500 dark:text-slate-400">
                      {item.desc}
                    </span>
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Hidden file inputs */}
          <input
            ref={imageInputRef}
            type="file"
            accept="image/*"
            multiple
            className="hidden"
            onChange={(e) => {
              if (e.target.files) handleUploadFiles(e.target.files);
              e.target.value = '';
            }}
          />
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="hidden"
            onChange={(e) => {
              if (e.target.files) handleUploadFiles(e.target.files);
              e.target.value = '';
            }}
          />

          <div
            className={`rounded-2xl border bg-white p-2 shadow-xs transition dark:bg-slate-950 ${
              dragOver
                ? 'border-indigo-500 ring-2 ring-indigo-500/20'
                : 'border-slate-300 focus-within:border-indigo-500 dark:border-slate-700/80'
            }`}
          >
            {/* Pending attachments preview strip */}
            {(pendingAttachments.length > 0 || uploadingCount > 0 || uploadError) && (
              <div className="mb-2 flex flex-wrap items-center gap-2 border-b border-slate-100 pb-2 dark:border-slate-800">
                {pendingAttachments.map((att) =>
                  att.is_image ? (
                    <div
                      key={att.id}
                      className="group relative h-14 w-14 overflow-hidden rounded-xl border border-slate-200 bg-slate-100 dark:border-slate-700 dark:bg-slate-900"
                      title={att.name}
                    >
                      <img
                        src={apiUrl(att.url)}
                        alt={att.name}
                        className="h-full w-full object-cover"
                      />
                      <button
                        onClick={() => handleRemoveAttachment(att.id)}
                        className="absolute top-0.5 right-0.5 rounded-full bg-black/65 p-0.5 text-white hover:bg-rose-600"
                        title="移除图片"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </div>
                  ) : (
                    <div
                      key={att.id}
                      className="flex items-center gap-1.5 rounded-xl border border-slate-200 bg-slate-50 px-2.5 py-1.5 text-xs text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
                    >
                      <FileText className="h-3.5 w-3.5 shrink-0 text-indigo-500" />
                      <span className="max-w-[140px] truncate">{att.name}</span>
                      <span className="text-[10px] text-slate-400">
                        {formatFileSize(att.size)}
                      </span>
                      <button
                        onClick={() => handleRemoveAttachment(att.id)}
                        className="text-slate-400 hover:text-rose-500"
                        title="移除文件"
                      >
                        <X className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  )
                )}

                {uploadingCount > 0 && (
                  <div className="flex items-center gap-1.5 rounded-xl border border-indigo-200 bg-indigo-50 px-2.5 py-1.5 text-xs text-indigo-600 dark:border-indigo-500/30 dark:bg-indigo-500/10 dark:text-indigo-300">
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    <span>正在上传 ({uploadingCount})...</span>
                  </div>
                )}

                {uploadError && (
                  <div className="flex items-center gap-1.5 rounded-xl border border-rose-200 bg-rose-50 px-2.5 py-1 text-xs text-rose-600 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-300">
                    <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                    <span>{uploadError}</span>
                    <button onClick={() => setUploadError(null)}>
                      <X className="h-3 w-3" />
                    </button>
                  </div>
                )}
              </div>
            )}

            {/* Textarea */}
            <textarea
              ref={textareaRef}
              rows={1}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onPaste={handlePaste}
              onKeyDown={(e) => {
                if (slashOpen && filteredCommands.length > 0) {
                  if (e.key === 'ArrowDown') {
                    e.preventDefault();
                    setSlashIndex((i) => (i + 1) % filteredCommands.length);
                    return;
                  }
                  if (e.key === 'ArrowUp') {
                    e.preventDefault();
                    setSlashIndex((i) => (i - 1 + filteredCommands.length) % filteredCommands.length);
                    return;
                  }
                  if (e.key === 'Enter' || e.key === 'Tab') {
                    e.preventDefault();
                    applySlashCommand(filteredCommands[slashIndex].cmd);
                    return;
                  }
                  if (e.key === 'Escape') {
                    e.preventDefault();
                    setSlashManualOpen(false);
                    return;
                  }
                }
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault();
                  handleSend();
                }
              }}
              placeholder="输入指令或问题，输入 / 选择命令，支持粘贴或拖拽图片/文件..."
              className="max-h-36 min-h-[40px] w-full resize-none bg-transparent px-2 py-1.5 text-base text-slate-900 placeholder-slate-400 focus:outline-none sm:text-sm dark:text-white dark:placeholder-slate-500"
            />

            {/* Bottom action bar inside composer */}
            <div className="mt-1 flex items-center justify-between gap-2 pt-1">
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  onClick={() => imageInputRef.current?.click()}
                  disabled={streaming}
                  title="上传图片（也支持直接粘贴截图或拖拽）"
                  className="flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs text-slate-500 transition hover:bg-slate-100 hover:text-slate-800 disabled:opacity-40 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200"
                >
                  <ImageIcon className="h-4 w-4" />
                  <span className="hidden sm:inline">图片</span>
                </button>

                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={streaming}
                  title="上传文件（日志、文档、代码等，最大 20MB）"
                  className="flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs text-slate-500 transition hover:bg-slate-100 hover:text-slate-800 disabled:opacity-40 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200"
                >
                  <Paperclip className="h-4 w-4" />
                  <span className="hidden sm:inline">文件</span>
                </button>

                <button
                  type="button"
                  onClick={() => setSlashManualOpen((v) => !v)}
                  disabled={streaming}
                  title="快捷选择 Antigravity CLI 命令 (/)"
                  className={`flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs transition disabled:opacity-40 ${
                    slashOpen
                      ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-500/15 dark:text-indigo-300'
                      : 'text-slate-500 hover:bg-slate-100 hover:text-slate-800 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200'
                  }`}
                >
                  <Slash className="h-3.5 w-3.5" />
                  <span>命令</span>
                </button>
              </div>

              {streaming ? (
                <button
                  onClick={handleStop}
                  className="flex h-9 shrink-0 items-center gap-1.5 rounded-xl bg-rose-600 px-3 text-xs font-medium text-white transition hover:bg-rose-500 sm:px-3.5"
                >
                  <Square className="h-3.5 w-3.5 fill-current" />
                  <span>停止</span>
                </button>
              ) : (
                <button
                  onClick={() => handleSend()}
                  disabled={(!input.trim() && pendingAttachments.length === 0) || uploadingCount > 0}
                  className="flex h-9 shrink-0 items-center gap-1.5 rounded-xl bg-indigo-600 px-3.5 text-xs font-medium text-white transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-40 sm:px-4"
                >
                  <Send className="h-3.5 w-3.5" />
                  <span>发送</span>
                </button>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Directory picker modal */}
      <DirPickerModal
        open={pickerMode !== null}
        title={pickerMode === 'workspace' ? '选择主工作区目录 (cwd)' : '添加附加上下文目录 (--add-dir)'}
        initialPath={workspace}
        onClose={() => setPickerMode(null)}
        onSelect={(chosen) => {
          if (pickerMode === 'workspace') {
            updateMeta({ workspace: chosen });
          } else if (pickerMode === 'extra' && !extraDirs.includes(chosen) && chosen !== workspace) {
            updateMeta({ extra_dirs: [...extraDirs, chosen] });
          }
          setPickerMode(null);
        }}
      />
    </div>
  );
};
