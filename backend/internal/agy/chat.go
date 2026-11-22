package agy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

type ChatRequest struct {
	SessionID      string
	Prompt         string
	ConversationID string
	Workspace      string
	ExtraDirs      []string
	Model          string
	Effort         string
	PermissionMode string
}

type PermissionOption struct {
	OptionID string `json:"option_id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // allow_once | allow_always | reject_once | reject_always
}

type PermissionRequest struct {
	RequestID  string             `json:"request_id"`
	SessionID  string             `json:"session_id"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	Title      string             `json:"title"`
	Kind       string             `json:"kind,omitempty"`
	Input      string             `json:"input,omitempty"`
	Options    []PermissionOption `json:"options"`
}

type PermissionDecision struct {
	OptionID  string
	Cancelled bool
}

// ChatEvent is a normalized SSE event sent to the frontend during a chat turn.
type ChatEvent struct {
	Type              string              `json:"type"` // meta | thinking | text | tool_call | tool_result | permission_request | permission_resolved | usage | error | done
	ConversationID    string              `json:"conversation_id,omitempty"`
	Delta             string              `json:"delta,omitempty"`
	ToolCall          *session.ToolCall   `json:"tool_call,omitempty"`
	PermissionRequest *PermissionRequest  `json:"permission_request,omitempty"`
	Usage             *session.TokenUsage `json:"usage,omitempty"`
	Error             string              `json:"error,omitempty"`
}

type ChatResult struct {
	ConversationID string
	Message        session.Message
}

// RunChat executes a turn via the official `agy acp` JSON-RPC 2.0 server.
func (m *Manager) RunChat(ctx context.Context, req ChatRequest, emit func(ChatEvent)) (*ChatResult, error) {
	bin := m.ResolveBinary()
	if bin == "" {
		return nil, errors.New("未检测到 agy 可执行文件，请先前往「设置中心」一键安装 Antigravity CLI")
	}

	cfg := m.cfgStore.Get()
	cwd := req.Workspace
	if cwd == "" {
		cwd = cfg.DefaultWorkspace
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		cwd = m.cfgStore.Paths().HomeDir
	}

	idleTimeout := time.Duration(cfg.PersistentIdleMinutes) * time.Minute
	return m.runACPTurn(ctx, req, bin, cwd, idleTimeout, emit)
}

// runLegacyStreamJSONChat is retained only as an internal reference; Web Chat uses ACP.
func (m *Manager) runLegacyStreamJSONChat(ctx context.Context, req ChatRequest, bin, cwd string, emit func(ChatEvent)) (*ChatResult, error) {
	cfg := m.cfgStore.Get()
	args := []string{"-p", req.Prompt, "--output-format", "stream-json"}

	if req.ConversationID != "" {
		args = append(args, "--conversation", req.ConversationID)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" {
		args = append(args, "--effort", req.Effort)
	}
	for _, dir := range req.ExtraDirs {
		dir = strings.TrimSpace(dir)
		if dir != "" {
			args = append(args, "--add-dir", dir)
		}
	}

	switch req.PermissionMode {
	case "accept-edits":
		args = append(args, "--mode", "accept-edits")
	case "plan":
		args = append(args, "--mode", "plan")
	case "bypass":
		if cfg.AllowDangerousSkip {
			args = append(args, "--dangerously-skip-permissions")
		}
	}

	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = m.BuildEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start agy: %w", err)
	}

	// Ensure the entire process group is terminated when ctx is cancelled.
	doneWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			terminateProcessGroup(cmd.Process.Pid)
		case <-doneWatch:
		}
	}()
	defer close(doneWatch)

	var stderrBuf strings.Builder
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderrBuf, io.LimitReader(stderr, 64*1024))
	}()

	res := &ChatResult{
		ConversationID: req.ConversationID,
		Message: session.Message{
			Role:      "assistant",
			CreatedAt: time.Now(),
		},
	}

	parser := newStreamParser(res, emit)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		parser.ProcessLine(scanner.Text())
	}

	waitErr := cmd.Wait()
	<-stderrDone

	res.Message.Content = parser.textBuilder.String()
	res.Message.Thinking = parser.thinkingBuilder.String()

	if ctx.Err() != nil {
		if res.Message.Error == "" {
			res.Message.Error = "已停止生成"
		}
		return res, ctx.Err()
	}

	if waitErr != nil {
		errText := strings.TrimSpace(stderrBuf.String())
		if errText == "" {
			errText = waitErr.Error()
		}
		if res.Message.Error == "" {
			res.Message.Error = errText
		}
		emit(ChatEvent{Type: "error", Error: res.Message.Error})
	}

	emit(ChatEvent{Type: "done", ConversationID: res.ConversationID, Usage: res.Message.Usage})
	return res, nil
}

type streamParser struct {
	res             *ChatResult
	emit            func(ChatEvent)
	textBuilder     strings.Builder
	thinkingBuilder strings.Builder
	toolMap         map[string]int
}

func newStreamParser(res *ChatResult, emit func(ChatEvent)) *streamParser {
	return &streamParser{
		res:     res,
		emit:    emit,
		toolMap: make(map[string]int),
	}
}

func (p *streamParser) setConversationID(cid string) {
	if cid != "" && cid != p.res.ConversationID {
		p.res.ConversationID = cid
		p.emit(ChatEvent{Type: "meta", ConversationID: cid})
	}
}

func (p *streamParser) upsertTool(tc session.ToolCall) {
	if tc.ID == "" {
		tc.ID = fmt.Sprintf("tool-%d", len(p.res.Message.ToolCalls)+1)
	}
	if idx, exists := p.toolMap[tc.ID]; exists {
		existing := &p.res.Message.ToolCalls[idx]
		if tc.Name != "" {
			existing.Name = tc.Name
		}
		if tc.Input != "" {
			existing.Input = tc.Input
		}
		if tc.Output != "" {
			existing.Output = tc.Output
		}
		if tc.Status != "" {
			existing.Status = tc.Status
		}
		copyTC := *existing
		p.emit(ChatEvent{Type: "tool_call", ToolCall: &copyTC})
		return
	}
	if tc.Status == "" {
		tc.Status = "running"
	}
	p.toolMap[tc.ID] = len(p.res.Message.ToolCalls)
	p.res.Message.ToolCalls = append(p.res.Message.ToolCalls, tc)
	copyTC := tc
	p.emit(ChatEvent{Type: "tool_call", ToolCall: &copyTC})
}

func (p *streamParser) ProcessLine(rawLine string) {
	line := strings.TrimSpace(rawLine)
	if line == "" {
		return
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		p.textBuilder.WriteString(line + "\n")
		p.emit(ChatEvent{Type: "text", Delta: line + "\n"})
		return
	}

	// Capture top-level conversation_id (e.g. on `init` event)
	p.setConversationID(firstString(raw, "conversation_id", "conversationId", "session_id", "sessionId"))

	evType := strings.ToLower(firstString(raw, "event", "type"))
	switch evType {
	case "init":
		if initObj, ok := raw["init"].(map[string]any); ok {
			p.setConversationID(firstString(initObj, "conversation_id", "conversationId"))
		}

	case "step_update":
		// Official agy v1.1+ nested envelope: {"event":"step_update","step_update":{...}}
		su, _ := raw["step_update"].(map[string]any)
		if su == nil {
			su = raw
		}
		p.handleStepUpdate(su)

	case "result", "done", "complete", "stats":
		// Official agy v1.1+ nested envelope: {"event":"result","result":{...}}
		resObj, _ := raw["result"].(map[string]any)
		if resObj == nil {
			resObj = raw
		}
		p.handleResult(resObj)

	case "thinking", "thought", "reasoning", "thinking_delta":
		if delta := extractTextField(raw); delta != "" {
			p.thinkingBuilder.WriteString(delta)
			p.emit(ChatEvent{Type: "thinking", Delta: delta})
		}

	case "text", "text_delta", "content_delta", "delta", "chunk":
		if delta := extractTextField(raw); delta != "" {
			p.textBuilder.WriteString(delta)
			p.emit(ChatEvent{Type: "text", Delta: delta})
		}

	case "assistant", "message":
		p.handleAssistantPayload(raw)

	case "tool_use", "tool_call":
		p.upsertTool(session.ToolCall{
			ID:     firstString(raw, "id", "tool_call_id", "call_id"),
			Name:   firstString(raw, "name", "tool", "tool_name"),
			Input:  stringifyField(raw["input"], raw["arguments"], raw["args"], raw["parameters"]),
			Status: "running",
		})

	case "tool_result":
		status := "done"
		if isErr, ok := raw["is_error"].(bool); ok && isErr {
			status = "error"
		}
		p.upsertTool(session.ToolCall{
			ID:     firstString(raw, "tool_use_id", "tool_call_id", "call_id", "id"),
			Name:   firstString(raw, "name", "tool", "tool_name"),
			Output: stringifyField(raw["output"], raw["content"], raw["result"]),
			Status: status,
		})

	case "error":
		errMsg := firstString(raw, "error", "message", "detail")
		if errMsg == "" {
			errMsg = line
		}
		p.res.Message.Error = errMsg
		p.emit(ChatEvent{Type: "error", Error: errMsg})

	default:
		if u := extractUsage(raw); u != nil {
			p.res.Message.Usage = u
			p.emit(ChatEvent{Type: "usage", Usage: u})
		}
		if delta := extractTextField(raw); delta != "" {
			p.textBuilder.WriteString(delta)
			p.emit(ChatEvent{Type: "text", Delta: delta})
		}
	}
}

func (p *streamParser) handleStepUpdate(su map[string]any) {
	p.setConversationID(firstString(su, "conversation_id", "conversationId"))

	if u := extractUsage(su); u != nil {
		p.res.Message.Usage = u
		p.emit(ChatEvent{Type: "usage", Usage: u})
	}

	stepType := strings.ToLower(firstString(su, "step_type", "type"))
	state := strings.ToUpper(firstString(su, "state"))

	// Thinking delta (if emitted on thinking/reasoning steps)
	if thinking := firstString(su, "thinking_delta", "thinking", "thought"); thinking != "" {
		p.thinkingBuilder.WriteString(thinking)
		p.emit(ChatEvent{Type: "thinking", Delta: thinking})
	}

	// Incremental agent response text
	if delta := firstString(su, "text_delta", "delta", "text"); delta != "" {
		if stepType == "thinking" || stepType == "thought" || stepType == "reasoning" {
			p.thinkingBuilder.WriteString(delta)
			p.emit(ChatEvent{Type: "thinking", Delta: delta})
		} else {
			p.textBuilder.WriteString(delta)
			p.emit(ChatEvent{Type: "text", Delta: delta})
		}
	}

	// Tool call step
	if stepType == "tool" || su["tool_info"] != nil || firstString(su, "tool_name") != "" {
		stepIdx := intFromMap(su, "step_index")
		toolID := fmt.Sprintf("step-%d", stepIdx)
		toolName := firstString(su, "tool_name")
		var inputStr, outputStr string
		status := "running"
		if state == "DONE" {
			status = "done"
		}

		if info, ok := su["tool_info"].(map[string]any); ok {
			if n := firstString(info, "name"); n != "" {
				toolName = n
			}
			inputStr = stringifyField(info["parameters"], info["input"], info["arguments"])
			outputStr = stringifyField(info["output"], info["result"])
			if errObj, hasErr := info["error"]; hasErr && errObj != nil {
				status = "error"
				if errStr := stringifyField(errObj); errStr != "" {
					if outputStr != "" {
						outputStr += "\n" + errStr
					} else {
						outputStr = errStr
					}
				}
			}
		}

		p.upsertTool(session.ToolCall{
			ID:     toolID,
			Name:   toolName,
			Input:  inputStr,
			Output: outputStr,
			Status: status,
		})
	}
}

func (p *streamParser) handleResult(resObj map[string]any) {
	p.setConversationID(firstString(resObj, "conversation_id", "conversationId"))

	if u := extractUsage(resObj); u != nil {
		p.res.Message.Usage = u
		p.emit(ChatEvent{Type: "usage", Usage: u})
	}

	// If no streaming text_delta arrived (or response only present in terminal result), use result.response
	if p.textBuilder.Len() == 0 {
		if finalTxt := firstString(resObj, "response", "result", "text", "content", "output"); finalTxt != "" {
			p.textBuilder.WriteString(finalTxt)
			p.emit(ChatEvent{Type: "text", Delta: finalTxt})
		}
	}

	status := strings.ToUpper(firstString(resObj, "status"))
	if errMsg := firstString(resObj, "error"); errMsg != "" {
		p.res.Message.Error = errMsg
		p.emit(ChatEvent{Type: "error", Error: errMsg})
	} else if status != "" && status != "SUCCESS" {
		p.res.Message.Error = fmt.Sprintf("执行结束状态: %s", status)
		p.emit(ChatEvent{Type: "error", Error: p.res.Message.Error})
	}
}

func (p *streamParser) handleAssistantPayload(raw map[string]any) {
	msgObj, _ := raw["message"].(map[string]any)
	if msgObj == nil {
		msgObj = raw
	}

	if contentStr, ok := msgObj["content"].(string); ok && contentStr != "" {
		p.textBuilder.WriteString(contentStr)
		p.emit(ChatEvent{Type: "text", Delta: contentStr})
		return
	}

	blocks, ok := msgObj["content"].([]any)
	if !ok {
		if delta := extractTextField(msgObj); delta != "" {
			p.textBuilder.WriteString(delta)
			p.emit(ChatEvent{Type: "text", Delta: delta})
		}
		return
	}

	for _, b := range blocks {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		bType := strings.ToLower(firstString(bm, "type"))
		switch bType {
		case "thinking", "thought":
			if t := firstString(bm, "thinking", "text", "content"); t != "" {
				p.thinkingBuilder.WriteString(t)
				p.emit(ChatEvent{Type: "thinking", Delta: t})
			}
		case "text":
			if t := firstString(bm, "text", "content"); t != "" {
				p.textBuilder.WriteString(t)
				p.emit(ChatEvent{Type: "text", Delta: t})
			}
		case "tool_use", "tool_call":
			p.upsertTool(session.ToolCall{
				ID:     firstString(bm, "id", "call_id"),
				Name:   firstString(bm, "name", "tool"),
				Input:  stringifyField(bm["input"], bm["arguments"]),
				Status: "running",
			})
		}
	}
}

func extractTextField(m map[string]any) string {
	if s := firstString(m, "text_delta", "delta", "text", "content", "response", "thinking", "thought"); s != "" {
		return s
	}
	if deltaObj, ok := m["delta"].(map[string]any); ok {
		return firstString(deltaObj, "text", "thinking", "content")
	}
	return ""
}

func extractUsage(m map[string]any) *session.TokenUsage {
	uObj, ok := m["usage"].(map[string]any)
	if !ok {
		if stats, ok2 := m["stats"].(map[string]any); ok2 {
			uObj = stats
		} else {
			return nil
		}
	}
	in := intFromMap(uObj, "input_tokens", "prompt_tokens", "inputTokens")
	out := intFromMap(uObj, "output_tokens", "completion_tokens", "outputTokens")
	total := intFromMap(uObj, "total_tokens", "totalTokens")
	if total == 0 {
		total = in + out
	}
	if in == 0 && out == 0 && total == 0 {
		return nil
	}
	return &session.TokenUsage{
		InputTokens:  in,
		OutputTokens: out,
		TotalTokens:  total,
	}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok2 := v.(string); ok2 && s != "" {
				return s
			}
		}
	}
	return ""
}

func intFromMap(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch n := v.(type) {
			case float64:
				return int(n)
			case int:
				return n
			}
		}
	}
	return 0
}

func stringifyField(vals ...any) string {
	for _, v := range vals {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok {
			if s != "" {
				return s
			}
			continue
		}
		b, err := json.MarshalIndent(v, "", "  ")
		if err == nil && len(b) > 0 && string(b) != "null" {
			return string(b)
		}
	}
	return ""
}

func terminateProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(800 * time.Millisecond)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
