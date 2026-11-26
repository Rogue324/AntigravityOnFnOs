package qqbot

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/agy"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/config"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

func TestDecryptBindSecret(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand key: %v", err)
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand nonce: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}

	plain := "super-secret-qq-bot-token-123"
	ct := gcm.Seal(nil, nonce, []byte(plain), nil)
	combined := append(append([]byte(nil), nonce...), ct...)

	got, err := DecryptBindSecret(
		base64.StdEncoding.EncodeToString(combined),
		base64.StdEncoding.EncodeToString(key),
	)
	if err != nil {
		t.Fatalf("DecryptBindSecret failed: %v", err)
	}
	if got != plain {
		t.Fatalf("expected %q, got %q", plain, got)
	}
}

func TestChunkMarkdownTextBalancesFences(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("```go\n")
	for i := 0; i < 60; i++ {
		sb.WriteString("fmt.Println(\"hello world from antigravity\")\n")
	}
	sb.WriteString("```\n")

	chunks := chunkMarkdownText(sb.String(), 400)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if strings.Count(c, "```")%2 != 0 {
			t.Fatalf("chunk %d has unbalanced code fence:\n%s", i, c)
		}
	}
}

func TestSendMarkdownReplyFallbackOn40034090(t *testing.T) {
	var msgTypes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-123",
				"expires_in":   "7200",
			})
		case "/v2/users/u-1/messages":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mt := int(body["msg_type"].(float64))
			msgTypes = append(msgTypes, mt)
			if mt == 2 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":    40034090,
					"message": "markdown not allowed",
				})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg-ok"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient("10001", "sec", srv.Client(), nil)
	c.tokenBaseURL = srv.URL
	c.apiBaseURL = srv.URL

	if err := c.SendMarkdownReply(context.Background(), "c2c", "u-1", "in-1", "**hello**"); err != nil {
		t.Fatalf("SendMarkdownReply error: %v", err)
	}
	if len(msgTypes) != 2 || msgTypes[0] != 2 || msgTypes[1] != 0 {
		t.Fatalf("expected [2, 0] fallback sequence, got %v", msgTypes)
	}
}

func TestAccessControlAndMentionCleaning(t *testing.T) {
	b := &bridge{}
	cfg := BotConfig{
		AllowC2C:        true,
		AllowGroup:      true,
		RestrictToOwner: true,
		OwnerUserOpenID: "owner-1",
		AllowedGroups:   []string{"grp-1"},
	}

	if ok, _ := b.checkAccess(cfg, InboundMessage{Kind: "c2c", SenderID: "owner-1"}); !ok {
		t.Fatal("expected owner C2C allowed")
	}
	if ok, reason := b.checkAccess(cfg, InboundMessage{Kind: "c2c", SenderID: "other-2"}); ok || !strings.Contains(reason, "other-2") {
		t.Fatalf("expected non-owner C2C blocked with OpenID hint, got ok=%v reason=%q", ok, reason)
	}
	if ok, _ := b.checkAccess(cfg, InboundMessage{Kind: "group", GroupOpenID: "grp-1", SenderID: "u-9"}); !ok {
		t.Fatal("expected whitelisted group allowed")
	}
	if ok, _ := b.checkAccess(cfg, InboundMessage{Kind: "group", GroupOpenID: "grp-2", SenderID: "u-9"}); ok {
		t.Fatal("expected non-whitelisted group blocked")
	}

	if got := cleanInboundContent("  <@!123456789>   /status  "); got != "/status" {
		t.Fatalf("expected '/status', got %q", got)
	}
}

func TestManagerConfigPersistence(t *testing.T) {
	tmp := t.TempDir()
	paths := config.Paths{
		DataDir:      tmp,
		HomeDir:      tmp + "/home",
		BinDir:       tmp + "/bin",
		ConfigFile:   tmp + "/config.json",
		SessionsFile: tmp + "/sessions.json",
	}
	cfgStore, err := config.NewStore(paths)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	sessStore, err := session.NewStore(paths.SessionsFile)
	if err != nil {
		t.Fatalf("session.NewStore: %v", err)
	}
	agyMgr := agy.NewManager(cfgStore)

	mgr, err := NewManager(cfgStore, agyMgr, sessStore)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	restrict := true
	allowGroup := false
	users := []string{"u1", " u2 ", "u1"}
	st, err := mgr.UpdateConfig(ConfigPatch{
		RestrictToOwner: &restrict,
		AllowGroup:      &allowGroup,
		AllowedUsers:    &users,
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if !st.Config.RestrictToOwner || st.Config.AllowGroup || len(st.Config.AllowedUsers) != 2 {
		t.Fatalf("unexpected status after UpdateConfig: %+v", st.Config)
	}

	// Reload from disk
	mgr2, err := NewManager(cfgStore, agyMgr, sessStore)
	if err != nil {
		t.Fatalf("reload NewManager: %v", err)
	}
	st2 := mgr2.Status()
	if !st2.Config.RestrictToOwner || st2.Config.AllowGroup || len(st2.Config.AllowedUsers) != 2 {
		t.Fatalf("persisted config mismatch: %+v", st2.Config)
	}

	var called atomic.Int32
	_ = called
}

func TestPresentPermissionPromptDualButtonsAndFallback(t *testing.T) {
	var payloads []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-btn",
				"expires_in":   "7200",
			})
		case "/v2/users/u-1/messages":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			payloads = append(payloads, body)
			if int(body["msg_type"].(float64)) == 2 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 40034090, "message": "keyboard not allowed"})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ok"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient("10001", "sec", srv.Client(), nil)
	c.tokenBaseURL = srv.URL
	c.apiBaseURL = srv.URL

	mgr := &Manager{}
	b := newBridge(mgr)
	b.presentPermissionPrompt(context.Background(), c, "c2c:u-1", InboundMessage{
		Kind:      "c2c",
		SenderID:  "u-1",
		MessageID: "in-99",
	}, &agy.PermissionRequest{
		SessionID: "sess-1",
		RequestID: "req-1",
		Title:     "Run shell command",
		Input:     "ls -la",
		Options: []agy.PermissionOption{
			{OptionID: "opt-always", Name: "Always Allow", Kind: "allow_always"},
			{OptionID: "opt-once", Name: "Allow Once", Kind: "allow_once"},
		},
	})

	if len(payloads) != 2 {
		t.Fatalf("expected 2 attempts (keyboard + plain fallback), got %d", len(payloads))
	}
	if int(payloads[0]["msg_type"].(float64)) != 2 || payloads[0]["keyboard"] == nil {
		t.Fatalf("expected first attempt to send msg_type=2 with keyboard, got %+v", payloads[0])
	}
	if int(payloads[1]["msg_type"].(float64)) != 0 {
		t.Fatalf("expected second attempt to fall back to msg_type=0, got %+v", payloads[1])
	}
}
