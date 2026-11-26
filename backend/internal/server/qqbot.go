package server

import (
	"encoding/json"
	"net/http"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/qqbot"
)

func (s *Server) registerQQBotRoutes() {
	s.mux.HandleFunc("GET /api/qqbot/status", s.requireAuth(s.handleQQBotStatus))
	s.mux.HandleFunc("PUT /api/qqbot/config", s.requireAuth(s.handleQQBotUpdateConfig))
	s.mux.HandleFunc("POST /api/qqbot/bind", s.requireAuth(s.handleQQBotBind))
	s.mux.HandleFunc("POST /api/qqbot/unbind", s.requireAuth(s.handleQQBotUnbind))
	s.mux.HandleFunc("POST /api/qqbot/restart", s.requireAuth(s.handleQQBotRestart))
	s.mux.HandleFunc("POST /api/qqbot/test", s.requireAuth(s.handleQQBotTest))
	s.mux.HandleFunc("POST /api/qqbot/qr/start", s.requireAuth(s.handleQQBotQRStart))
	s.mux.HandleFunc("GET /api/qqbot/qr/status", s.requireAuth(s.handleQQBotQRStatus))
	s.mux.HandleFunc("POST /api/qqbot/qr/cancel", s.requireAuth(s.handleQQBotQRCancel))
}

func (s *Server) requireQQMgr(w http.ResponseWriter) bool {
	if s.qqMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "QQ 机器人模块未初始化")
		return false
	}
	return true
}

func (s *Server) handleQQBotStatus(w http.ResponseWriter, _ *http.Request) {
	if !s.requireQQMgr(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.qqMgr.Status())
}

func (s *Server) handleQQBotUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireQQMgr(w) {
		return
	}
	var patch qqbot.ConfigPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	st, err := s.qqMgr.UpdateConfig(patch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleQQBotBind(w http.ResponseWriter, r *http.Request) {
	if !s.requireQQMgr(w) {
		return
	}
	var body struct {
		AppID           string `json:"app_id"`
		AppSecret       string `json:"app_secret"`
		OwnerUserOpenID string `json:"owner_user_openid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	st, err := s.qqMgr.BindCredentials(body.AppID, body.AppSecret, body.OwnerUserOpenID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleQQBotUnbind(w http.ResponseWriter, _ *http.Request) {
	if !s.requireQQMgr(w) {
		return
	}
	st, err := s.qqMgr.Unbind()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleQQBotRestart(w http.ResponseWriter, _ *http.Request) {
	if !s.requireQQMgr(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.qqMgr.Restart())
}

func (s *Server) handleQQBotTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireQQMgr(w) {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.qqMgr.SendTestMessage(r.Context(), body.Text); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleQQBotQRStart(w http.ResponseWriter, _ *http.Request) {
	if !s.requireQQMgr(w) {
		return
	}
	attempt, err := s.qqMgr.StartQRProvisioning()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, attempt)
}

func (s *Server) handleQQBotQRStatus(w http.ResponseWriter, _ *http.Request) {
	if !s.requireQQMgr(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.qqMgr.GetQRProvisioning())
}

func (s *Server) handleQQBotQRCancel(w http.ResponseWriter, _ *http.Request) {
	if !s.requireQQMgr(w) {
		return
	}
	s.qqMgr.CancelQRProvisioning()
	writeJSON(w, http.StatusOK, s.qqMgr.GetQRProvisioning())
}
