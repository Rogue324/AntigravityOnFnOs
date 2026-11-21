export type ActiveTab = 'chat' | 'terminal' | 'settings';

export type PermissionMode = 'default' | 'accept-edits' | 'plan' | 'bypass';

export interface AuthStatus {
  initialized: boolean;
  authenticated: boolean;
}

export interface AppSettings {
  http_proxy: string;
  https_proxy: string;
  no_proxy: string;
  default_model: string;
  default_permission_mode: PermissionMode;
  allow_dangerous_skip: boolean;
  default_workspace: string;
  agy_binary_path: string;
  data_dir: string;
  home_dir: string;
  bin_dir: string;
}

export interface AgyStatus {
  installed: boolean;
  binary_path: string;
  version: string;
  authenticated: boolean;
  home_dir: string;
  proxy_active: boolean;
}

export interface ProxyTestResult {
  ok: boolean;
  status_code?: number;
  latency_ms: number;
  error?: string;
  target_url: string;
}

export interface RootItem {
  label: string;
  path: string;
  kind: 'share' | 'volume' | 'home';
}

export interface DirEntry {
  name: string;
  path: string;
  is_dir: boolean;
}

export interface BrowseResponse {
  path: string;
  parent: string;
  entries: DirEntry[];
  roots: RootItem[];
}

export interface ToolCall {
  id: string;
  name: string;
  input?: string;
  output?: string;
  status: 'running' | 'done' | 'error';
}

export interface TokenUsage {
  input_tokens?: number;
  output_tokens?: number;
  total_tokens?: number;
}

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  thinking?: string;
  tool_calls?: ToolCall[];
  usage?: TokenUsage;
  error?: string;
  created_at: string;
}

export interface SessionSummary {
  id: string;
  title: string;
  conversation_id?: string;
  workspace: string;
  extra_dirs?: string[];
  model?: string;
  permission_mode?: string;
  message_count: number;
  created_at: string;
  updated_at: string;
}

export interface ChatSession {
  id: string;
  title: string;
  conversation_id?: string;
  workspace: string;
  extra_dirs?: string[];
  model?: string;
  permission_mode?: string;
  messages: ChatMessage[];
  created_at: string;
  updated_at: string;
}

export interface ChatSSEEvent {
  type: 'meta' | 'thinking' | 'text' | 'tool_call' | 'tool_result' | 'usage' | 'error' | 'done';
  conversation_id?: string;
  delta?: string;
  tool_call?: ToolCall;
  usage?: TokenUsage;
  error?: string;
}

export interface TerminalLaunchRequest {
  mode: 'agy' | 'shell';
  workspace?: string;
  conversationId?: string;
  nonce: number;
}
