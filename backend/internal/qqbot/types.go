package qqbot

import "time"

// BotConfig holds persistent QQ bot configuration stored in ${TRIM_PKGVAR}/qqbot.json.
type BotConfig struct {
	Enabled         bool     `json:"enabled"`
	AppID           string   `json:"app_id"`
	AppSecret       string   `json:"app_secret"`
	OwnerUserOpenID string   `json:"owner_user_openid,omitempty"`
	Workspace       string   `json:"workspace,omitempty"`
	Model           string   `json:"model,omitempty"`
	Effort          string   `json:"effort,omitempty"`
	PermissionMode  string   `json:"permission_mode,omitempty"`
	AllowC2C        bool     `json:"allow_c2c"`
	AllowGroup      bool     `json:"allow_group"`
	RestrictToOwner bool     `json:"restrict_to_owner"`
	AllowedUsers    []string `json:"allowed_users,omitempty"`
	AllowedGroups   []string `json:"allowed_groups,omitempty"`
	CreatedAt       string   `json:"created_at,omitempty"`
	ConnectedAt     string   `json:"connected_at,omitempty"`
}

// PublicBotConfig is the sanitized configuration returned to the frontend.
type PublicBotConfig struct {
	Configured      bool     `json:"configured"`
	Enabled         bool     `json:"enabled"`
	AppID           string   `json:"app_id"`
	AppIDMasked     string   `json:"app_id_masked"`
	HasAppSecret    bool     `json:"has_app_secret"`
	OwnerUserOpenID string   `json:"owner_user_openid,omitempty"`
	Workspace       string   `json:"workspace,omitempty"`
	Model           string   `json:"model,omitempty"`
	Effort          string   `json:"effort,omitempty"`
	PermissionMode  string   `json:"permission_mode,omitempty"`
	AllowC2C        bool     `json:"allow_c2c"`
	AllowGroup      bool     `json:"allow_group"`
	RestrictToOwner bool     `json:"restrict_to_owner"`
	AllowedUsers    []string `json:"allowed_users"`
	AllowedGroups   []string `json:"allowed_groups"`
	CreatedAt       string   `json:"created_at,omitempty"`
	ConnectedAt     string   `json:"connected_at,omitempty"`
}

// ConfigPatch represents partial updates to BotConfig from the settings UI.
type ConfigPatch struct {
	Enabled         *bool     `json:"enabled"`
	OwnerUserOpenID *string   `json:"owner_user_openid"`
	Workspace       *string   `json:"workspace"`
	Model           *string   `json:"model"`
	Effort          *string   `json:"effort"`
	PermissionMode  *string   `json:"permission_mode"`
	AllowC2C        *bool     `json:"allow_c2c"`
	AllowGroup      *bool     `json:"allow_group"`
	RestrictToOwner *bool     `json:"restrict_to_owner"`
	AllowedUsers    *[]string `json:"allowed_users"`
	AllowedGroups   *[]string `json:"allowed_groups"`
}

// BotState persists conversation-to-session mappings and gateway resume state.
type BotState struct {
	Conversations     map[string]string `json:"conversations"`
	LastC2CUserOpenID string            `json:"last_c2c_user_openid,omitempty"`
	GatewaySessionID  string            `json:"gateway_session_id,omitempty"`
	GatewayLastSeq    int64             `json:"gateway_last_seq,omitempty"`
}

// QRAttemptStatus describes the live state of an active or recent QR provisioning attempt.
type QRAttemptStatus struct {
	AttemptID       string `json:"attempt_id"`
	Status          string `json:"status"` // starting | pending | refreshing | connecting | connected | failed | cancelled
	QRRevision      int    `json:"qr_revision"`
	VerificationURL string `json:"verification_url,omitempty"`
	ExpiresAt       int64  `json:"expires_at,omitempty"` // unix ms
	Error           string `json:"error,omitempty"`
	PollIntervalMs  int    `json:"poll_interval_ms"`
}

// Status describes the live runtime state of the QQ bot service.
type Status struct {
	Config          PublicBotConfig  `json:"config"`
	ConnectionState string           `json:"connection_state"` // idle | connecting | connected | reconnecting | error
	Connected       bool             `json:"connected"`
	BotUsername     string           `json:"bot_username,omitempty"`
	BotUserID       string           `json:"bot_user_id,omitempty"`
	LastConnectedAt string           `json:"last_connected_at,omitempty"`
	LastError       string           `json:"last_error,omitempty"`
	CanSendTest     bool             `json:"can_send_test"`
	QR              *QRAttemptStatus `json:"qr,omitempty"`
	Logs            []string         `json:"logs"`
}

// InboundAttachment represents an attachment on an inbound QQ message.
type InboundAttachment struct {
	ContentType  string `json:"content_type"`
	URL          string `json:"url"`
	Filename     string `json:"filename,omitempty"`
	Height       int    `json:"height,omitempty"`
	Width        int    `json:"width,omitempty"`
	Size         int64  `json:"size,omitempty"`
	VoiceWavURL  string `json:"voice_wav_url,omitempty"`
	ASRReferText string `json:"asr_refer_text,omitempty"`
}

// InboundMessage is a normalized C2C or Group @ message from the QQ Gateway.
type InboundMessage struct {
	Kind        string // "c2c" | "group"
	SenderID    string // user_openid (c2c) or member_openid (group)
	GroupOpenID string // non-empty for group messages
	MessageID   string
	Content     string
	Timestamp   string
	Attachments []InboundAttachment
}

func (m InboundMessage) ConversationKey() string {
	if m.Kind == "group" {
		return "group:" + m.GroupOpenID
	}
	return "c2c:" + m.SenderID
}

func (m InboundMessage) TargetID() string {
	if m.Kind == "group" {
		return m.GroupOpenID
	}
	return m.SenderID
}

func maskAppID(appID string) string {
	if len(appID) <= 4 {
		return appID
	}
	if len(appID) <= 8 {
		return appID[:2] + "****" + appID[len(appID)-2:]
	}
	return appID[:4] + "****" + appID[len(appID)-4:]
}

func formatTimeISO(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
