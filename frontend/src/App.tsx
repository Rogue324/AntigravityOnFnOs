import React, { useCallback, useEffect, useState } from 'react';
import { Menu, Plus, Sun, Moon } from 'lucide-react';
import { api } from './api/client';
import { AuthGate } from './components/AuthGate';
import { Sidebar } from './components/Sidebar';
import { ChatView } from './components/ChatView';
import { TerminalView } from './components/TerminalView';
import { SettingsView } from './components/SettingsView';
import type {
  ActiveTab,
  AgyStatus,
  AppSettings,
  AuthStatus,
  ChatSession,
  ModelOption,
  SessionSummary,
  TerminalLaunchRequest,
  ThemeMode,
} from './types';

function readInitialTheme(): ThemeMode {
  try {
    const saved = window.localStorage.getItem('agy_theme');
    if (saved === 'dark' || saved === 'light') return saved;
  } catch {
    // ignore
  }
  return 'light';
}

export default function App() {
  const [theme, setTheme] = useState<ThemeMode>(readInitialTheme);
  const [auth, setAuth] = useState<AuthStatus | null>(null);
  const [activeTab, setActiveTab] = useState<ActiveTab>('chat');
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [desktopCollapsed, setDesktopCollapsed] = useState(false);

  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [agyStatus, setAgyStatus] = useState<AgyStatus | null>(null);
  const [models, setModels] = useState<ModelOption[]>([]);

  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [activeSession, setActiveSession] = useState<ChatSession | null>(null);

  const [termLaunch, setTermLaunch] = useState<TerminalLaunchRequest | null>(null);

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark');
    try {
      window.localStorage.setItem('agy_theme', theme);
    } catch {
      // ignore
    }
  }, [theme]);

  const toggleTheme = useCallback(() => {
    setTheme((prev) => (prev === 'light' ? 'dark' : 'light'));
  }, []);

  const loadAuth = useCallback(async () => {
    try {
      const st = await api.authStatus();
      setAuth(st);
    } catch {
      setAuth({ initialized: false, authenticated: false });
    }
  }, []);

  const refreshModels = useCallback(async () => {
    try {
      const res = await api.agyModels();
      if (Array.isArray(res.models)) {
        setModels(res.models);
      }
    } catch {
      // ignore
    }
  }, []);

  const loadAppData = useCallback(async () => {
    try {
      const [cfg, agySt, sessList] = await Promise.all([
        api.getSettings(),
        api.agyStatus(),
        api.listSessions(),
      ]);
      setSettings(cfg);
      setAgyStatus(agySt);
      setSessions(sessList.sessions);
      if (agySt.installed) {
        refreshModels();
      }

      if (sessList.sessions.length > 0) {
        const first = await api.getSession(sessList.sessions[0].id);
        setActiveSession(first);
      } else {
        setActiveSession(null);
      }
    } catch {
      loadAuth();
    }
  }, [loadAuth, refreshModels]);

  useEffect(() => {
    loadAuth();
  }, [loadAuth]);

  useEffect(() => {
    if (auth?.authenticated) {
      loadAppData();
    }
  }, [auth?.authenticated, loadAppData]);

  useEffect(() => {
    if (!auth?.authenticated) return;
    api
      .agyStatus()
      .then((st) => {
        setAgyStatus(st);
        if (st.installed && models.length === 0) {
          refreshModels();
        }
      })
      .catch(() => {});
  }, [auth?.authenticated, activeTab, models.length, refreshModels]);

  const refreshSessionsList = useCallback(async () => {
    try {
      const { sessions: list } = await api.listSessions();
      setSessions(list);
    } catch {
      // ignore
    }
  }, []);

  const handleSelectSession = async (id: string) => {
    try {
      const sess = await api.getSession(id);
      setActiveSession(sess);
    } catch {
      // ignore
    }
  };

  const handleNewSession = async (): Promise<ChatSession | null> => {
    try {
      const created = await api.createSession({
        workspace: settings?.default_workspace,
        model: settings?.default_model,
        effort: settings?.default_effort,
        permission_mode: settings?.default_permission_mode,
      });
      setActiveSession(created);
      await refreshSessionsList();
      setActiveTab('chat');
      setMobileMenuOpen(false);
      return created;
    } catch {
      return null;
    }
  };

  const handleDeleteSession = async (id: string) => {
    try {
      await api.deleteSession(id);
      const { sessions: list } = await api.listSessions();
      setSessions(list);
      if (activeSession?.id === id) {
        if (list.length > 0) {
          const next = await api.getSession(list[0].id);
          setActiveSession(next);
        } else {
          setActiveSession(null);
        }
      }
    } catch {
      // ignore
    }
  };

  const handleSessionUpdated = (updated: ChatSession) => {
    setActiveSession(updated);
    refreshSessionsList();
  };

  const handleOpenInTerminal = (workspace: string, conversationId?: string) => {
    setTermLaunch({
      mode: 'agy',
      workspace,
      conversationId,
      nonce: Date.now(),
    });
    setActiveTab('terminal');
    setMobileMenuOpen(false);
  };

  const handleLogout = async () => {
    try {
      await api.logout();
    } finally {
      setAuth({ initialized: true, authenticated: false });
      setActiveSession(null);
      setSessions([]);
      setMobileMenuOpen(false);
    }
  };

  if (!auth) {
    return (
      <div className="flex h-full items-center justify-center bg-slate-50 text-xs text-slate-500 dark:bg-slate-950 dark:text-slate-400">
        正在加载 Antigravity for fnOS...
      </div>
    );
  }

  if (!auth.authenticated) {
    return (
      <AuthGate
        initialized={auth.initialized}
        theme={theme}
        onToggleTheme={toggleTheme}
        onAuthenticated={() => setAuth({ initialized: true, authenticated: true })}
      />
    );
  }

  const mobileTitle =
    activeTab === 'chat'
      ? activeSession?.title || 'Antigravity 对话'
      : activeTab === 'terminal'
      ? '内置 Web 终端'
      : '设置中心';

  return (
    <div className="flex h-full w-full flex-col overflow-hidden bg-slate-50 text-slate-900 lg:flex-row dark:bg-slate-950 dark:text-slate-100">
      {/* Mobile / compact-window top bar (< 1024px, or when desktop sidebar is collapsed) */}
      <div
        className={`flex shrink-0 items-center gap-2 border-b border-slate-200 bg-white px-3 py-2.5 dark:border-slate-800 dark:bg-slate-900 ${
          desktopCollapsed ? '' : 'lg:hidden'
        }`}
      >
        <button
          onClick={() => {
            if (window.innerWidth >= 1024) {
              setDesktopCollapsed(false);
            } else {
              setMobileMenuOpen(true);
            }
          }}
          aria-label="打开菜单"
          className="rounded-lg p-1.5 text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
        >
          <Menu className="h-5 w-5" />
        </button>

        <img src="./icon.png" alt="Antigravity" className="h-5 w-5 shrink-0 object-contain" />
        <span className="min-w-0 flex-1 truncate text-sm font-semibold text-slate-900 dark:text-white">
          {mobileTitle}
        </span>

        {activeTab === 'chat' && (
          <button
            onClick={handleNewSession}
            title="新建对话"
            className="rounded-lg p-1.5 text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
          >
            <Plus className="h-5 w-5" />
          </button>
        )}

        <button
          onClick={toggleTheme}
          title={theme === 'light' ? '深色模式' : '浅色模式'}
          className="rounded-lg p-1.5 text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
        >
          {theme === 'light' ? (
            <Moon className="h-4 w-4" />
          ) : (
            <Sun className="h-4 w-4 text-amber-400" />
          )}
        </button>
      </div>

      {/* Desktop persistent sidebar + Mobile slide-over drawer */}
      <Sidebar
        activeTab={activeTab}
        onSelectTab={(tab) => {
          setActiveTab(tab);
          setMobileMenuOpen(false);
        }}
        sessions={sessions}
        activeSessionId={activeSession?.id || null}
        onSelectSession={(id) => {
          handleSelectSession(id);
          setMobileMenuOpen(false);
        }}
        onNewSession={handleNewSession}
        onDeleteSession={handleDeleteSession}
        agyStatus={agyStatus}
        theme={theme}
        onToggleTheme={toggleTheme}
        onLogout={handleLogout}
        mobileOpen={mobileMenuOpen}
        onCloseMobile={() => setMobileMenuOpen(false)}
        desktopCollapsed={desktopCollapsed}
        onCollapseDesktop={() => setDesktopCollapsed(true)}
      />

      <main className="min-h-0 min-w-0 flex-1 overflow-hidden">
        {activeTab === 'chat' && (
          <ChatView
            session={activeSession}
            settings={settings}
            agyStatus={agyStatus}
            models={models}
            onRefreshModels={refreshModels}
            onSessionUpdated={handleSessionUpdated}
            onCreateSession={handleNewSession}
            onOpenInTerminal={handleOpenInTerminal}
            onGoToSettings={() => setActiveTab('settings')}
            onGoToTerminal={() => setActiveTab('terminal')}
          />
        )}

        {activeTab === 'terminal' && (
          <TerminalView
            settings={settings}
            theme={theme}
            launchRequest={termLaunch}
            onStatusRefresh={async () => {
              try {
                const st = await api.agyStatus();
                setAgyStatus(st);
                if (st.installed) refreshModels();
              } catch {
                // ignore
              }
            }}
          />
        )}

        {activeTab === 'settings' && (
          <SettingsView
            settings={settings}
            agyStatus={agyStatus}
            models={models}
            onSettingsSaved={setSettings}
            onAgyStatusUpdated={(st) => {
              setAgyStatus(st);
              if (st.installed) refreshModels();
            }}
            onGoToTerminal={() => setActiveTab('terminal')}
          />
        )}
      </main>
    </div>
  );
}
