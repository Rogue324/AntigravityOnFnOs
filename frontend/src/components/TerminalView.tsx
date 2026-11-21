import React, { useEffect, useRef, useState } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import {
  RefreshCw,
  Trash2,
  FolderOpen,
  Terminal,
  Sparkles,
  Info,
} from 'lucide-react';
import { api, wsUrl } from '../api/client';
import type { AppSettings, TerminalLaunchRequest, ThemeMode } from '../types';
import { DirPickerModal } from './DirPickerModal';

interface TerminalViewProps {
  settings: AppSettings | null;
  theme: ThemeMode;
  launchRequest: TerminalLaunchRequest | null;
  onStatusRefresh: () => void;
}

const LIGHT_XTERM_THEME = {
  background: '#f8fafc',
  foreground: '#0f172a',
  cursor: '#4f46e5',
  selectionBackground: 'rgba(99, 102, 241, 0.25)',
};

const DARK_XTERM_THEME = {
  background: '#090d16',
  foreground: '#e2e8f0',
  cursor: '#818cf8',
  selectionBackground: 'rgba(99, 102, 241, 0.35)',
};

export const TerminalView: React.FC<TerminalViewProps> = ({
  settings,
  theme,
  launchRequest,
  onStatusRefresh,
}) => {
  const [mode, setMode] = useState<'agy' | 'shell'>(launchRequest?.mode || 'agy');
  const [workspace, setWorkspace] = useState(
    launchRequest?.workspace || settings?.default_workspace || '/vol1'
  );
  const [conversationId, setConversationId] = useState(launchRequest?.conversationId || '');
  const [connState, setConnState] = useState<'connecting' | 'connected' | 'disconnected'>(
    'connecting'
  );
  const [pickerOpen, setPickerOpen] = useState(false);
  const [reconnectNonce, setReconnectNonce] = useState(0);

  const containerRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<XTerm | null>(null);
  const wsRef = useRef<WebSocket | null>(null);

  // Dynamically update xterm palette when user toggles theme without reconnecting PTY
  useEffect(() => {
    if (termRef.current) {
      termRef.current.options.theme = theme === 'dark' ? DARK_XTERM_THEME : LIGHT_XTERM_THEME;
    }
  }, [theme]);

  // Sync external handoff request (e.g., "Open in Terminal" from ChatView)
  useEffect(() => {
    if (!launchRequest) return;
    setMode(launchRequest.mode);
    if (launchRequest.workspace) setWorkspace(launchRequest.workspace);
    setConversationId(launchRequest.conversationId || '');
    setReconnectNonce((n) => n + 1);
  }, [launchRequest?.nonce]);

  useEffect(() => {
    if (!containerRef.current) return;

    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13.5,
      fontFamily:
        'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      theme: theme === 'dark' ? DARK_XTERM_THEME : LIGHT_XTERM_THEME,
      scrollback: 5000,
    });

    const fitAddon = new FitAddon();
    const linksAddon = new WebLinksAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(linksAddon);
    term.open(containerRef.current);
    fitAddon.fit();

    termRef.current = term;

    let disposed = false;

    const connect = async () => {
      setConnState('connecting');
      term.writeln('\x1b[90m正在建立终端连接...\x1b[0m');

      try {
        const { ticket } = await api.createWSTicket();
        if (disposed) return;

        const params = new URLSearchParams({
          ticket,
          mode,
          workspace,
          cols: String(term.cols || 120),
          rows: String(term.rows || 34),
        });
        if (conversationId) {
          params.set('conversation', conversationId);
        }

        const ws = new WebSocket(wsUrl(`/api/terminal/ws?${params}`));
        ws.binaryType = 'arraybuffer';
        wsRef.current = ws;

        ws.onopen = () => {
          if (disposed) return;
          setConnState('connected');
          term.clear();
          fitAddon.fit();
          ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
        };

        ws.onmessage = (ev) => {
          if (ev.data instanceof ArrayBuffer) {
            term.write(new Uint8Array(ev.data));
          } else if (typeof ev.data === 'string') {
            term.write(ev.data);
          }
        };

        ws.onclose = () => {
          if (disposed) return;
          setConnState('disconnected');
          term.writeln('\r\n\x1b[33m[终端连接已关闭，点击右上角「重连」可重新启动]\x1b[0m');
          onStatusRefresh();
        };

        ws.onerror = () => {
          if (disposed) return;
          setConnState('disconnected');
        };
      } catch (err) {
        if (disposed) return;
        setConnState('disconnected');
        term.writeln(
          `\r\n\x1b[31m[连接失败: ${err instanceof Error ? err.message : String(err)}]\x1b[0m`
        );
      }
    };

    const dataDisp = term.onData((data) => {
      const ws = wsRef.current;
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'input', data }));
      }
    });

    const resizeObserver = new ResizeObserver(() => {
      try {
        fitAddon.fit();
        const ws = wsRef.current;
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
        }
      } catch {
        // ignore resize race
      }
    });
    resizeObserver.observe(containerRef.current);

    connect();

    return () => {
      disposed = true;
      resizeObserver.disconnect();
      dataDisp.dispose();
      wsRef.current?.close();
      wsRef.current = null;
      term.dispose();
      termRef.current = null;
    };
  }, [mode, workspace, conversationId, reconnectNonce]);

  return (
    <div className="flex h-full flex-col bg-slate-50 dark:bg-[#090d16]">
      {/* Toolbar */}
      <header className="flex flex-wrap items-center gap-2 border-b border-slate-200 bg-white px-4 py-2.5 dark:border-slate-800 dark:bg-slate-900/90">
        <div className="flex items-center rounded-lg border border-slate-200 bg-slate-100 p-0.5 text-xs dark:border-slate-800 dark:bg-slate-950">
          <button
            onClick={() => setMode('agy')}
            className={`flex items-center gap-1.5 rounded-md px-2.5 py-1 font-medium transition ${
              mode === 'agy'
                ? 'bg-indigo-600 text-white'
                : 'text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-200'
            }`}
          >
            <Sparkles className="h-3.5 w-3.5" />
            <span>Antigravity TUI</span>
          </button>
          <button
            onClick={() => {
              setConversationId('');
              setMode('shell');
            }}
            className={`flex items-center gap-1.5 rounded-md px-2.5 py-1 font-medium transition ${
              mode === 'shell'
                ? 'bg-indigo-600 text-white'
                : 'text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-200'
            }`}
          >
            <Terminal className="h-3.5 w-3.5" />
            <span>系统 Shell</span>
          </button>
        </div>

        <button
          onClick={() => setPickerOpen(true)}
          className="flex max-w-xs items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1.5 text-xs text-slate-700 hover:border-indigo-500/60 dark:border-slate-700/80 dark:bg-slate-950 dark:text-slate-200"
        >
          <FolderOpen className="h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
          <span className="truncate font-mono">{workspace}</span>
        </button>

        {conversationId && (
          <span className="rounded-lg border border-indigo-200 bg-indigo-50 px-2 py-1 font-mono text-[11px] text-indigo-700 dark:border-indigo-500/30 dark:bg-indigo-500/10 dark:text-indigo-300">
            会话: {conversationId.slice(0, 10)}…
          </span>
        )}

        <div className="ml-auto flex items-center gap-2">
          <span
            className={`flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px] font-medium ${
              connState === 'connected'
                ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
                : connState === 'connecting'
                ? 'bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
                : 'bg-rose-50 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300'
            }`}
          >
            <span
              className={`h-1.5 w-1.5 rounded-full ${
                connState === 'connected'
                  ? 'bg-emerald-500 dark:bg-emerald-400'
                  : connState === 'connecting'
                  ? 'animate-pulse bg-amber-500 dark:bg-amber-400'
                  : 'bg-rose-500 dark:bg-rose-400'
              }`}
            />
            {connState === 'connected'
              ? '已连接'
              : connState === 'connecting'
              ? '连接中'
              : '已断开'}
          </span>

          <button
            onClick={() => termRef.current?.clear()}
            className="flex items-center gap-1 rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:border-slate-700 dark:bg-slate-800/80 dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white"
          >
            <Trash2 className="h-3.5 w-3.5" />
            <span>清屏</span>
          </button>

          <button
            onClick={() => setReconnectNonce((n) => n + 1)}
            className="flex items-center gap-1 rounded-lg bg-slate-100 px-2.5 py-1.5 text-xs text-slate-700 hover:bg-indigo-600 hover:text-white dark:bg-slate-800 dark:text-slate-200"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            <span>重连</span>
          </button>
        </div>
      </header>

      {/* Helpful tip for NAS headless OAuth */}
      <div className="flex items-center gap-2 border-b border-slate-200/80 bg-slate-100/70 px-4 py-1.5 text-[11px] text-slate-600 dark:border-slate-800/70 dark:bg-slate-900/40 dark:text-slate-400">
        <Info className="h-3.5 w-3.5 shrink-0 text-indigo-500 dark:text-indigo-400" />
        <span>
          首次登录或使用 <code className="text-slate-800 dark:text-slate-300">/mcp</code>、
          <code className="text-slate-800 dark:text-slate-300">/skills</code>、
          <code className="text-slate-800 dark:text-slate-300">/diff</code> 等 TUI 命令请直接在此交互；已自动注入设置中心的代理与隔离 HOME 目录。
        </span>
      </div>

      {/* XTerm mount */}
      <div ref={containerRef} className="min-h-0 flex-1 p-2" />

      <DirPickerModal
        open={pickerOpen}
        title="选择终端工作目录"
        initialPath={workspace}
        onClose={() => setPickerOpen(false)}
        onSelect={(chosen) => {
          setWorkspace(chosen);
          setPickerOpen(false);
        }}
      />
    </div>
  );
};
