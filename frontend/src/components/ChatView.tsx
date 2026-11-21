import React, { useEffect, useRef, useState } from 'react';
import ReactMarkdown from 'react-markdown';
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
} from 'lucide-react';
import { api, streamSessionChat } from '../api/client';
import type {
  AgyStatus,
  AppSettings,
  ChatMessage,
  ChatSession,
  ToolCall,
} from '../types';
import { DirPickerModal } from './DirPickerModal';

interface ChatViewProps {
  session: ChatSession | null;
  settings: AppSettings | null;
  agyStatus: AgyStatus | null;
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
    <div className="mb-3 overflow-hidden rounded-xl border border-indigo-500/20 bg-indigo-950/20">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs font-medium text-indigo-300 hover:bg-indigo-500/10"
      >
        <Brain className="h-3.5 w-3.5" />
        <span>思考过程</span>
        <span className="ml-auto text-slate-400">
          {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
        </span>
      </button>
      {open && (
        <div className="border-t border-indigo-500/15 px-3 py-2.5 font-mono text-xs whitespace-pre-wrap text-slate-300/90">
          {thinking}
        </div>
      )}
    </div>
  );
};

const ToolCallCard: React.FC<{ tool: ToolCall }> = ({ tool }) => {
  const [open, setOpen] = useState(false);
  return (
    <div className="overflow-hidden rounded-xl border border-slate-700/80 bg-slate-950/70 text-xs">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-slate-800/60"
      >
        {tool.status === 'running' ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin text-amber-400" />
        ) : tool.status === 'error' ? (
          <AlertTriangle className="h-3.5 w-3.5 text-rose-400" />
        ) : (
          <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
        )}
        <Wrench className="h-3.5 w-3.5 text-slate-400" />
        <span className="font-mono font-medium text-slate-200">{tool.name || 'Tool'}</span>
        <span className="ml-auto text-[10px] uppercase text-slate-500">{tool.status}</span>
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 text-slate-500" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 text-slate-500" />
        )}
      </button>
      {open && (
        <div className="space-y-2 border-t border-slate-800 p-3 font-mono text-[11px]">
          {tool.input && (
            <div>
              <div className="mb-1 text-slate-500">输入参数</div>
              <pre className="max-h-48 overflow-auto rounded-lg bg-slate-900 p-2 text-slate-300">
                {tool.input}
              </pre>
            </div>
          )}
          {tool.output && (
            <div>
              <div className="mb-1 text-slate-500">执行输出</div>
              <pre className="max-h-56 overflow-auto rounded-lg bg-slate-900 p-2 text-slate-300">
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

  const abortRef = useRef<AbortController | null>(null);
  const bottomRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [session?.messages.length, draftMsg?.content, draftMsg?.thinking, draftMsg?.tool_calls?.length]);

  const updateMeta = async (patch: {
    workspace?: string;
    extra_dirs?: string[];
    model?: string;
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

  const handleSend = async (overridePrompt?: string) => {
    const prompt = (overridePrompt ?? input).trim();
    if (!prompt || streaming) return;

    let active = session;
    if (!active) {
      active = await onCreateSession();
      if (!active) return;
    }

    setInput('');
    setStreaming(true);

    // Optimistically show user message
    const optimisticUser: ChatMessage = {
      id: `local-${Date.now()}`,
      role: 'user',
      content: prompt,
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
          permission_mode: active.permission_mode,
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
      try {
        const fresh = await api.getSession(active.id);
        onSessionUpdated(fresh);
      } catch {
        // ignore
      }
    }
  };

  const handleStop = () => {
    abortRef.current?.abort();
  };

  const workspace = session?.workspace || settings?.default_workspace || '/vol1';
  const extraDirs = session?.extra_dirs || [];
  const model = session?.model ?? settings?.default_model ?? '';
  const permMode = session?.permission_mode || settings?.default_permission_mode || 'default';

  const messages = session?.messages || [];

  return (
    <div className="flex h-full flex-col bg-slate-950">
      {/* Top workspace & runtime toolbar */}
      <header className="flex flex-wrap items-center gap-2 border-b border-slate-800/90 bg-slate-900/70 px-4 py-2.5 backdrop-blur">
        <button
          onClick={() => setPickerMode('workspace')}
          className="flex max-w-xs items-center gap-1.5 rounded-lg border border-slate-700/80 bg-slate-950 px-2.5 py-1.5 text-xs text-slate-200 transition hover:border-indigo-500/60"
          title="切换工作区目录 (cwd)"
        >
          <FolderOpen className="h-3.5 w-3.5 shrink-0 text-amber-400" />
          <span className="truncate font-mono">{workspace}</span>
        </button>

        {extraDirs.map((dir) => (
          <span
            key={dir}
            className="flex items-center gap-1 rounded-lg border border-slate-800 bg-slate-900 px-2 py-1 font-mono text-[11px] text-slate-300"
          >
            <span className="max-w-[140px] truncate">+ {dir}</span>
            <button
              onClick={() =>
                updateMeta({ extra_dirs: extraDirs.filter((d) => d !== dir) })
              }
              className="text-slate-500 hover:text-rose-400"
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}

        <button
          onClick={() => setPickerMode('extra')}
          className="flex items-center gap-1 rounded-lg border border-dashed border-slate-700 px-2 py-1.5 text-[11px] text-slate-400 transition hover:border-slate-500 hover:text-slate-200"
          title="添加附加上下文目录 (--add-dir)"
        >
          <Plus className="h-3 w-3" />
          <span>附加目录</span>
        </button>

        <div className="ml-auto flex flex-wrap items-center gap-2">
          {/* Model */}
          <div className="flex items-center gap-1 rounded-lg border border-slate-800 bg-slate-950 px-2 py-1 text-xs">
            <Cpu className="h-3.5 w-3.5 text-slate-400" />
            <input
              type="text"
              value={model}
              onChange={(e) => updateMeta({ model: e.target.value })}
              placeholder="默认模型"
              className="w-28 bg-transparent text-xs text-slate-200 placeholder-slate-500 focus:outline-none"
            />
          </div>

          {/* Permission Mode */}
          <div className="flex items-center gap-1 rounded-lg border border-slate-800 bg-slate-950 px-2 py-1 text-xs">
            <Shield className="h-3.5 w-3.5 text-slate-400" />
            <select
              value={permMode}
              onChange={(e) => updateMeta({ permission_mode: e.target.value })}
              className="bg-transparent text-xs text-slate-200 focus:outline-none"
            >
              <option value="default" className="bg-slate-900">
                标准模式 (default)
              </option>
              <option value="accept-edits" className="bg-slate-900">
                自动接受编辑 (accept-edits)
              </option>
              <option value="plan" className="bg-slate-900">
                仅规划 (plan)
              </option>
              {settings?.allow_dangerous_skip && (
                <option value="bypass" className="bg-slate-900">
                  跳过权限确认 (危险)
                </option>
              )}
            </select>
          </div>

          {/* Handoff to Terminal */}
          <button
            onClick={() => onOpenInTerminal(workspace, session?.conversation_id)}
            className="flex items-center gap-1.5 rounded-lg border border-indigo-500/30 bg-indigo-500/10 px-2.5 py-1.5 text-xs font-medium text-indigo-300 transition hover:bg-indigo-500/20"
            title="在内置终端中继续当前工作区或会话"
          >
            <Terminal className="h-3.5 w-3.5" />
            <span>终端继续</span>
          </button>
        </div>
      </header>

      {/* Setup warning banner if agy missing or unauthenticated */}
      {agyStatus && (!agyStatus.installed || !agyStatus.authenticated) && (
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-amber-500/20 bg-amber-500/10 px-4 py-2 text-xs text-amber-200">
          <div className="flex items-center gap-2">
            <AlertTriangle className="h-4 w-4 shrink-0 text-amber-400" />
            {!agyStatus.installed ? (
              <span>尚未检测到 Antigravity CLI (`agy`)，请先前往设置中心一键安装。</span>
            ) : (
              <span>尚未检测到登录凭据，请先在「内置 Web 终端」中运行一次 `agy` 完成 OAuth 登录。</span>
            )}
          </div>
          {!agyStatus.installed ? (
            <button
              onClick={onGoToSettings}
              className="rounded-lg bg-amber-500/20 px-2.5 py-1 font-medium text-amber-100 hover:bg-amber-500/30"
            >
              前往安装
            </button>
          ) : (
            <button
              onClick={onGoToTerminal}
              className="rounded-lg bg-amber-500/20 px-2.5 py-1 font-medium text-amber-100 hover:bg-amber-500/30"
            >
              打开终端登录
            </button>
          )}
        </div>
      )}

      {/* Messages area */}
      <div className="flex-1 overflow-y-auto px-4 py-6">
        <div className="mx-auto max-w-4xl space-y-5">
          {messages.length === 0 && !draftMsg ? (
            <div className="flex flex-col items-center justify-center py-16 text-center">
              <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-indigo-500/20 to-violet-600/20 ring-1 ring-indigo-500/30">
                <Sparkles className="h-7 w-7 text-indigo-400" />
              </div>
              <h2 className="text-lg font-semibold text-white">开始与 Antigravity 对话</h2>
              <p className="mt-1.5 max-w-md text-xs leading-relaxed text-slate-400">
                当前工作区：<span className="font-mono text-slate-300">{workspace}</span>
                <br />
                支持直接分析 NAS 共享目录中的代码与文档，也可随时一键切至内置终端使用完整 TUI。
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
                    className="rounded-xl border border-slate-800 bg-slate-900/70 p-3 text-left text-xs text-slate-300 transition hover:border-indigo-500/40 hover:bg-slate-900 hover:text-white"
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
                  <div className="max-w-[82%] rounded-2xl rounded-tr-sm bg-indigo-600 px-4 py-2.5 text-sm whitespace-pre-wrap text-white shadow">
                    {msg.content}
                  </div>
                ) : (
                  <div className="w-full max-w-[92%] rounded-2xl border border-slate-800/90 bg-slate-900/80 p-4 text-sm text-slate-100 shadow-sm">
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
                      <div className="chat-markdown">
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
                      </div>
                    ) : msg.id.startsWith('stream-') && !msg.error ? (
                      <div className="flex items-center gap-2 text-xs text-slate-400">
                        <Loader2 className="h-3.5 w-3.5 animate-spin text-indigo-400" />
                        <span>Antigravity 正在思考与执行...</span>
                      </div>
                    ) : null}

                    {msg.error && (
                      <div className="mt-3 rounded-xl border border-rose-500/30 bg-rose-500/10 px-3 py-2 font-mono text-xs whitespace-pre-wrap text-rose-300">
                        {msg.error}
                      </div>
                    )}

                    {msg.usage && (
                      <div className="mt-3 flex items-center gap-3 border-t border-slate-800/80 pt-2 text-[11px] text-slate-500">
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
          <div ref={bottomRef} />
        </div>
      </div>

      {/* Input composer */}
      <div className="border-t border-slate-800/90 bg-slate-900/70 p-4">
        <div className="mx-auto flex max-w-4xl items-end gap-2 rounded-2xl border border-slate-700/80 bg-slate-950 p-2 focus-within:border-indigo-500/70">
          <textarea
            rows={1}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            placeholder="输入指令或问题（Enter 发送，Shift+Enter 换行）..."
            className="max-h-40 min-h-[40px] flex-1 resize-none bg-transparent px-2.5 py-2 text-sm text-white placeholder-slate-500 focus:outline-none"
          />
          {streaming ? (
            <button
              onClick={handleStop}
              className="flex h-10 items-center gap-1.5 rounded-xl bg-rose-600 px-3.5 text-xs font-medium text-white transition hover:bg-rose-500"
            >
              <Square className="h-3.5 w-3.5 fill-current" />
              <span>停止</span>
            </button>
          ) : (
            <button
              onClick={() => handleSend()}
              disabled={!input.trim()}
              className="flex h-10 items-center gap-1.5 rounded-xl bg-indigo-600 px-4 text-xs font-medium text-white transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-40"
            >
              <Send className="h-3.5 w-3.5" />
              <span>发送</span>
            </button>
          )}
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
