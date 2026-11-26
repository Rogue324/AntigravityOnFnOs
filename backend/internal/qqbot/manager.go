package qqbot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/agy"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/config"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

const maxBotLogLines = 120

// Manager supervises the QQ bot configuration, QR provisioning flow, WebSocket gateway, and message bridge.
type Manager struct {
	cfgStore   *config.Store
	agyMgr     *agy.Manager
	sessStore  *session.Store
	configFile string
	stateFile  string
	qrClient   *QRClient
	bridge     *bridge

	mu              sync.Mutex
	cfg             BotConfig
	state           BotState
	connectionState string
	botUserID       string
	botUsername     string
	lastConnectedAt time.Time
	lastError       string
	logs            []string

	runCancel context.CancelFunc
	runDone   chan struct{}
	client    *Client

	qrCancel context.CancelFunc
	qrStatus *QRAttemptStatus
}

// NewManager initializes the QQ bot manager and loads persisted config & state from DataDir.
func NewManager(cfgStore *config.Store, agyMgr *agy.Manager, sessStore *session.Store) (*Manager, error) {
	dataDir := cfgStore.Paths().DataDir
	m := &Manager{
		cfgStore:        cfgStore,
		agyMgr:          agyMgr,
		sessStore:       sessStore,
		configFile:      filepath.Join(dataDir, "qqbot.json"),
		stateFile:       filepath.Join(dataDir, "qqbot_state.json"),
		qrClient:        NewQRClient(nil),
		connectionState: "idle",
		cfg: BotConfig{
			AllowC2C:   true,
			AllowGroup: false,
		},
		state: BotState{
			Conversations: make(map[string]string),
		},
	}
	m.bridge = newBridge(m)

	if err := m.loadConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := m.loadState(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return m, nil
}

// StartBackground launches the QQ bot gateway if already configured and enabled.
func (m *Manager) StartBackground() {
	m.mu.Lock()
	shouldStart := m.cfg.Enabled && m.cfg.AppID != "" && m.cfg.AppSecret != ""
	m.mu.Unlock()
	if shouldStart {
		m.Start()
	}
}

// Close gracefully stops QR provisioning and the WebSocket gateway.
func (m *Manager) Close() {
	m.CancelQRProvisioning()
	m.Stop()
}

// Start connects the QQ bot WebSocket gateway using the persisted credentials.
func (m *Manager) Start() {
	m.Stop()

	m.mu.Lock()
	if m.cfg.AppID == "" || m.cfg.AppSecret == "" {
		m.connectionState = "idle"
		m.mu.Unlock()
		return
	}
	appID := m.cfg.AppID
	appSecret := m.cfg.AppSecret
	savedSID := m.state.GatewaySessionID
	savedSeq := m.state.GatewayLastSeq

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.runCancel = cancel
	m.runDone = done
	m.connectionState = "connecting"
	m.lastError = ""
	client := NewClient(appID, appSecret, nil, func(line string) {
		m.appendLog("%s", line)
	})
	m.client = client
	m.appendLogLocked("[manager] 正在启动 QQ 机器人连接 (AppID=%s)...", maskAppID(appID))
	m.mu.Unlock()

	runner := newGatewayRunner(client, savedSID, savedSeq, gatewayCallbacks{
		onStateChange: func(state string, lastErr string) {
			m.mu.Lock()
			m.connectionState = state
			if lastErr != "" {
				m.lastError = lastErr
			} else if state == "connected" {
				m.lastError = ""
			}
			m.mu.Unlock()
		},
		onReady: func(sessionID, botUserID, botUsername string) {
			m.mu.Lock()
			m.botUserID = botUserID
			m.botUsername = botUsername
			m.lastConnectedAt = time.Now()
			m.lastError = ""
			m.cfg.ConnectedAt = formatTimeISO(m.lastConnectedAt)
			_ = m.saveConfigLocked()
			m.mu.Unlock()
		},
		onSessionSave: func(sessionID string, lastSeq int64) {
			m.mu.Lock()
			m.state.GatewaySessionID = sessionID
			m.state.GatewayLastSeq = lastSeq
			_ = m.saveStateLocked()
			m.mu.Unlock()
		},
		onMessage: func(msg InboundMessage) {
			m.bridge.handleInbound(ctx, client, msg)
		},
		log: func(line string) {
			m.appendLog("%s", line)
		},
	})

	go func() {
		defer close(done)
		runner.run(ctx)
	}()
}

// Stop disconnects the active QQ bot WebSocket gateway.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.runCancel
	done := m.runDone
	m.runCancel = nil
	m.runDone = nil
	m.client = nil
	if cancel != nil {
		m.appendLogLocked("[manager] 正在停止 QQ 机器人连接...")
	}
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}

	m.mu.Lock()
	if m.runCancel == nil {
		m.connectionState = "idle"
	}
	m.mu.Unlock()
}

// Restart reconnects the QQ bot gateway if configured and enabled.
func (m *Manager) Restart() Status {
	m.mu.Lock()
	canStart := m.cfg.AppID != "" && m.cfg.AppSecret != ""
	if canStart {
		m.cfg.Enabled = true
		_ = m.saveConfigLocked()
	}
	m.mu.Unlock()
	if canStart {
		m.Start()
	}
	return m.Status()
}

// BindCredentials saves manual AppID + AppSecret, enables the bot, and starts the gateway.
func (m *Manager) BindCredentials(appID, appSecret, ownerOpenID string) (Status, error) {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	ownerOpenID = strings.TrimSpace(ownerOpenID)
	if appID == "" || appSecret == "" {
		return m.Status(), errors.New("AppID 和 AppSecret 不能为空")
	}

	// Verify credentials by fetching an access token before committing.
	verifyCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	testClient := NewClient(appID, appSecret, nil, nil)
	if _, err := testClient.GetAccessToken(verifyCtx); err != nil {
		return m.Status(), fmt.Errorf("凭据校验失败，请检查 AppID 与 AppSecret：%w", err)
	}

	m.CancelQRProvisioning()

	m.mu.Lock()
	now := formatTimeISO(time.Now())
	if m.cfg.AppID != appID {
		// Switching bot account clears old gateway session resume token
		m.state.GatewaySessionID = ""
		m.state.GatewayLastSeq = 0
		_ = m.saveStateLocked()
	}
	m.cfg.AppID = appID
	m.cfg.AppSecret = appSecret
	m.cfg.Enabled = true
	if ownerOpenID != "" {
		m.cfg.OwnerUserOpenID = ownerOpenID
	}
	if m.cfg.CreatedAt == "" {
		m.cfg.CreatedAt = now
	}
	if err := m.saveConfigLocked(); err != nil {
		m.mu.Unlock()
		return m.Status(), err
	}
	m.mu.Unlock()

	m.Start()
	return m.Status(), nil
}

// Unbind stops the bot and clears credentials and gateway state.
func (m *Manager) Unbind() (Status, error) {
	m.CancelQRProvisioning()
	m.Stop()

	m.mu.Lock()
	m.cfg = BotConfig{
		AllowC2C:   true,
		AllowGroup: false,
	}
	m.state = BotState{
		Conversations: make(map[string]string),
	}
	m.botUserID = ""
	m.botUsername = ""
	m.lastConnectedAt = time.Time{}
	m.lastError = ""
	m.connectionState = "idle"
	m.appendLogLocked("[manager] 已移除 QQ 机器人绑定")
	err1 := m.saveConfigLocked()
	err2 := m.saveStateLocked()
	m.mu.Unlock()

	if err1 != nil {
		return m.Status(), err1
	}
	if err2 != nil {
		return m.Status(), err2
	}
	return m.Status(), nil
}

// UpdateConfig applies partial settings updates and synchronizes the gateway lifecycle.
func (m *Manager) UpdateConfig(patch ConfigPatch) (Status, error) {
	m.mu.Lock()
	prevEnabled := m.cfg.Enabled
	if patch.Enabled != nil {
		m.cfg.Enabled = *patch.Enabled
	}
	if patch.OwnerUserOpenID != nil {
		m.cfg.OwnerUserOpenID = strings.TrimSpace(*patch.OwnerUserOpenID)
	}
	if patch.Workspace != nil {
		m.cfg.Workspace = strings.TrimSpace(*patch.Workspace)
	}
	if patch.Model != nil {
		m.cfg.Model = strings.TrimSpace(*patch.Model)
	}
	if patch.Effort != nil {
		m.cfg.Effort = strings.TrimSpace(*patch.Effort)
	}
	if patch.PermissionMode != nil {
		m.cfg.PermissionMode = strings.TrimSpace(*patch.PermissionMode)
	}
	if patch.AllowC2C != nil {
		m.cfg.AllowC2C = *patch.AllowC2C
	}
	if patch.AllowGroup != nil {
		m.cfg.AllowGroup = *patch.AllowGroup
	}
	if patch.RestrictToOwner != nil {
		m.cfg.RestrictToOwner = *patch.RestrictToOwner
	}
	if patch.AllowedUsers != nil {
		m.cfg.AllowedUsers = cleanStringList(*patch.AllowedUsers)
	}
	if patch.AllowedGroups != nil {
		m.cfg.AllowedGroups = cleanStringList(*patch.AllowedGroups)
	}
	if err := m.saveConfigLocked(); err != nil {
		m.mu.Unlock()
		return m.Status(), err
	}
	enabled := m.cfg.Enabled
	configured := m.cfg.AppID != "" && m.cfg.AppSecret != ""
	running := m.runCancel != nil
	m.mu.Unlock()

	if enabled && configured && (!running || !prevEnabled) {
		m.Start()
	} else if !enabled && running {
		m.Stop()
	}
	return m.Status(), nil
}

// SendTestMessage sends a proactive test message to the bound owner or last C2C user.
func (m *Manager) SendTestMessage(ctx context.Context, text string) error {
	m.mu.Lock()
	client := m.client
	connected := m.connectionState == "connected"
	target := m.state.LastC2CUserOpenID
	if target == "" {
		target = m.cfg.OwnerUserOpenID
	}
	m.mu.Unlock()

	if !connected || client == nil {
		return errors.New("QQ 机器人当前未连接，请先启动连接")
	}
	if target == "" || target == "*" {
		return errors.New("暂无可投递的私聊目标，请先在手机 QQ 中向机器人发送任意一条私聊消息，或在配置中填写主人 OpenID")
	}
	if strings.TrimSpace(text) == "" {
		text = "Antigravity for fnOS：QQ 机器人连接测试成功！"
	}
	return client.SendText(ctx, "c2c", target, "", text, 0)
}

// StartQRProvisioning initiates a Tencent QQ Bot QR code bind flow and waits for the first QR URL.
func (m *Manager) StartQRProvisioning() (QRAttemptStatus, error) {
	m.CancelQRProvisioning()

	attemptID := newRandomHex(8)
	ctx, cancel := context.WithCancel(context.Background())

	firstQRCh := make(chan struct{})
	firstErrCh := make(chan error, 1)
	var firstOnce sync.Once

	m.mu.Lock()
	m.qrCancel = cancel
	m.qrStatus = &QRAttemptStatus{
		AttemptID:      attemptID,
		Status:         "starting",
		QRRevision:     0,
		PollIntervalMs: 1500,
	}
	m.appendLogLocked("[qr] 正在向腾讯平台申请绑定二维码...")
	m.mu.Unlock()

	go func() {
		creds, err := m.qrClient.RunQRProvisioning(
			ctx,
			func(verificationURL string, expiresAt time.Time) {
				m.mu.Lock()
				if m.qrStatus != nil && m.qrStatus.AttemptID == attemptID {
					m.qrStatus.Status = "pending"
					m.qrStatus.VerificationURL = verificationURL
					m.qrStatus.ExpiresAt = expiresAt.UnixMilli()
					m.qrStatus.QRRevision++
					m.qrStatus.Error = ""
				}
				m.mu.Unlock()
				firstOnce.Do(func() { close(firstQRCh) })
			},
			func() {
				m.mu.Lock()
				if m.qrStatus != nil && m.qrStatus.AttemptID == attemptID {
					m.qrStatus.Status = "refreshing"
					m.qrStatus.VerificationURL = ""
					m.qrStatus.ExpiresAt = 0
				}
				m.appendLogLocked("[qr] 二维码已过期，正在自动刷新...")
				m.mu.Unlock()
			},
		)

		if err != nil {
			m.mu.Lock()
			if m.qrStatus != nil && m.qrStatus.AttemptID == attemptID {
				if errors.Is(err, context.Canceled) {
					m.qrStatus.Status = "cancelled"
					m.qrStatus.Error = "扫码绑定已取消"
				} else {
					m.qrStatus.Status = "failed"
					m.qrStatus.Error = err.Error()
					m.appendLogLocked("[qr] 扫码绑定失败: %v", err)
				}
			}
			m.mu.Unlock()
			firstOnce.Do(func() {
				firstErrCh <- err
			})
			return
		}

		// Scanned successfully -> save credentials & start gateway
		m.mu.Lock()
		if m.qrStatus != nil && m.qrStatus.AttemptID == attemptID {
			m.qrStatus.Status = "connecting"
			m.qrStatus.VerificationURL = ""
		}
		m.appendLogLocked("[qr] 扫码授权成功 (AppID=%s)，正在连接机器人...", maskAppID(creds.AppID))
		m.mu.Unlock()

		now := formatTimeISO(time.Now())
		m.mu.Lock()
		if m.cfg.AppID != creds.AppID {
			m.state.GatewaySessionID = ""
			m.state.GatewayLastSeq = 0
			_ = m.saveStateLocked()
		}
		m.cfg.AppID = creds.AppID
		m.cfg.AppSecret = creds.AppSecret
		m.cfg.Enabled = true
		if creds.UserOpenID != "" {
			m.cfg.OwnerUserOpenID = creds.UserOpenID
			m.cfg.RestrictToOwner = true
			m.state.LastC2CUserOpenID = creds.UserOpenID
			_ = m.saveStateLocked()
		}
		if m.cfg.CreatedAt == "" {
			m.cfg.CreatedAt = now
		}
		saveErr := m.saveConfigLocked()
		m.mu.Unlock()

		if saveErr != nil {
			m.mu.Lock()
			if m.qrStatus != nil && m.qrStatus.AttemptID == attemptID {
				m.qrStatus.Status = "failed"
				m.qrStatus.Error = "保存凭据失败: " + saveErr.Error()
			}
			m.mu.Unlock()
			return
		}

		m.Start()

		m.mu.Lock()
		if m.qrStatus != nil && m.qrStatus.AttemptID == attemptID {
			m.qrStatus.Status = "connected"
			m.qrStatus.Error = ""
		}
		m.mu.Unlock()
	}()

	select {
	case <-firstQRCh:
		return m.GetQRProvisioning(), nil
	case err := <-firstErrCh:
		return m.GetQRProvisioning(), err
	case <-time.After(12 * time.Second):
		m.CancelQRProvisioning()
		return m.GetQRProvisioning(), errors.New("获取 QQ 绑定二维码超时，请检查网络连接后重试")
	}
}

// GetQRProvisioning returns a copy of the current QR provisioning status.
func (m *Manager) GetQRProvisioning() QRAttemptStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.qrStatus == nil {
		return QRAttemptStatus{Status: "idle", PollIntervalMs: 1500}
	}
	return *m.qrStatus
}

// CancelQRProvisioning aborts any active QR bind task.
func (m *Manager) CancelQRProvisioning() {
	m.mu.Lock()
	cancel := m.qrCancel
	m.qrCancel = nil
	if m.qrStatus != nil && (m.qrStatus.Status == "starting" || m.qrStatus.Status == "pending" || m.qrStatus.Status == "refreshing") {
		m.qrStatus.Status = "cancelled"
		m.qrStatus.Error = "扫码绑定已取消"
		m.qrStatus.VerificationURL = ""
	}
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Status returns the full sanitized status snapshot for the frontend UI.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	logsCopy := make([]string, len(m.logs))
	copy(logsCopy, m.logs)

	allowedUsers := make([]string, len(m.cfg.AllowedUsers))
	copy(allowedUsers, m.cfg.AllowedUsers)
	allowedGroups := make([]string, len(m.cfg.AllowedGroups))
	copy(allowedGroups, m.cfg.AllowedGroups)

	var qrCopy *QRAttemptStatus
	if m.qrStatus != nil {
		cp := *m.qrStatus
		qrCopy = &cp
	}

	canTest := m.connectionState == "connected" && (m.state.LastC2CUserOpenID != "" || (m.cfg.OwnerUserOpenID != "" && m.cfg.OwnerUserOpenID != "*"))

	return Status{
		Config: PublicBotConfig{
			Configured:      m.cfg.AppID != "" && m.cfg.AppSecret != "",
			Enabled:         m.cfg.Enabled,
			AppID:           m.cfg.AppID,
			AppIDMasked:     maskAppID(m.cfg.AppID),
			HasAppSecret:    m.cfg.AppSecret != "",
			OwnerUserOpenID: m.cfg.OwnerUserOpenID,
			Workspace:       m.cfg.Workspace,
			Model:           m.cfg.Model,
			Effort:          m.cfg.Effort,
			PermissionMode:  m.cfg.PermissionMode,
			AllowC2C:        m.cfg.AllowC2C,
			AllowGroup:      m.cfg.AllowGroup,
			RestrictToOwner: m.cfg.RestrictToOwner,
			AllowedUsers:    allowedUsers,
			AllowedGroups:   allowedGroups,
			CreatedAt:       m.cfg.CreatedAt,
			ConnectedAt:     m.cfg.ConnectedAt,
		},
		ConnectionState: m.connectionState,
		Connected:       m.connectionState == "connected",
		BotUsername:     m.botUsername,
		BotUserID:       m.botUserID,
		LastConnectedAt: formatTimeISO(m.lastConnectedAt),
		LastError:       m.lastError,
		CanSendTest:     canTest,
		QR:              qrCopy,
		Logs:            logsCopy,
	}
}

func (m *Manager) getConfigCopy() BotConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := m.cfg
	cp.AllowedUsers = append([]string(nil), m.cfg.AllowedUsers...)
	cp.AllowedGroups = append([]string(nil), m.cfg.AllowedGroups...)
	return cp
}

func (m *Manager) getConversationSession(convKey string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.Conversations[convKey]
}

func (m *Manager) setConversationSession(convKey, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Conversations == nil {
		m.state.Conversations = make(map[string]string)
	}
	m.state.Conversations[convKey] = sessionID
	_ = m.saveStateLocked()
}

func (m *Manager) recordLastC2CUser(openID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.LastC2CUserOpenID != openID {
		m.state.LastC2CUserOpenID = openID
		_ = m.saveStateLocked()
	}
}

func (m *Manager) appendLog(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendLogLocked(format, args...)
}

func (m *Manager) appendLogLocked(format string, args ...any) {
	line := strings.TrimRight(fmt.Sprintf(format, args...), "\r\n")
	if line == "" {
		return
	}
	entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), line)
	m.logs = append(m.logs, entry)
	if len(m.logs) > maxBotLogLines {
		m.logs = append([]string(nil), m.logs[len(m.logs)-maxBotLogLines:]...)
	}
}

func (m *Manager) loadConfig() error {
	raw, err := os.ReadFile(m.configFile)
	if err != nil {
		return err
	}
	var loaded BotConfig
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return err
	}
	m.cfg = loaded
	return nil
}

func (m *Manager) saveConfigLocked() error {
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.configFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.configFile)
}

func (m *Manager) loadState() error {
	raw, err := os.ReadFile(m.stateFile)
	if err != nil {
		return err
	}
	var loaded BotState
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return err
	}
	if loaded.Conversations == nil {
		loaded.Conversations = make(map[string]string)
	}
	m.state = loaded
	return nil
}

func (m *Manager) saveStateLocked() error {
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.stateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.stateFile)
}

func cleanStringList(items []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func newRandomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
