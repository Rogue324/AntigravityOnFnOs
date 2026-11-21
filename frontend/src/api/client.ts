import type {
  AgyStatus,
  AppSettings,
  AuthStatus,
  BrowseResponse,
  ChatSSEEvent,
  ChatSession,
  ModelOption,
  ProxyTestResult,
  SessionSummary,
} from '../types';

const GATEWAY_PREFIX = '/app/antigravity-cli';

export function getBasePrefix(): string {
  const p = window.location.pathname;
  if (p === GATEWAY_PREFIX || p.startsWith(`${GATEWAY_PREFIX}/`)) {
    return GATEWAY_PREFIX;
  }
  return '';
}

export function apiUrl(path: string): string {
  return `${getBasePrefix()}${path}`;
}

export function wsUrl(path: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${window.location.host}${apiUrl(path)}`;
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiUrl(url), {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  });

  if (!res.ok) {
    let msg = `请求失败 (${res.status})`;
    try {
      const errBody = await res.json();
      if (errBody?.error) msg = errBody.error;
    } catch {
      // ignore
    }
    throw new Error(msg);
  }
  return res.json() as Promise<T>;
}

export const api = {
  authStatus: () => request<AuthStatus>('/api/auth/status'),
  setupPassword: (password: string) =>
    request<{ ok: boolean; token: string }>('/api/auth/setup', {
      method: 'POST',
      body: JSON.stringify({ password }),
    }),
  login: (password: string) =>
    request<{ ok: boolean; token: string }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ password }),
    }),
  logout: () => request<{ ok: boolean }>('/api/auth/logout', { method: 'POST' }),
  changePassword: (current_password: string, new_password: string) =>
    request<{ ok: boolean }>('/api/auth/change-password', {
      method: 'POST',
      body: JSON.stringify({ current_password, new_password }),
    }),
  createWSTicket: () => request<{ ticket: string }>('/api/auth/ws-ticket', { method: 'POST' }),

  getSettings: () => request<AppSettings>('/api/settings'),
  updateSettings: (patch: Partial<AppSettings>) =>
    request<AppSettings>('/api/settings', {
      method: 'PUT',
      body: JSON.stringify(patch),
    }),
  testProxy: (http_proxy: string, https_proxy: string) =>
    request<ProxyTestResult>('/api/settings/test-proxy', {
      method: 'POST',
      body: JSON.stringify({ http_proxy, https_proxy }),
    }),

  agyStatus: () => request<AgyStatus>('/api/agy/status'),
  agyModels: () => request<{ models: ModelOption[]; error?: string }>('/api/agy/models'),

  browseFS: (path = '', hidden = false) =>
    request<BrowseResponse>(
      `/api/fs/browse?path=${encodeURIComponent(path)}&hidden=${hidden ? 'true' : 'false'}`
    ),

  listSessions: () => request<{ sessions: SessionSummary[] }>('/api/sessions'),
  createSession: (payload: {
    title?: string;
    workspace?: string;
    extra_dirs?: string[];
    model?: string;
    effort?: string;
    permission_mode?: string;
  }) =>
    request<ChatSession>('/api/sessions', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  getSession: (id: string) => request<ChatSession>(`/api/sessions/${encodeURIComponent(id)}`),
  updateSession: (
    id: string,
    patch: {
      title?: string;
      workspace?: string;
      extra_dirs?: string[];
      model?: string;
      effort?: string;
      permission_mode?: string;
    }
  ) =>
    request<ChatSession>(`/api/sessions/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),
  deleteSession: (id: string) =>
    request<{ ok: boolean }>(`/api/sessions/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
};

async function readSSEStream(
  res: Response,
  onEvent: (eventName: string, data: string) => void
): Promise<void> {
  if (!res.ok) {
    let msg = `流式请求失败 (${res.status})`;
    try {
      const errBody = await res.json();
      if (errBody?.error) msg = errBody.error;
    } catch {
      // ignore
    }
    throw new Error(msg);
  }
  const reader = res.body?.getReader();
  if (!reader) throw new Error('浏览器不支持流式响应');

  const decoder = new TextDecoder();
  let buffer = '';

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let sepIdx: number;
    while ((sepIdx = buffer.indexOf('\n\n')) !== -1) {
      const block = buffer.slice(0, sepIdx);
      buffer = buffer.slice(sepIdx + 2);

      let eventName = 'message';
      const dataLines: string[] = [];
      for (const rawLine of block.split('\n')) {
        const line = rawLine.trimEnd();
        if (line.startsWith('event:')) {
          eventName = line.slice(6).trim();
        } else if (line.startsWith('data:')) {
          dataLines.push(line.slice(5).trimStart());
        }
      }
      if (dataLines.length > 0) {
        onEvent(eventName, dataLines.join('\n'));
      }
    }
  }
}

export async function streamSessionChat(
  sessionId: string,
  payload: {
    prompt: string;
    workspace?: string;
    extra_dirs?: string[];
    model?: string;
    effort?: string;
    permission_mode?: string;
  },
  onEvent: (ev: ChatSSEEvent) => void,
  signal?: AbortSignal
): Promise<void> {
  const res = await fetch(apiUrl(`/api/sessions/${encodeURIComponent(sessionId)}/chat`), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
    signal,
  });

  await readSSEStream(res, (_eventName, data) => {
    try {
      const parsed = JSON.parse(data) as ChatSSEEvent;
      onEvent(parsed);
    } catch {
      // ignore malformed frame
    }
  });
}

export async function streamAgyInstall(
  onLog: (line: string) => void,
  onDone: (result: { ok: boolean; error?: string; status?: AgyStatus }) => void,
  signal?: AbortSignal
): Promise<void> {
  const res = await fetch(apiUrl('/api/agy/install'), {
    method: 'POST',
    credentials: 'include',
    signal,
  });

  await readSSEStream(res, (eventName, data) => {
    try {
      const parsed = JSON.parse(data);
      if (eventName === 'log' && typeof parsed.line === 'string') {
        onLog(parsed.line);
      } else if (eventName === 'done') {
        onDone(parsed);
      }
    } catch {
      // ignore
    }
  });
}
