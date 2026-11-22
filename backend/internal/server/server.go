package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/websocket"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/agy"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/auth"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/config"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/fsapi"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

type Server struct {
	cfgStore      *config.Store
	authMgr       *auth.Manager
	agyMgr        *agy.Manager
	sessStore     *session.Store
	gatewayPrefix string
	upgrader      websocket.Upgrader
	mux           *http.ServeMux
}

func New(cfgStore *config.Store, authMgr *auth.Manager, agyMgr *agy.Manager, sessStore *session.Store, gatewayPrefix string) *Server {
	prefix := strings.TrimRight(strings.TrimSpace(gatewayPrefix), "/")
	s := &Server{
		cfgStore:      cfgStore,
		authMgr:       authMgr,
		agyMgr:        agyMgr,
		sessStore:     sessStore,
		gatewayPrefix: prefix,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  8192,
			WriteBufferSize: 8192,
			// Allow access via fnOS unified gateway, NAS LAN IP, custom domain, or fnConnect relay;
			// actual access control is enforced by session cookie / single-use WS ticket.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		mux: http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.gatewayPrefix != "" {
		if r.URL.Path == s.gatewayPrefix {
			target := s.gatewayPrefix + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
		if strings.HasPrefix(r.URL.Path, s.gatewayPrefix+"/") {
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(r.URL.Path, s.gatewayPrefix)
			if r2.URL.RawPath != "" {
				r2.URL.RawPath = strings.TrimPrefix(r2.URL.RawPath, s.gatewayPrefix)
			}
			s.mux.ServeHTTP(w, r2)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	// Public routes
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("POST /api/auth/setup", s.handleAuthSetup)
	s.mux.HandleFunc("POST /api/auth/skip", s.handleAuthSkip)
	s.mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	s.mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)

	// Authenticated routes
	s.mux.HandleFunc("POST /api/auth/change-password", s.requireAuth(s.handleChangePassword))
	s.mux.HandleFunc("POST /api/auth/ws-ticket", s.requireAuth(s.handleWSTicket))

	s.mux.HandleFunc("GET /api/settings", s.requireAuth(s.handleGetSettings))
	s.mux.HandleFunc("PUT /api/settings", s.requireAuth(s.handleUpdateSettings))
	s.mux.HandleFunc("POST /api/settings/test-proxy", s.requireAuth(s.handleTestProxy))

	s.mux.HandleFunc("GET /api/agy/status", s.requireAuth(s.handleAgyStatus))
	s.mux.HandleFunc("GET /api/agy/models", s.requireAuth(s.handleAgyModels))
	s.mux.HandleFunc("POST /api/agy/install", s.requireAuth(s.handleAgyInstall))

	s.mux.HandleFunc("GET /api/remote-control/status", s.requireAuth(s.handleRemoteControlStatus))
	s.mux.HandleFunc("POST /api/remote-control/restart", s.requireAuth(s.handleRemoteControlRestart))

	s.mux.HandleFunc("GET /api/fs/browse", s.requireAuth(s.handleFSBrowse))

	s.mux.HandleFunc("POST /api/uploads", s.requireAuth(s.handleUploadFile))
	s.mux.HandleFunc("GET /api/uploads/{sessionId}/{filename}", s.requireAuth(s.handleServeUpload))

	s.mux.HandleFunc("GET /api/sessions", s.requireAuth(s.handleListSessions))
	s.mux.HandleFunc("POST /api/sessions", s.requireAuth(s.handleCreateSession))
	s.mux.HandleFunc("GET /api/sessions/{id}", s.requireAuth(s.handleGetSession))
	s.mux.HandleFunc("PATCH /api/sessions/{id}", s.requireAuth(s.handleUpdateSession))
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.requireAuth(s.handleDeleteSession))
	s.mux.HandleFunc("POST /api/sessions/{id}/chat", s.requireAuth(s.handleSessionChat))
	s.mux.HandleFunc("POST /api/sessions/{id}/permission", s.requireAuth(s.handleSessionPermission))

	// WebSocket terminal (validates either single-use ticket or session cookie)
	s.mux.HandleFunc("GET /api/terminal/ws", s.handleTerminalWS)

	// Embedded SPA static files
	s.mux.Handle("/", spaHandler())
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authMgr.ValidateRequest(r); !ok {
			writeError(w, http.StatusUnauthorized, "未登录或会话已过期")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "app": "antigravity-cli"})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	_, authenticated := s.authMgr.ValidateRequest(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"initialized":   s.authMgr.IsInitialized(),
		"authenticated": authenticated,
		"auth_disabled": s.authMgr.IsAuthDisabled(),
	})
}

func (s *Server) handleAuthSkip(w http.ResponseWriter, _ *http.Request) {
	if err := s.authMgr.SkipInitialSetup(); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, auth.ErrAlreadyInitialized) {
			code = http.StatusConflict
		}
		writeError(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "auth_disabled": true})
}

func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	token, err := s.authMgr.SetupPassword(body.Password)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, auth.ErrAlreadyInitialized) {
			code = http.StatusConflict
		}
		writeError(w, code, err.Error())
		return
	}
	s.authMgr.SetSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	token, err := s.authMgr.Login(auth.ClientIP(r), body.Password)
	if err != nil {
		code := http.StatusUnauthorized
		if errors.Is(err, auth.ErrRateLimited) {
			code = http.StatusTooManyRequests
		}
		writeError(w, code, err.Error())
		return
	}
	s.authMgr.SetSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	token := auth.ExtractToken(r)
	s.authMgr.Logout(token)
	s.authMgr.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	wasDisabled := s.authMgr.IsAuthDisabled()
	if err := s.authMgr.ChangePassword(body.CurrentPassword, body.NewPassword); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, auth.ErrInvalidPassword) {
			code = http.StatusUnauthorized
		}
		writeError(w, code, err.Error())
		return
	}
	// If password protection was previously off, issue a session cookie so the current browser stays signed in.
	if wasDisabled {
		if token, err := s.authMgr.IssueSession(); err == nil {
			s.authMgr.SetSessionCookie(w, r, token)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleWSTicket(w http.ResponseWriter, _ *http.Request) {
	ticket, err := s.authMgr.CreateWSTicket()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ticket": ticket})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cfgStore.Public())
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		HTTPProxy             *string `json:"http_proxy"`
		HTTPSProxy            *string `json:"https_proxy"`
		NoProxy               *string `json:"no_proxy"`
		DefaultModel          *string `json:"default_model"`
		DefaultEffort         *string `json:"default_effort"`
		DefaultPermissionMode *string `json:"default_permission_mode"`
		AllowDangerousSkip    *bool   `json:"allow_dangerous_skip"`
		DefaultWorkspace      *string `json:"default_workspace"`
		AgyBinaryPath         *string `json:"agy_binary_path"`
		PersistentIdleMinutes *int    `json:"persistent_idle_minutes"`
		AuthDisabled          *bool   `json:"auth_disabled"`
		RemoteControlEnabled  *bool   `json:"remote_control_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	if body.AuthDisabled != nil && !*body.AuthDisabled && !s.authMgr.IsInitialized() {
		writeError(w, http.StatusBadRequest, "请先设置启动密码后再开启密码保护")
		return
	}
	wasDisabled := s.authMgr.IsAuthDisabled()
	prevCfg := s.cfgStore.Get()

	err := s.cfgStore.Update(func(c *config.Config) {
		if body.HTTPProxy != nil {
			c.HTTPProxy = strings.TrimSpace(*body.HTTPProxy)
		}
		if body.HTTPSProxy != nil {
			c.HTTPSProxy = strings.TrimSpace(*body.HTTPSProxy)
		}
		if body.NoProxy != nil {
			c.NoProxy = strings.TrimSpace(*body.NoProxy)
		}
		if body.DefaultModel != nil {
			c.DefaultModel = strings.TrimSpace(*body.DefaultModel)
		}
		if body.DefaultEffort != nil {
			c.DefaultEffort = strings.TrimSpace(*body.DefaultEffort)
		}
		if body.DefaultPermissionMode != nil {
			mode := strings.TrimSpace(*body.DefaultPermissionMode)
			switch mode {
			case "default", "accept-edits", "plan", "bypass":
				c.DefaultPermissionMode = mode
			}
		}
		if body.AllowDangerousSkip != nil {
			c.AllowDangerousSkip = *body.AllowDangerousSkip
		}
		if body.DefaultWorkspace != nil {
			c.DefaultWorkspace = strings.TrimSpace(*body.DefaultWorkspace)
		}
		if body.AgyBinaryPath != nil {
			c.AgyBinaryPath = strings.TrimSpace(*body.AgyBinaryPath)
		}
		if body.PersistentIdleMinutes != nil {
			mins := *body.PersistentIdleMinutes
			if mins < 0 {
				mins = 0
			}
			if mins > 120 {
				mins = 120
			}
			c.PersistentIdleMinutes = mins
		}
		if body.AuthDisabled != nil {
			c.AuthDisabled = *body.AuthDisabled
		}
		if body.RemoteControlEnabled != nil {
			c.RemoteControlEnabled = *body.RemoteControlEnabled
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if body.PersistentIdleMinutes != nil && *body.PersistentIdleMinutes == 0 {
		s.agyMgr.CloseChatPool()
	}

	nextCfg := s.cfgStore.Get()
	if body.RemoteControlEnabled != nil && nextCfg.RemoteControlEnabled != prevCfg.RemoteControlEnabled {
		s.agyMgr.SyncRemoteControl()
	} else if nextCfg.RemoteControlEnabled &&
		(nextCfg.HTTPProxy != prevCfg.HTTPProxy ||
			nextCfg.HTTPSProxy != prevCfg.HTTPSProxy ||
			nextCfg.NoProxy != prevCfg.NoProxy ||
			nextCfg.AgyBinaryPath != prevCfg.AgyBinaryPath ||
			nextCfg.DefaultWorkspace != prevCfg.DefaultWorkspace) {
		s.agyMgr.RestartRemoteControl()
	}

	// If re-enabling password protection from passwordless mode, issue a fresh session cookie
	// so the current browser remains authenticated.
	if wasDisabled && body.AuthDisabled != nil && !*body.AuthDisabled {
		if token, err := s.authMgr.IssueSession(); err == nil {
			s.authMgr.SetSessionCookie(w, r, token)
		}
	}
	writeJSON(w, http.StatusOK, s.cfgStore.Public())
}

func (s *Server) handleRemoteControlStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.agyMgr.RemoteControlStatus())
}

func (s *Server) handleRemoteControlRestart(w http.ResponseWriter, _ *http.Request) {
	if !s.cfgStore.Get().RemoteControlEnabled {
		writeError(w, http.StatusBadRequest, "远程控制未开启")
		return
	}
	if !s.agyMgr.RestartRemoteControl() {
		s.agyMgr.SyncRemoteControl()
	}
	writeJSON(w, http.StatusOK, s.agyMgr.RemoteControlStatus())
}

func (s *Server) handleTestProxy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		HTTPProxy  string `json:"http_proxy"`
		HTTPSProxy string `json:"https_proxy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	result := s.agyMgr.TestProxy(r.Context(), body.HTTPProxy, body.HTTPSProxy)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAgyStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.agyMgr.GetStatus(r.Context()))
}

func (s *Server) handleAgyModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.agyMgr.ListModels(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []agy.ModelOption{}, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleAgyInstall(w http.ResponseWriter, r *http.Request) {
	flusher, ok := prepareSSE(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	err := s.agyMgr.InstallOrUpgrade(r.Context(), func(line string) {
		sendSSE(w, flusher, "log", map[string]string{"line": line})
	})

	status := s.agyMgr.GetStatus(r.Context())
	if err != nil {
		sendSSE(w, flusher, "done", map[string]any{
			"ok":     false,
			"error":  err.Error(),
			"status": status,
		})
		return
	}
	sendSSE(w, flusher, "done", map[string]any{
		"ok":     true,
		"status": status,
	})
}

func (s *Server) handleFSBrowse(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("path")
	showHidden := r.URL.Query().Get("hidden") == "true"
	resp, err := fsapi.Browse(s.cfgStore.Paths(), target, showHidden)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListSessions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.sessStore.List()})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title          string   `json:"title"`
		Workspace      string   `json:"workspace"`
		ExtraDirs      []string `json:"extra_dirs"`
		Model          string   `json:"model"`
		Effort         string   `json:"effort"`
		PermissionMode string   `json:"permission_mode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	cfg := s.cfgStore.Get()
	if body.Workspace == "" {
		body.Workspace = cfg.DefaultWorkspace
	}
	if body.Model == "" {
		body.Model = cfg.DefaultModel
	}
	if body.Effort == "" {
		body.Effort = cfg.DefaultEffort
	}
	if body.PermissionMode == "" {
		body.PermissionMode = cfg.DefaultPermissionMode
	}

	sess, err := s.sessStore.Create(body.Title, body.Workspace, body.ExtraDirs, body.Model, body.Effort, body.PermissionMode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.sessStore.Get(id)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			writeError(w, http.StatusNotFound, "会话不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Title          *string   `json:"title"`
		Workspace      *string   `json:"workspace"`
		ExtraDirs      *[]string `json:"extra_dirs"`
		Model          *string   `json:"model"`
		Effort         *string   `json:"effort"`
		PermissionMode *string   `json:"permission_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	sess, err := s.sessStore.Update(id, func(sess *session.Session) {
		if body.Title != nil && strings.TrimSpace(*body.Title) != "" {
			sess.Title = strings.TrimSpace(*body.Title)
		}
		if body.Workspace != nil {
			sess.Workspace = strings.TrimSpace(*body.Workspace)
		}
		if body.ExtraDirs != nil {
			sess.ExtraDirs = append([]string(nil), (*body.ExtraDirs)...)
		}
		if body.Model != nil {
			sess.Model = strings.TrimSpace(*body.Model)
		}
		if body.Effort != nil {
			sess.Effort = strings.TrimSpace(*body.Effort)
		}
		if body.PermissionMode != nil {
			sess.PermissionMode = strings.TrimSpace(*body.PermissionMode)
		}
	})
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			writeError(w, http.StatusNotFound, "会话不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.agyMgr.CloseSession(id)
	if err := s.sessStore.Delete(id); err != nil {
		if errors.Is(err, session.ErrNotFound) {
			writeError(w, http.StatusNotFound, "会话不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSessionChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.sessStore.Get(id)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			writeError(w, http.StatusNotFound, "会话不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var body struct {
		Prompt         string               `json:"prompt"`
		Attachments    []session.Attachment `json:"attachments,omitempty"`
		Workspace      string               `json:"workspace,omitempty"`
		ExtraDirs      []string             `json:"extra_dirs,omitempty"`
		Model          string               `json:"model,omitempty"`
		Effort         string               `json:"effort,omitempty"`
		PermissionMode string               `json:"permission_mode,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" && len(body.Attachments) == 0 {
		writeError(w, http.StatusBadRequest, "消息内容和附件不能同时为空")
		return
	}

	// Update session settings if overridden on this turn, and auto-title on first message
	sess, err = s.sessStore.Update(id, func(cur *session.Session) {
		if body.Workspace != "" {
			cur.Workspace = body.Workspace
		}
		if body.ExtraDirs != nil {
			cur.ExtraDirs = append([]string(nil), body.ExtraDirs...)
		}
		if body.Model != "" {
			cur.Model = body.Model
		}
		if body.Effort != "" {
			cur.Effort = body.Effort
		}
		if body.PermissionMode != "" {
			cur.PermissionMode = body.PermissionMode
		}
		if len(cur.Messages) == 0 && (cur.Title == "" || cur.Title == "新对话") {
			titleSrc := prompt
			if titleSrc == "" && len(body.Attachments) > 0 {
				titleSrc = "附件: " + body.Attachments[0].Name
			}
			cur.Title = truncateTitle(titleSrc, 28)
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Persist user message immediately
	if _, err := s.sessStore.AppendMessage(id, session.Message{
		Role:        "user",
		Content:     prompt,
		Attachments: body.Attachments,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build effective prompt + extraDirs when attachments are included
	effectivePrompt := prompt
	effectiveExtraDirs := append([]string(nil), sess.ExtraDirs...)
	if len(body.Attachments) > 0 {
		var sb strings.Builder
		if effectivePrompt != "" {
			sb.WriteString(effectivePrompt)
			sb.WriteString("\n\n")
		} else {
			sb.WriteString("请查看并分析以下附件：\n\n")
		}
		sb.WriteString("附带文件（位于本地磁盘，请直接读取）：\n")
		for _, att := range body.Attachments {
			kind := "文件"
			if att.IsImage {
				kind = "图片"
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s (%s)\n", kind, att.Name, att.Path))
		}
		effectivePrompt = sb.String()

		uploadDir := s.uploadsBaseDir()
		hasUploadDir := false
		for _, d := range effectiveExtraDirs {
			if d == uploadDir {
				hasUploadDir = true
				break
			}
		}
		if !hasUploadDir {
			effectiveExtraDirs = append(effectiveExtraDirs, uploadDir)
		}
	}

	flusher, ok := prepareSSE(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	result, runErr := s.agyMgr.RunChat(r.Context(), agy.ChatRequest{
		SessionID:      sess.ID,
		Prompt:         effectivePrompt,
		ConversationID: sess.ConversationID,
		Workspace:      sess.Workspace,
		ExtraDirs:      effectiveExtraDirs,
		Model:          sess.Model,
		Effort:         sess.Effort,
		PermissionMode: sess.PermissionMode,
	}, func(ev agy.ChatEvent) {
		sendSSE(w, flusher, ev.Type, ev)
	})

	if runErr != nil && result == nil {
		errMsg := runErr.Error()
		_, _ = s.sessStore.AppendMessage(id, session.Message{
			Role:  "assistant",
			Error: errMsg,
		})
		sendSSE(w, flusher, "error", agy.ChatEvent{Type: "error", Error: errMsg})
		sendSSE(w, flusher, "done", agy.ChatEvent{Type: "done"})
		return
	}

	if result != nil {
		_, _ = s.sessStore.Update(id, func(cur *session.Session) {
			if result.ConversationID != "" {
				cur.ConversationID = result.ConversationID
			}
		})
		_, _ = s.sessStore.AppendMessage(id, result.Message)
	}
}

func (s *Server) handleSessionPermission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		RequestID string `json:"request_id"`
		OptionID  string `json:"option_id"`
		Cancelled bool   `json:"cancelled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if strings.TrimSpace(body.RequestID) == "" {
		writeError(w, http.StatusBadRequest, "缺少 request_id")
		return
	}
	if err := s.agyMgr.ResolvePermission(id, body.RequestID, agy.PermissionDecision{
		OptionID:  body.OptionID,
		Cancelled: body.Cancelled,
	}); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("ticket")
	authed := false
	if ticket != "" && s.authMgr.ConsumeWSTicket(ticket) {
		authed = true
	} else if _, ok := s.authMgr.ValidateRequest(r); ok {
		authed = true
	}
	if !authed {
		writeError(w, http.StatusUnauthorized, "WebSocket 鉴权失败")
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))

	opts := agy.PTYOptions{
		Mode:           r.URL.Query().Get("mode"),
		Workspace:      r.URL.Query().Get("workspace"),
		ConversationID: r.URL.Query().Get("conversation"),
		Cols:           uint16(cols),
		Rows:           uint16(rows),
	}

	if err := s.agyMgr.ServePTY(conn, opts); err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31m"+err.Error()+"\x1b[0m\r\n"))
	}
}

func prepareSSE(w http.ResponseWriter) (http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return flusher, true
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func truncateTitle(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "…"
}
