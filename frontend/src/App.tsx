import React, { useCallback, useEffect, useState } from 'react';
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
  SessionSummary,
  TerminalLaunchRequest,
} from './types';

export default function App() {
  const [auth, setAuth] = useState<AuthStatus | null>(null);
  const [activeTab, setActiveTab] = useState<ActiveTab>('chat');

  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [agyStatus, setAgyStatus] = useState<AgyStatus | null>(null);

  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [activeSession, setActiveSession] = useState<ChatSession | null>(null);

  const [termLaunch, setTermLaunch] = useState<TerminalLaunchRequest | null>(null);

  const loadAuth = useCallback(async () => {
    try {
      const st = await api.authStatus();
      setAuth(st);
    } catch {
      setAuth({ initialized: false, authenticated: false });
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

      if (sessList.sessions.length > 0) {
        const first = await api.getSession(sessList.sessions[0].id);
        setActiveSession(first);
      } else {
        setActiveSession(null);
      }
    } catch {
      // If session expired, refresh auth status
      loadAuth();
    }
  }, [loadAuth]);

  useEffect(() => {
    loadAuth();
  }, [loadAuth]);

  useEffect(() => {
    if (auth?.authenticated) {
      loadAppData();
    }
  }, [auth?.authenticated, loadAppData]);

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
        permission_mode: settings?.default_permission_mode,
      });
      setActiveSession(created);
      await refreshSessionsList();
      setActiveTab('chat');
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
  };

  const handleLogout = async () => {
    try {
      await api.logout();
    } finally {
      setAuth({ initialized: true, authenticated: false });
      setActiveSession(null);
      setSessions([]);
    }
  };

  if (!auth) {
    return (
      <div className="flex h-full items-center justify-center bg-slate-950 text-xs text-slate-400">
        正在加载 Antigravity for fnOS...
      </div>
    );
  }

  if (!auth.authenticated) {
    return (
      <AuthGate
        initialized={auth.initialized}
        onAuthenticated={() => setAuth({ initialized: true, authenticated: true })}
      />
    );
  }

  return (
    <div className="flex h-full w-full overflow-hidden bg-slate-950 text-slate-100">
      <Sidebar
        activeTab={activeTab}
        onSelectTab={setActiveTab}
        sessions={sessions}
        activeSessionId={activeSession?.id || null}
        onSelectSession={handleSelectSession}
        onNewSession={handleNewSession}
        onDeleteSession={handleDeleteSession}
        agyStatus={agyStatus}
        onLogout={handleLogout}
      />

      <main className="min-w-0 flex-1 overflow-hidden">
        {activeTab === 'chat' && (
          <ChatView
            session={activeSession}
            settings={settings}
            agyStatus={agyStatus}
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
            launchRequest={termLaunch}
            onStatusRefresh={async () => {
              try {
                const st = await api.agyStatus();
                setAgyStatus(st);
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
            onSettingsSaved={setSettings}
            onAgyStatusUpdated={setAgyStatus}
            onGoToTerminal={() => setActiveTab('terminal')}
          />
        )}
      </main>
    </div>
  );
}
