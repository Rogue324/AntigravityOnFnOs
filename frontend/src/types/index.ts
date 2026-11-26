export type ActiveTab = 'chat' | 'terminal' | 'settings';
export type ThemeMode = 'light' | 'dark';

export type PermissionMode = 'default' | 'accept-edits' | 'plan' | 'bypass';

export interface AuthStatus {
  initialized: boolean;
  authenticated: boolean;
  auth_disabled?: boolean;
}

export interface ModelOption {
  id: string;
  label: string;
}

export interface AppSettings {
  http_proxy: string;
  https_proxy: string;
  no_proxy: string;
  default_model: string;
  default_effort: string;
  default_permission_mode: PermissionMode;
  allow_dangerous_skip: boolean;
  default_workspace: string;
  agy_binary_path: string;
  persistent_idle_minutes: number;
  auth_disabled: boolean;
  remote_control_enabled: boolean;
  password_set: boolean;
  data_dir: string;
  home_dir: string;
  bin_dir: string;
}

export interface RemoteControlStatus {
  enabled: boolean;
  running: boolean;
  pid?: number;
  state: 'stopped' | 'starting' | 'running' | 'restarting' | 'error';
  started_at?: string;
  restart_count: number;
  last_error?: string;
  logs: string[];
}

export interface QQBotPublicConfig {
  configured: boolean;
  enabled: boolean;
  app_id: string;
  app_id_masked: string;
  has_app_secret: boolean;
  owner_user_openid?: string;
  workspace?: string;
  model?: string;
  effort?: string;
  permission_mode?: string;
  allow_c2c: boolean;
  allow_group: boolean;
  restrict_to_owner: boolean;
  allowed_users: string[];
  allowed_groups: string[];
  created_at?: string;
  connected_at?: string;
}

export interface QQBotQRStatus {
  attempt_id: string;
  status: 'idle' | 'starting' | 'pending' | 'refreshing' | 'connecting' | 'connected' | 'failed' | 'cancelled';
  qr_revision: number;
  verification_url?: string;
  expires_at?: number;
  error?: string;
  poll_interval_ms: number;
}

export interface QQBotStatus {
  config: QQBotPublicConfig;
  connection_state: 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'error';
  connected: boolean;
  bot_username?: string;
  bot_user_id?: string;
  last_connected_at?: string;
  last_error?: string;
  can_send_test: boolean;
  qr?: QQBotQRStatus;
  logs: string[];
}

export interface QQBotConfigPatch {
  enabled?: boolean;
  owner_user_openid?: string;
  workspace?: string;
  model?: string;
  effort?: string;
  permission_mode?: string;
  allow_c2c?: boolean;
  allow_group?: boolean;
  restrict_to_owner?: boolean;
  allowed_users?: string[];
  allowed_groups?: string[];
}

export interface AgyStatus {
  installed: boolean;
  binary_path: string;
  version: string;
  authenticated: boolean;
  home_dir: string;
  proxy_active: boolean;
  warm_sessions?: string[];
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

export interface Attachment {
  id: string;
  name: string;
  path: string;
  url: string;
  mime_type: string;
  size: number;
  is_image: boolean;
}

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  attachments?: Attachment[];
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
  effort?: string;
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
  effort?: string;
  permission_mode?: string;
  messages: ChatMessage[];
  created_at: string;
  updated_at: string;
}

export interface PermissionOption {
  option_id: string;
  name: string;
  kind: string; // allow_once | allow_always | reject_once | reject_always
}

export interface PermissionRequest {
  request_id: string;
  session_id: string;
  tool_call_id?: string;
  title: string;
  kind?: string;
  input?: string;
  options: PermissionOption[];
}

export interface ChatSSEEvent {
  type:
    | 'meta'
    | 'thinking'
    | 'text'
    | 'tool_call'
    | 'tool_result'
    | 'permission_request'
    | 'permission_resolved'
    | 'usage'
    | 'error'
    | 'done';
  conversation_id?: string;
  delta?: string;
  tool_call?: ToolCall;
  permission_request?: PermissionRequest;
  usage?: TokenUsage;
  error?: string;
}

export interface TerminalLaunchRequest {
  mode: 'agy' | 'shell';
  workspace?: string;
  conversationId?: string;
  nonce: number;
}
