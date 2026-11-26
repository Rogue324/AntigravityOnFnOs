package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/agy"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/auth"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/config"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/qqbot"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
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
	qqMgr, err := qqbot.NewManager(cfgStore, agyMgr, sessStore)
	if err != nil {
		t.Fatalf("qqbot.NewManager: %v", err)
	}
	return New(cfgStore, auth.NewManager(cfgStore), agyMgr, sessStore, qqMgr, "/app/antigravity-cli")
}

func TestAuthSettingsAndSessionsFlow(t *testing.T) {
	srv := newTestServer(t)

	// 1. Health check works both at root and under fnOS gateway prefix
	for _, p := range []string{"/api/health", "/app/antigravity-cli/api/health"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", p, rec.Code)
		}
	}

	// Gateway root without trailing slash redirects to trailing slash for relative assets
	req := httptest.NewRequest(http.MethodGet, "/app/antigravity-cli", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/app/antigravity-cli/" {
		t.Fatalf("expected redirect to /app/antigravity-cli/, got %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	// 2. Protected route rejected before login
	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 before login, got %d", rec.Code)
	}

	// 3. Setup initial password
	body := bytes.NewBufferString(`{"password":"secret-password"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/auth/setup", body)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for setup, got %d: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected session cookie to be set")
	}
	sessionCookie := cookies[0]

	// 4. Update proxy settings
	req = httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"http_proxy":"http://127.0.0.1:7890"}`))
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for PUT /api/settings, got %d", rec.Code)
	}
	var cfg config.PublicConfig
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if cfg.HTTPProxy != "http://127.0.0.1:7890" {
		t.Fatalf("expected proxy to be saved, got %q", cfg.HTTPProxy)
	}

	// 5. Create and list chat session
	req = httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewBufferString(`{"title":"测试会话"}`))
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for POST /api/sessions, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /api/sessions, got %d", rec.Code)
	}
	var listResp struct {
		Sessions []session.Summary `json:"sessions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(listResp.Sessions) != 1 || listResp.Sessions[0].Title != "测试会话" {
		t.Fatalf("unexpected sessions list: %+v", listResp.Sessions)
	}

	// 6. WS ticket is single-use
	req = httptest.NewRequest(http.MethodPost, "/api/auth/ws-ticket", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for ws-ticket, got %d", rec.Code)
	}
	var ticketResp struct {
		Ticket string `json:"ticket"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&ticketResp)
	if ticketResp.Ticket == "" {
		t.Fatalf("expected non-empty ws ticket")
	}
	if !srv.authMgr.ConsumeWSTicket(ticketResp.Ticket) {
		t.Fatalf("expected first ConsumeWSTicket to succeed")
	}
	if srv.authMgr.ConsumeWSTicket(ticketResp.Ticket) {
		t.Fatalf("expected second ConsumeWSTicket to fail (single-use)")
	}

	// 7. QQ bot status & config update routes
	req = httptest.NewRequest(http.MethodGet, "/api/qqbot/status", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /api/qqbot/status, got %d: %s", rec.Code, rec.Body.String())
	}
	var qqSt qqbot.Status
	if err := json.NewDecoder(rec.Body).Decode(&qqSt); err != nil {
		t.Fatalf("decode qqbot status: %v", err)
	}
	if qqSt.Config.Configured || qqSt.ConnectionState != "idle" {
		t.Fatalf("expected initial qqbot unbound/idle, got %+v", qqSt)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/qqbot/config", bytes.NewBufferString(`{"restrict_to_owner":true,"allowed_users":["openid-a"]}`))
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for PUT /api/qqbot/config, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&qqSt); err != nil {
		t.Fatalf("decode updated qqbot status: %v", err)
	}
	if !qqSt.Config.RestrictToOwner || len(qqSt.Config.AllowedUsers) != 1 || qqSt.Config.AllowedUsers[0] != "openid-a" {
		t.Fatalf("unexpected updated qqbot config: %+v", qqSt.Config)
	}
}
