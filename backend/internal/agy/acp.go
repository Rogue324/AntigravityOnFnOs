package agy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

const (
	acpClientName    = "AntigravityOnFnOs"
	acpClientTitle   = "Antigravity for fnOS"
	acpClientVersion = "0.1.11"
)

type pendingPermission struct {
	sessionID     string
	agentReqID    string
	ch            chan PermissionDecision
	agentCancelCh chan struct{}
	once          sync.Once
}

func (p *pendingPermission) cancelByAgent() {
	p.once.Do(func() {
		close(p.agentCancelCh)
	})
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("ACP error (%d): %s", e.Code, e.Message)
}

type rpcResponse struct {
	result json.RawMessage
	err    *rpcError
}

type acpModeInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type acpConfigOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

type acpTurnContext struct {
	ctx            context.Context
	sessionID      string
	permissionMode string
	allowBypass    bool
	res            *ChatResult
	emit           func(ChatEvent)
	textBuilder    strings.Builder
	thinkingBuilder strings.Builder
	toolMap        map[string]int
	suppressUpdate bool
	mu             sync.Mutex
}

// acpWorker manages a single `agy acp` stdio JSON-RPC 2.0 subprocess for a chat session.
type acpWorker struct {
	mgr       *Manager
	sessionID string
	signature string
	cwd       string

	cmd       *exec.Cmd
	stdin     io.WriteCloser
	writeMu   sync.Mutex
	stderrBuf strings.Builder
	stderrMu  sync.Mutex

	nextID     atomic.Int64
	pendingMu  sync.Mutex
	pendingRPC map[int64]chan rpcResponse

	turnMu     sync.Mutex
	activeMu   sync.Mutex
	activeTurn *acpTurnContext

	acpSessionID    string
	availableModes  []acpModeInfo
	configOptions   []acpConfigOption
	lastModeSent    string
	lastModelSent   string
	lastEffortSent  string

	timerMu   sync.Mutex
	idleTimer *time.Timer
	closed    bool
	closeOnce sync.Once
	eofCh     chan struct{}
	onClose   func()
}

type acpPool struct {
	mu      sync.Mutex
	workers map[string]*acpWorker
}

func newACPPool() *acpPool {
	return &acpPool{
		workers: make(map[string]*acpWorker),
	}
}

func (p *acpPool) activeSessionIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.workers))
	for id, w := range p.workers {
		if !w.isClosed() {
			out = append(out, id)
		}
	}
	return out
}

func (p *acpPool) closeSession(sessionID string) {
	p.mu.Lock()
	w := p.workers[sessionID]
	delete(p.workers, sessionID)
	p.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

func (p *acpPool) closeAll() {
	p.mu.Lock()
	all := make([]*acpWorker, 0, len(p.workers))
	for id, w := range p.workers {
		all = append(all, w)
		delete(p.workers, id)
	}
	p.mu.Unlock()
	for _, w := range all {
		w.Close()
	}
}

func (w *acpWorker) isClosed() bool {
	w.timerMu.Lock()
	defer w.timerMu.Unlock()
	return w.closed
}

func (w *acpWorker) stopIdleTimer() {
	w.timerMu.Lock()
	defer w.timerMu.Unlock()
	if w.idleTimer != nil {
		w.idleTimer.Stop()
		w.idleTimer = nil
	}
}

func (w *acpWorker) resetIdleTimer(d time.Duration) {
	w.timerMu.Lock()
	defer w.timerMu.Unlock()
	if w.closed || d <= 0 {
		return
	}
	if w.idleTimer != nil {
		w.idleTimer.Stop()
	}
	w.idleTimer = time.AfterFunc(d, func() {
		w.Close()
	})
}

func (w *acpWorker) stderrText() string {
	w.stderrMu.Lock()
	defer w.stderrMu.Unlock()
	return strings.TrimSpace(w.stderrBuf.String())
}

func (w *acpWorker) Close() {
	w.closeOnce.Do(func() {
		w.timerMu.Lock()
		w.closed = true
		if w.idleTimer != nil {
			w.idleTimer.Stop()
			w.idleTimer = nil
		}
		w.timerMu.Unlock()

		_ = w.stdin.Close()

		done := make(chan struct{})
		go func() {
			_ = w.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if w.cmd.Process != nil {
				terminateProcessGroup(w.cmd.Process.Pid)
			}
			<-done
		}

		if w.onClose != nil {
			w.onClose()
		}
	})
}

func (w *acpWorker) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	_, err = w.stdin.Write(append(data, '\n'))
	return err
}

func (w *acpWorker) notify(method string, params any) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	return w.writeJSON(msg)
}

func (w *acpWorker) replyResult(id json.RawMessage, result any) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	return w.writeJSON(msg)
}

func (w *acpWorker) replyError(id json.RawMessage, code int, message string) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	return w.writeJSON(msg)
}

func (w *acpWorker) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := w.nextID.Add(1)
	ch := make(chan rpcResponse, 1)

	w.pendingMu.Lock()
	w.pendingRPC[id] = ch
	w.pendingMu.Unlock()

	cleanup := func() {
		w.pendingMu.Lock()
		delete(w.pendingRPC, id)
		w.pendingMu.Unlock()
	}

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	if err := w.writeJSON(req); err != nil {
		cleanup()
		return nil, err
	}

	select {
	case resp, ok := <-ch:
		cleanup()
		if !ok {
			stderr := w.stderrText()
			if stderr != "" {
				return nil, fmt.Errorf("agy acp 进程已退出: %s", stderr)
			}
			return nil, errors.New("agy acp 进程意外退出")
		}
		if resp.err != nil {
			return nil, resp.err
		}
		return resp.result, nil
	case <-w.eofCh:
		cleanup()
		stderr := w.stderrText()
		if stderr != "" {
			return nil, fmt.Errorf("agy acp 进程已退出: %s", stderr)
		}
		return nil, errors.New("agy acp 进程意外退出")
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	}
}

func (w *acpWorker) readLoop(stdout io.Reader) {
	defer func() {
		close(w.eofCh)
		w.pendingMu.Lock()
		for id, ch := range w.pendingRPC {
			close(ch)
			delete(w.pendingRPC, id)
		}
		w.pendingMu.Unlock()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env rpcEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}

		hasID := len(env.ID) > 0 && string(env.ID) != "null"
		if env.Method == "" && hasID {
			// Response to our outgoing request
			var numID int64
			if err := json.Unmarshal(env.ID, &numID); err == nil {
				w.pendingMu.Lock()
				ch, ok := w.pendingRPC[numID]
				w.pendingMu.Unlock()
				if ok {
					ch <- rpcResponse{result: env.Result, err: env.Error}
				}
			}
			continue
		}

		if env.Method != "" && !hasID {
			// Notification from agent
			w.handleNotification(env.Method, env.Params)
			continue
		}

		if env.Method != "" && hasID {
			// Request from agent -> handle in goroutine so reader never deadlocks!
			go w.handleAgentRequest(env.ID, env.Method, env.Params)
		}
	}
}

func (w *acpWorker) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "session/update":
		w.activeMu.Lock()
		turn := w.activeTurn
		w.activeMu.Unlock()
		if turn != nil {
			turn.handleSessionUpdate(params)
		}
	case "$/cancel_request":
		var p struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(params, &p) == nil && len(p.ID) > 0 {
			w.mgr.cancelPermissionByAgentID(string(p.ID))
		}
	}
}

func (w *acpWorker) handleAgentRequest(id json.RawMessage, method string, params json.RawMessage) {
	switch method {
	case "session/request_permission":
		w.handlePermissionRequest(id, params)
	default:
		_ = w.replyError(id, -32601, fmt.Sprintf("Method not supported: %s", method))
	}
}

func (w *acpWorker) handlePermissionRequest(id json.RawMessage, params json.RawMessage) {
	var req struct {
		SessionID string `json:"sessionId"`
		ToolCall  struct {
			ToolCallID string          `json:"toolCallId"`
			Title      string          `json:"title"`
			Kind       string          `json:"kind"`
			RawInput   json.RawMessage `json:"rawInput"`
			Locations  []struct {
				Path string `json:"path"`
			} `json:"locations"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		_ = w.replyError(id, -32602, "invalid session/request_permission params")
		return
	}

	opts := make([]PermissionOption, 0, len(req.Options))
	for _, o := range req.Options {
		opts = append(opts, PermissionOption{
			OptionID: o.OptionID,
			Name:     o.Name,
			Kind:     o.Kind,
		})
	}

	w.activeMu.Lock()
	turn := w.activeTurn
	w.activeMu.Unlock()

	if turn == nil {
		_ = w.replyResult(id, map[string]any{
			"outcome": map[string]any{"outcome": "cancelled"},
		})
		return
	}

	// Auto-approve when user explicitly enabled bypass mode and allow_dangerous_skip is on.
	if turn.permissionMode == "bypass" && turn.allowBypass {
		if optID := pickAllowOption(opts); optID != "" {
			_ = w.replyResult(id, map[string]any{
				"outcome": map[string]any{
					"outcome":  "selected",
					"optionId": optID,
				},
			})
			return
		}
	}

	// Build readable input summary for the UI approval card.
	inputStr := formatRawJSON(req.ToolCall.RawInput)
	if inputStr == "" && len(req.ToolCall.Locations) > 0 {
		paths := make([]string, 0, len(req.ToolCall.Locations))
		for _, loc := range req.ToolCall.Locations {
			if loc.Path != "" {
				paths = append(paths, loc.Path)
			}
		}
		inputStr = strings.Join(paths, "\n")
	}

	title := strings.TrimSpace(req.ToolCall.Title)
	if title == "" {
		title = req.ToolCall.Kind
	}
	if title == "" {
		title = "请求执行工具操作"
	}

	permReqID, pending := w.mgr.registerPendingPermission(turn.sessionID, string(id))
	defer w.mgr.deletePendingPermission(permReqID)

	pr := &PermissionRequest{
		RequestID:  permReqID,
		SessionID:  turn.sessionID,
		ToolCallID: req.ToolCall.ToolCallID,
		Title:      title,
		Kind:       req.ToolCall.Kind,
		Input:      inputStr,
		Options:    opts,
	}

	turn.emitSafe(ChatEvent{
		Type:              "permission_request",
		PermissionRequest: pr,
	})

	select {
	case dec := <-pending.ch:
		if dec.Cancelled || dec.OptionID == "" {
			_ = w.replyResult(id, map[string]any{
				"outcome": map[string]any{"outcome": "cancelled"},
			})
		} else {
			_ = w.replyResult(id, map[string]any{
				"outcome": map[string]any{
					"outcome":  "selected",
					"optionId": dec.OptionID,
				},
			})
		}
		turn.emitSafe(ChatEvent{
			Type:              "permission_resolved",
			PermissionRequest: pr,
		})
	case <-pending.agentCancelCh:
		turn.emitSafe(ChatEvent{
			Type:              "permission_resolved",
			PermissionRequest: pr,
		})
	case <-turn.ctx.Done():
		_ = w.replyResult(id, map[string]any{
			"outcome": map[string]any{"outcome": "cancelled"},
		})
		turn.emitSafe(ChatEvent{
			Type:              "permission_resolved",
			PermissionRequest: pr,
		})
	}
}

func pickAllowOption(opts []PermissionOption) string {
	for _, o := range opts {
		if o.Kind == "allow_once" {
			return o.OptionID
		}
	}
	for _, o := range opts {
		if o.Kind == "allow_always" {
			return o.OptionID
		}
	}
	for _, o := range opts {
		if strings.Contains(strings.ToLower(o.OptionID), "allow") || strings.Contains(strings.ToLower(o.OptionID), "proceed") {
			return o.OptionID
		}
	}
	if len(opts) > 0 {
		return opts[0].OptionID
	}
	return ""
}

func (t *acpTurnContext) emitSafe(ev ChatEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.emit(ev)
}

func (t *acpTurnContext) handleSessionUpdate(params json.RawMessage) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.suppressUpdate {
		return
	}

	var outer map[string]any
	if err := json.Unmarshal(params, &outer); err != nil {
		return
	}

	updateObj := outer
	if nested, ok := outer["update"].(map[string]any); ok {
		updateObj = nested
	}

	kind := strings.ToLower(firstString(updateObj, "sessionUpdate", "type"))
	switch kind {
	case "agent_message_chunk":
		text := extractACPContentText(updateObj["content"])
		if text == "" {
			text = firstString(updateObj, "text", "delta")
		}
		if text != "" {
			t.textBuilder.WriteString(text)
			t.emit(ChatEvent{Type: "text", Delta: text})
		}

	case "agent_thought_chunk":
		text := extractACPContentText(updateObj["content"])
		if text == "" {
			text = firstString(updateObj, "text", "delta")
		}
		if text != "" {
			t.thinkingBuilder.WriteString(text)
			t.emit(ChatEvent{Type: "thinking", Delta: text})
		}

	case "tool_call", "tool_call_update":
		t.handleToolUpdateLocked(kind, updateObj)

	case "usage_update":
		if u := extractACPUsage(updateObj); u != nil {
			t.res.Message.Usage = u
			t.emit(ChatEvent{Type: "usage", Usage: u})
		}
	}
}

func (t *acpTurnContext) handleToolUpdateLocked(kind string, obj map[string]any) {
	id := firstString(obj, "toolCallId", "id")
	if id == "" {
		id = fmt.Sprintf("tool-%d", len(t.res.Message.ToolCalls)+1)
	}

	title := firstString(obj, "title", "name")
	toolKind := firstString(obj, "kind")
	if title == "" {
		title = toolKind
	}

	rawStatus := strings.ToLower(firstString(obj, "status"))
	status := ""
	switch rawStatus {
	case "completed", "done", "success":
		status = "done"
	case "failed", "error", "cancelled", "rejected":
		status = "error"
	case "pending", "in_progress", "running", "waiting":
		status = "running"
	}

	input := ""
	if rawIn, ok := obj["rawInput"]; ok && rawIn != nil {
		input = acpStringify(rawIn)
	}

	output := ""
	if rawOut, ok := obj["rawOutput"]; ok && rawOut != nil {
		output = acpStringify(rawOut)
	}
	if contentText := extractACPToolContent(obj["content"]); contentText != "" {
		if output == "" {
			output = contentText
		} else if !strings.Contains(output, contentText) {
			output = output + "\n" + contentText
		}
	}

	idx, exists := t.toolMap[id]
	if !exists {
		if title == "" {
			title = "Tool"
		}
		if status == "" {
			status = "running"
		}
		tc := session.ToolCall{
			ID:     id,
			Name:   title,
			Input:  input,
			Output: output,
			Status: status,
		}
		t.toolMap[id] = len(t.res.Message.ToolCalls)
		t.res.Message.ToolCalls = append(t.res.Message.ToolCalls, tc)
		evType := "tool_call"
		if status != "running" {
			evType = "tool_result"
		}
		t.emit(ChatEvent{Type: evType, ToolCall: &tc})
		return
	}

	existing := &t.res.Message.ToolCalls[idx]
	if title != "" {
		existing.Name = title
	}
	if input != "" {
		existing.Input = input
	}
	if output != "" {
		existing.Output = output
	}
	if status != "" {
		existing.Status = status
	}
	tcCopy := *existing
	evType := "tool_call"
	if tcCopy.Status != "running" || kind == "tool_call_update" {
		evType = "tool_result"
	}
	t.emit(ChatEvent{Type: evType, ToolCall: &tcCopy})
}

func extractACPContentText(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		if s, ok := val["text"].(string); ok {
			return s
		}
		if inner, ok := val["content"]; ok {
			return extractACPContentText(inner)
		}
	case []any:
		var sb strings.Builder
		for _, item := range val {
			sb.WriteString(extractACPContentText(item))
		}
		return sb.String()
	}
	return ""
}

func extractACPToolContent(v any) string {
	arr, ok := v.([]any)
	if !ok {
		return extractACPContentText(v)
	}
	var parts []string
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			if s := extractACPContentText(item); s != "" {
				parts = append(parts, s)
			}
			continue
		}
		typ := strings.ToLower(firstString(m, "type"))
		switch typ {
		case "diff":
			path := firstString(m, "path")
			newText := firstString(m, "newText")
			if path != "" {
				if newText != "" {
					parts = append(parts, fmt.Sprintf("[diff] %s\n%s", path, newText))
				} else {
					parts = append(parts, fmt.Sprintf("[diff] %s", path))
				}
			}
		case "content":
			if s := extractACPContentText(m["content"]); s != "" {
				parts = append(parts, s)
			}
		default:
			if s := extractACPContentText(m); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func extractACPUsage(obj map[string]any) *session.TokenUsage {
	src := obj
	if nested, ok := obj["usage"].(map[string]any); ok {
		src = nested
	}
	inTok := acpFirstInt(src, "inputTokens", "input_tokens", "promptTokens")
	outTok := acpFirstInt(src, "outputTokens", "output_tokens", "completionTokens")
	totTok := acpFirstInt(src, "totalTokens", "total_tokens")
	if totTok == 0 && (inTok > 0 || outTok > 0) {
		totTok = inTok + outTok
	}
	if inTok == 0 && outTok == 0 && totTok == 0 {
		return nil
	}
	return &session.TokenUsage{
		InputTokens:  inTok,
		OutputTokens: outTok,
		TotalTokens:  totTok,
	}
}

func acpFirstInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch n := v.(type) {
			case float64:
				return int(n)
			case int:
				return n
			case int64:
				return int(n)
			}
		}
	}
	return 0
}

func acpStringify(v any) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		b, err := json.MarshalIndent(val, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

func formatRawJSON(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "{}" {
		return ""
	}
	var obj any
	if err := json.Unmarshal(raw, &obj); err == nil {
		if m, ok := obj.(map[string]any); ok {
			if cmd, ok := m["command"].(string); ok && cmd != "" {
				return cmd
			}
		}
		if b, err := json.MarshalIndent(obj, "", "  "); err == nil {
			return string(b)
		}
	}
	return trimmed
}

func (m *Manager) registerPendingPermission(sessionID, agentReqID string) (string, *pendingPermission) {
	id := fmt.Sprintf("perm-%d", m.permSeq.Add(1))
	p := &pendingPermission{
		sessionID:     sessionID,
		agentReqID:    agentReqID,
		ch:            make(chan PermissionDecision, 1),
		agentCancelCh: make(chan struct{}),
	}
	m.permsMu.Lock()
	m.pendingPerms[id] = p
	m.permsMu.Unlock()
	return id, p
}

func (m *Manager) deletePendingPermission(requestID string) {
	m.permsMu.Lock()
	delete(m.pendingPerms, requestID)
	m.permsMu.Unlock()
}

func (m *Manager) cancelPermissionByAgentID(agentReqID string) {
	m.permsMu.Lock()
	var target *pendingPermission
	for _, p := range m.pendingPerms {
		if p.agentReqID == agentReqID {
			target = p
			break
		}
	}
	m.permsMu.Unlock()
	if target != nil {
		target.cancelByAgent()
	}
}

// ResolvePermission delivers the user's approval/rejection choice to the waiting ACP turn.
func (m *Manager) ResolvePermission(sessionID, requestID string, decision PermissionDecision) error {
	m.permsMu.Lock()
	p, ok := m.pendingPerms[requestID]
	if ok && sessionID != "" && p.sessionID != sessionID {
		m.permsMu.Unlock()
		return errors.New("权限请求与当前会话不匹配")
	}
	m.permsMu.Unlock()
	if !ok {
		return errors.New("权限请求已过期或已处理")
	}
	select {
	case p.ch <- decision:
		return nil
	default:
		return errors.New("权限请求已被处理")
	}
}

func acpWorkerSignature(bin, cwd, httpProxy, httpsProxy string) string {
	return strings.Join([]string{bin, cwd, httpProxy, httpsProxy}, "\x00")
}

func (m *Manager) spawnACPWorker(ctx context.Context, sessionID, bin, cwd, sig string, pooled bool) (*acpWorker, error) {
	cmd := exec.Command(bin, "acp")
	cmd.Dir = cwd
	cmd.Env = m.BuildEnv("NO_COLOR=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("启动 agy acp 失败: %w", err)
	}

	w := &acpWorker{
		mgr:        m,
		sessionID:  sessionID,
		signature:  sig,
		cwd:        cwd,
		cmd:        cmd,
		stdin:      stdin,
		pendingRPC: make(map[int64]chan rpcResponse),
		eofCh:      make(chan struct{}),
	}

	if pooled {
		w.onClose = func() {
			m.acpPool.mu.Lock()
			if cur, ok := m.acpPool.workers[sessionID]; ok && cur == w {
				delete(m.acpPool.workers, sessionID)
			}
			m.acpPool.mu.Unlock()
		}
	}

	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := stderr.Read(buf)
			if n > 0 {
				w.stderrMu.Lock()
				if w.stderrBuf.Len() < 64*1024 {
					w.stderrBuf.Write(buf[:n])
				}
				w.stderrMu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}()

	go w.readLoop(stdout)

	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err = w.call(initCtx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{
				"readTextFile":  false,
				"writeTextFile": false,
			},
			"terminal": false,
		},
		"clientInfo": map[string]any{
			"name":    acpClientName,
			"title":   acpClientTitle,
			"version": acpClientVersion,
		},
	})
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("ACP initialize 失败: %w", err)
	}

	return w, nil
}

func (m *Manager) getOrSpawnACPWorker(ctx context.Context, req ChatRequest, bin, cwd string, pooled bool) (*acpWorker, error) {
	cfg := m.cfgStore.Get()
	sig := acpWorkerSignature(bin, cwd, cfg.HTTPProxy, cfg.HTTPSProxy)

	if pooled && req.SessionID != "" {
		m.acpPool.mu.Lock()
		existing, ok := m.acpPool.workers[req.SessionID]
		if ok {
			if existing.isClosed() || existing.signature != sig {
				delete(m.acpPool.workers, req.SessionID)
				m.acpPool.mu.Unlock()
				existing.Close()
			} else {
				existing.stopIdleTimer()
				m.acpPool.mu.Unlock()
				return existing, nil
			}
		} else {
			m.acpPool.mu.Unlock()
		}
	}

	w, err := m.spawnACPWorker(ctx, req.SessionID, bin, cwd, sig, pooled)
	if err != nil {
		return nil, err
	}

	if pooled && req.SessionID != "" {
		m.acpPool.mu.Lock()
		if old, exists := m.acpPool.workers[req.SessionID]; exists && old != w {
			go old.Close()
		}
		m.acpPool.workers[req.SessionID] = w
		m.acpPool.mu.Unlock()
	}

	return w, nil
}

func (w *acpWorker) ensureACPSession(ctx context.Context, req ChatRequest, turn *acpTurnContext) error {
	if w.acpSessionID != "" {
		if w.acpSessionID != turn.res.ConversationID {
			turn.res.ConversationID = w.acpSessionID
			turn.emitSafe(ChatEvent{Type: "meta", ConversationID: w.acpSessionID})
		}
		return nil
	}

	if req.ConversationID != "" {
		// Prefer session/resume (no history replay); fall back to session/load (suppressing replay).
		if raw, err := w.call(ctx, "session/resume", map[string]any{
			"sessionId":  req.ConversationID,
			"cwd":        w.cwd,
			"mcpServers": []any{},
		}); err == nil {
			w.parseSessionMetadata(raw)
			w.acpSessionID = req.ConversationID
			turn.res.ConversationID = w.acpSessionID
			turn.emitSafe(ChatEvent{Type: "meta", ConversationID: w.acpSessionID})
			return nil
		}

		turn.mu.Lock()
		turn.suppressUpdate = true
		turn.mu.Unlock()
		raw, err := w.call(ctx, "session/load", map[string]any{
			"sessionId":  req.ConversationID,
			"cwd":        w.cwd,
			"mcpServers": []any{},
		})
		turn.mu.Lock()
		turn.suppressUpdate = false
		turn.mu.Unlock()
		if err == nil {
			w.parseSessionMetadata(raw)
			w.acpSessionID = req.ConversationID
			turn.res.ConversationID = w.acpSessionID
			turn.emitSafe(ChatEvent{Type: "meta", ConversationID: w.acpSessionID})
			return nil
		}
	}

	raw, err := w.call(ctx, "session/new", map[string]any{
		"cwd":        w.cwd,
		"mcpServers": []any{},
	})
	if err != nil {
		return fmt.Errorf("ACP session/new 失败: %w", err)
	}

	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.SessionID == "" {
		return fmt.Errorf("ACP session/new 未返回有效的 sessionId")
	}

	w.parseSessionMetadata(raw)
	w.acpSessionID = resp.SessionID
	turn.res.ConversationID = w.acpSessionID
	turn.emitSafe(ChatEvent{Type: "meta", ConversationID: w.acpSessionID})
	return nil
}

func (w *acpWorker) parseSessionMetadata(raw json.RawMessage) {
	var meta struct {
		Modes struct {
			AvailableModes []acpModeInfo `json:"availableModes"`
		} `json:"modes"`
		ConfigOptions []acpConfigOption `json:"configOptions"`
	}
	if json.Unmarshal(raw, &meta) != nil {
		return
	}
	if len(meta.Modes.AvailableModes) > 0 {
		w.availableModes = meta.Modes.AvailableModes
	}
	if len(meta.ConfigOptions) > 0 {
		w.configOptions = meta.ConfigOptions
	}
}

func (w *acpWorker) syncTurnOptions(ctx context.Context, req ChatRequest, allowBypass bool) {
	// 1. Permission / execution mode
	targetMode := w.resolveModeID(req.PermissionMode, allowBypass)
	if targetMode != "" && targetMode != w.lastModeSent {
		if _, err := w.call(ctx, "session/set_mode", map[string]any{
			"sessionId": w.acpSessionID,
			"modeId":    targetMode,
		}); err == nil {
			w.lastModeSent = targetMode
		}
	}

	// 2. Model
	if req.Model != "" && req.Model != w.lastModelSent {
		if _, err := w.call(ctx, "session/set_model", map[string]any{
			"sessionId": w.acpSessionID,
			"modelId":   req.Model,
		}); err == nil {
			w.lastModelSent = req.Model
		}
	}

	// 3. Thinking / effort config option (if advertised by agent)
	if req.Effort != "" && req.Effort != w.lastEffortSent {
		for _, opt := range w.configOptions {
			lower := strings.ToLower(opt.ID + " " + opt.Name + " " + opt.Category)
			if strings.Contains(lower, "effort") || strings.Contains(lower, "thinking") || strings.Contains(lower, "reasoning") {
				if _, err := w.call(ctx, "session/set_config_option", map[string]any{
					"sessionId": w.acpSessionID,
					"configId":  opt.ID,
					"value":     req.Effort,
				}); err == nil {
					w.lastEffortSent = req.Effort
				}
				break
			}
		}
	}
}

func (w *acpWorker) resolveModeID(requested string, allowBypass bool) string {
	want := strings.TrimSpace(requested)
	if want == "" {
		want = "default"
	}
	if want == "bypass" && !allowBypass {
		want = "default"
	}

	candidates := map[string][]string{
		"default":      {"default", "request-review", "ask"},
		"accept-edits": {"accept-edits", "accept_edits", "auto"},
		"plan":         {"plan"},
		"bypass":       {"bypass", "bypassPermissions", "yolo", "always-proceed", "full"},
	}[want]

	if len(w.availableModes) > 0 {
		for _, c := range candidates {
			for _, avail := range w.availableModes {
				if strings.EqualFold(avail.ID, c) {
					return avail.ID
				}
			}
		}
		return ""
	}

	// Fallback when agent didn't advertise availableModes
	if want == "bypass" {
		return ""
	}
	return want
}

func (m *Manager) runACPTurn(
	ctx context.Context,
	req ChatRequest,
	bin, cwd string,
	idleTimeout time.Duration,
	emit func(ChatEvent),
) (*ChatResult, error) {
	pooled := idleTimeout > 0 && req.SessionID != ""
	w, err := m.getOrSpawnACPWorker(ctx, req, bin, cwd, pooled)
	if err != nil {
		return nil, err
	}

	w.turnMu.Lock()
	defer w.turnMu.Unlock()

	if !pooled {
		defer w.Close()
	} else {
		w.stopIdleTimer()
		defer func() {
			if !w.isClosed() {
				w.resetIdleTimer(idleTimeout)
			}
		}()
	}

	cfg := m.cfgStore.Get()
	res := &ChatResult{
		ConversationID: req.ConversationID,
		Message: session.Message{
			Role:      "assistant",
			CreatedAt: time.Now(),
		},
	}

	turn := &acpTurnContext{
		ctx:            ctx,
		sessionID:      req.SessionID,
		permissionMode: req.PermissionMode,
		allowBypass:    cfg.AllowDangerousSkip,
		res:            res,
		emit:           emit,
		toolMap:        make(map[string]int),
	}

	w.activeMu.Lock()
	w.activeTurn = turn
	w.activeMu.Unlock()
	defer func() {
		w.activeMu.Lock()
		if w.activeTurn == turn {
			w.activeTurn = nil
		}
		w.activeMu.Unlock()
	}()

	if err := w.ensureACPSession(ctx, req, turn); err != nil {
		w.Close()
		return nil, err
	}

	w.syncTurnOptions(ctx, req, cfg.AllowDangerousSkip)

	// Watch for turn cancellation and send ACP session/cancel notification cleanly.
	promptDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = w.notify("session/cancel", map[string]any{
				"sessionId": w.acpSessionID,
			})
		case <-promptDone:
		}
	}()

	rawResp, promptErr := w.call(ctx, "session/prompt", map[string]any{
		"sessionId": w.acpSessionID,
		"prompt": []map[string]any{
			{
				"type": "text",
				"text": req.Prompt,
			},
		},
	})
	close(promptDone)

	turn.mu.Lock()
	res.Message.Content = turn.textBuilder.String()
	res.Message.Thinking = turn.thinkingBuilder.String()
	turn.mu.Unlock()

	if ctx.Err() != nil {
		if res.Message.Error == "" {
			res.Message.Error = "已停止生成"
		}
		return res, ctx.Err()
	}

	if promptErr != nil {
		errText := promptErr.Error()
		if res.Message.Error == "" {
			res.Message.Error = errText
		}
		turn.emitSafe(ChatEvent{Type: "error", Error: res.Message.Error})
		// If the process died or errored fatally, drop the worker so next turn starts fresh.
		if w.isClosed() {
			w.Close()
		}
	} else if len(rawResp) > 0 {
		var pr map[string]any
		if json.Unmarshal(rawResp, &pr) == nil {
			if u := extractACPUsage(pr); u != nil {
				res.Message.Usage = u
				turn.emitSafe(ChatEvent{Type: "usage", Usage: u})
			}
		}
	}

	turn.emitSafe(ChatEvent{Type: "done", ConversationID: res.ConversationID, Usage: res.Message.Usage})
	return res, nil
}
