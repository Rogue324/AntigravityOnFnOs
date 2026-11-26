package qqbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	intentGroupAndC2C = 1 << 25

	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opResume         = 6
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatACK   = 11

	closeAuthFailed          = 4004
	closeInvalidSession      = 4006
	closeSeqOutOfRange       = 4007
	closeRateLimited         = 4008
	closeSessionTimeout      = 4009
	closeInsufficientIntents = 4914
	closeDisallowedIntents   = 4915
)

var reconnectDelays = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

type wsPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

type readyEventData struct {
	SessionID string `json:"session_id"`
	User      struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
}

type c2cMessageEvent struct {
	ID          string              `json:"id"`
	Content     string              `json:"content"`
	Timestamp   string              `json:"timestamp"`
	Attachments []InboundAttachment `json:"attachments"`
	Author      struct {
		UserOpenID string `json:"user_openid"`
	} `json:"author"`
}

type groupMessageEvent struct {
	ID          string              `json:"id"`
	GroupOpenID string              `json:"group_openid"`
	Content     string              `json:"content"`
	Timestamp   string              `json:"timestamp"`
	Attachments []InboundAttachment `json:"attachments"`
	Author      struct {
		MemberOpenID string `json:"member_openid"`
	} `json:"author"`
}

type gatewayCallbacks struct {
	onStateChange func(state string, lastErr string)
	onReady       func(sessionID, botUserID, botUsername string)
	onSessionSave func(sessionID string, lastSeq int64)
	onMessage     func(msg InboundMessage)
	log           func(string)
}

type gatewayRunner struct {
	client *Client
	cb     gatewayCallbacks

	mu           sync.Mutex
	sessionID    string
	lastSeq      int64
	hasSeq       bool
	refreshToken bool
}

func newGatewayRunner(client *Client, savedSessionID string, savedLastSeq int64, cb gatewayCallbacks) *gatewayRunner {
	return &gatewayRunner{
		client:    client,
		cb:        cb,
		sessionID: savedSessionID,
		lastSeq:   savedLastSeq,
		hasSeq:    savedSessionID != "" && savedLastSeq > 0,
	}
}

func (g *gatewayRunner) log(format string, args ...any) {
	if g.cb.log != nil {
		g.cb.log(fmt.Sprintf(format, args...))
	}
}

func (g *gatewayRunner) setState(state, lastErr string) {
	if g.cb.onStateChange != nil {
		g.cb.onStateChange(state, lastErr)
	}
}

func (g *gatewayRunner) run(ctx context.Context) {
	attempt := 0
	for {
		if ctx.Err() != nil {
			g.setState("idle", "")
			return
		}

		if attempt == 0 {
			g.setState("connecting", "")
		} else {
			g.setState("reconnecting", "")
		}

		connectedAt, fatal, customDelay, err := g.connectOnce(ctx)
		if ctx.Err() != nil {
			g.setState("idle", "")
			return
		}

		if !connectedAt.IsZero() && time.Since(connectedAt) > 30*time.Second {
			attempt = 0
		}

		if fatal {
			errText := "QQ 网关连接被拒绝"
			if err != nil {
				errText = err.Error()
			}
			g.log("[gateway] 致命错误，停止重连: %s", errText)
			g.setState("error", errText)
			return
		}

		errText := ""
		if err != nil {
			errText = err.Error()
			g.log("[gateway] 连接断开: %s", errText)
		}

		delay := customDelay
		if delay <= 0 {
			idx := attempt
			if idx >= len(reconnectDelays) {
				idx = len(reconnectDelays) - 1
			}
			delay = reconnectDelays[idx]
		}
		attempt++

		g.setState("reconnecting", errText)
		g.log("[gateway] 将在 %s 后重试连接 (第 %d 次)", delay, attempt)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			g.setState("idle", "")
			return
		case <-timer.C:
		}
	}
}

func (g *gatewayRunner) connectOnce(ctx context.Context) (connectedAt time.Time, fatal bool, customDelay time.Duration, err error) {
	g.mu.Lock()
	needRefresh := g.refreshToken
	g.refreshToken = false
	g.mu.Unlock()

	if needRefresh {
		g.client.ClearTokenCache()
	}

	token, err := g.client.GetAccessToken(ctx)
	if err != nil {
		return time.Time{}, false, 0, err
	}

	gatewayURL, err := g.client.GetGatewayURL(ctx)
	if err != nil {
		return time.Time{}, false, 0, err
	}

	g.log("[gateway] 正在连接 %s", gatewayURL)
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		Proxy:            http.ProxyFromEnvironment,
	}
	headers := http.Header{}
	headers.Set("User-Agent", qqUserAgent)

	conn, _, err := dialer.DialContext(ctx, gatewayURL, headers)
	if err != nil {
		return time.Time{}, false, 0, fmt.Errorf("WebSocket 握手失败: %w", err)
	}
	defer conn.Close()

	// Ensure context cancellation unblocks conn.ReadMessage immediately.
	connCtx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()
	go func() {
		<-connCtx.Done()
		_ = conn.Close()
	}()

	var writeMu sync.Mutex
	sendJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(v)
	}

	var hbCancel context.CancelFunc
	stopHeartbeat := func() {
		if hbCancel != nil {
			hbCancel()
			hbCancel = nil
		}
	}
	defer stopHeartbeat()

	for {
		_, data, readErr := conn.ReadMessage()
		if readErr != nil {
			if ctx.Err() != nil {
				return connectedAt, false, 0, ctx.Err()
			}
			var closeErr *websocket.CloseError
			if errors.As(readErr, &closeErr) {
				return connectedAt, g.handleCloseCode(closeErr.Code), closeDelay(closeErr.Code), fmt.Errorf("WebSocket 关闭 (%d): %s", closeErr.Code, closeErr.Text)
			}
			return connectedAt, false, 0, readErr
		}

		var payload wsPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			g.log("[gateway] 忽略无法解析的帧: %v", err)
			continue
		}

		if payload.S != nil {
			g.mu.Lock()
			g.lastSeq = *payload.S
			g.hasSeq = true
			sid := g.sessionID
			seq := g.lastSeq
			g.mu.Unlock()
			if sid != "" && g.cb.onSessionSave != nil {
				g.cb.onSessionSave(sid, seq)
			}
		}

		switch payload.Op {
		case opHello:
			var hello struct {
				HeartbeatInterval int `json:"heartbeat_interval"`
			}
			if err := json.Unmarshal(payload.D, &hello); err != nil || hello.HeartbeatInterval <= 0 {
				hello.HeartbeatInterval = 30000
			}

			g.mu.Lock()
			canResume := g.sessionID != "" && g.hasSeq
			sid := g.sessionID
			seq := g.lastSeq
			g.mu.Unlock()

			var authFrame map[string]any
			if canResume {
				authFrame = map[string]any{
					"op": opResume,
					"d": map[string]any{
						"token":      "QQBot " + token,
						"session_id": sid,
						"seq":        seq,
					},
				}
			} else {
				authFrame = map[string]any{
					"op": opIdentify,
					"d": map[string]any{
						"token":   "QQBot " + token,
						"intents": intentGroupAndC2C,
						"shard":   []int{0, 1},
					},
				}
			}

			if err := sendJSON(authFrame); err != nil {
				return connectedAt, false, 0, fmt.Errorf("发送鉴权帧失败: %w", err)
			}

			stopHeartbeat()
			hbCtx, cancel := context.WithCancel(connCtx)
			hbCancel = cancel
			interval := time.Duration(hello.HeartbeatInterval) * time.Millisecond
			go func() {
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					select {
					case <-hbCtx.Done():
						return
					case <-ticker.C:
						g.mu.Lock()
						var seqVal any
						if g.hasSeq {
							seqVal = g.lastSeq
						}
						g.mu.Unlock()
						if err := sendJSON(map[string]any{"op": opHeartbeat, "d": seqVal}); err != nil {
							return
						}
					}
				}
			}()

		case opDispatch:
			switch payload.T {
			case "READY":
				var ready readyEventData
				if err := json.Unmarshal(payload.D, &ready); err == nil {
					g.mu.Lock()
					g.sessionID = ready.SessionID
					seq := g.lastSeq
					g.mu.Unlock()
					connectedAt = time.Now()
					g.log("[gateway] 已就绪: bot=%s (%s)", ready.User.Username, ready.User.ID)
					if g.cb.onSessionSave != nil {
						g.cb.onSessionSave(ready.SessionID, seq)
					}
					if g.cb.onReady != nil {
						g.cb.onReady(ready.SessionID, ready.User.ID, ready.User.Username)
					}
					g.setState("connected", "")
				}

			case "RESUMED":
				connectedAt = time.Now()
				g.log("[gateway] 会话已恢复 (RESUMED)")
				g.setState("connected", "")

			case "C2C_MESSAGE_CREATE":
				var ev c2cMessageEvent
				if err := json.Unmarshal(payload.D, &ev); err == nil {
					if g.cb.onMessage != nil {
						go g.cb.onMessage(InboundMessage{
							Kind:        "c2c",
							SenderID:    ev.Author.UserOpenID,
							MessageID:   ev.ID,
							Content:     ev.Content,
							Timestamp:   ev.Timestamp,
							Attachments: ev.Attachments,
						})
					}
				}

			case "GROUP_AT_MESSAGE_CREATE":
				var ev groupMessageEvent
				if err := json.Unmarshal(payload.D, &ev); err == nil {
					if g.cb.onMessage != nil {
						go g.cb.onMessage(InboundMessage{
							Kind:        "group",
							SenderID:    ev.Author.MemberOpenID,
							GroupOpenID: ev.GroupOpenID,
							MessageID:   ev.ID,
							Content:     ev.Content,
							Timestamp:   ev.Timestamp,
							Attachments: ev.Attachments,
						})
					}
				}
			}

		case opHeartbeatACK:
			// healthy

		case opReconnect:
			g.log("[gateway] 服务端要求重新连接 (op=7)")
			return connectedAt, false, 2 * time.Second, nil

		case opInvalidSession:
			var canResume bool
			_ = json.Unmarshal(payload.D, &canResume)
			if !canResume {
				g.clearSession(true)
			}
			g.log("[gateway] 会话失效 (op=9, resumable=%v)", canResume)
			return connectedAt, false, 3 * time.Second, nil
		}
	}
}

func (g *gatewayRunner) clearSession(refreshToken bool) {
	g.mu.Lock()
	g.sessionID = ""
	g.lastSeq = 0
	g.hasSeq = false
	if refreshToken {
		g.refreshToken = true
	}
	g.mu.Unlock()
	if g.cb.onSessionSave != nil {
		g.cb.onSessionSave("", 0)
	}
}

func (g *gatewayRunner) handleCloseCode(code int) (fatal bool) {
	switch code {
	case closeAuthFailed:
		g.clearSession(true)
		return true
	case closeInsufficientIntents, closeDisallowedIntents:
		g.clearSession(false)
		return true
	case closeInvalidSession, closeSeqOutOfRange, closeSessionTimeout:
		g.clearSession(false)
		return false
	default:
		return false
	}
}

func closeDelay(code int) time.Duration {
	if code == closeRateLimited {
		return 60 * time.Second
	}
	return 0
}
