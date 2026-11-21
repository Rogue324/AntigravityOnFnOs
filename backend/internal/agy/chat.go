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
	Prompt         string
	ConversationID string
	Workspace      string
	ExtraDirs      []string
	Model          string
	PermissionMode string
}

// ChatEvent is a normalized SSE event sent to the frontend during a chat turn.
type ChatEvent struct {
	Type           string              `json:"type"` // meta | thinking | text | tool_call | tool_result | usage | error | done
	ConversationID string              `json:"conversation_id,omitempty"`
	Delta          string              `json:"delta,omitempty"`
	ToolCall       *session.ToolCall   `json:"tool_call,omitempty"`
	Usage          *session.TokenUsage `json:"usage,omitempty"`
	Error          string              `json:"error,omitempty"`
}

type ChatResult struct {
	ConversationID string
	Message        session.Message
}

// RunChat executes `agy -p ... --output-format stream-json` and streams normalized events.
func (m *Manager) RunChat(ctx context.Context, req ChatRequest, emit func(ChatEvent)) (*ChatResult, error) {
	bin := m.ResolveBinary()
	if bin == "" {
		return nil, errors.New("未检测到 agy 可执行文件，请先前往「设置中心」一键安装 Antigravity CLI")
	}

	cfg := m.cfgStore.Get()
	args := []string{"-p", req.Prompt, "--output-format", "stream-json"}

	if req.ConversationID != "" {
		args = append(args, "--conversation", req.ConversationID)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	for _, dir := range req.ExtraDirs {
		dir = strings.TrimSpace(dir)
		if dir != "" {
			args = append(args, "--add-dir", dir)
		}
	}

	switch req.PermissionMode {
	case "accept-edits":
		args = append(args, "--permission-mode", "accept-edits")
	case "plan":
		args = append(args, "--permission-mode", "plan")
	case "bypass":
		if cfg.AllowDangerousSkip {
			args = append(args, "--dangerously-skip-permissions")
		}
	}

	cwd := req.Workspace
	if cwd == "" {
		cwd = cfg.DefaultWorkspace
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		cwd = m.cfgStore.Paths().HomeDir
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

	var textBuilder strings.Builder
	var thinkingBuilder strings.Builder
	toolMap := make(map[string]int)

	upsertTool := func(tc session.ToolCall) {
		if tc.ID == "" {
			tc.ID = fmt.Sprintf("tool-%d", len(res.Message.ToolCalls)+1)
		}
		if idx, exists := toolMap[tc.ID]; exists {
			existing := &res.Message.ToolCalls[idx]
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
			emit(ChatEvent{Type: "tool_call", ToolCall: &copyTC})
			return
		}
		if tc.Status == "" {
			tc.Status = "running"
		}
		toolMap[tc.ID] = len(res.Message.ToolCalls)
		res.Message.ToolCalls = append(res.Message.ToolCalls, tc)
		copyTC := tc
		emit(ChatEvent{Type: "tool_call", ToolCall: &copyTC})
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			// Graceful fallback for non-JSON output lines
			textBuilder.WriteString(line + "\n")
			emit(ChatEvent{Type: "text", Delta: line + "\n"})
			continue
		}

		// Extract conversation/session id if present
		if cid := firstString(raw, "conversation_id", "conversationId", "session_id", "sessionId"); cid != "" && cid != res.ConversationID {
			res.ConversationID = cid
			emit(ChatEvent{Type: "meta", ConversationID: cid})
		}

		evType := strings.ToLower(firstString(raw, "type", "event"))
		switch evType {
		case "thinking", "thought", "reasoning", "thinking_delta":
			if delta := extractTextField(raw); delta != "" {
				thinkingBuilder.WriteString(delta)
				emit(ChatEvent{Type: "thinking", Delta: delta})
			}

		case "text", "text_delta", "content_delta", "delta", "chunk":
			if delta := extractTextField(raw); delta != "" {
				textBuilder.WriteString(delta)
				emit(ChatEvent{Type: "text", Delta: delta})
			}

		case "assistant", "message":
			handleAssistantPayload(raw, &textBuilder, &thinkingBuilder, upsertTool, emit)

		case "tool_use", "tool_call":
			tc := session.ToolCall{
				ID:     firstString(raw, "id", "tool_call_id", "call_id"),
				Name:   firstString(raw, "name", "tool", "tool_name"),
				Input:  stringifyField(raw["input"], raw["arguments"], raw["args"]),
				Status: "running",
			}
			upsertTool(tc)

		case "tool_result":
			status := "done"
			if isErr, ok := raw["is_error"].(bool); ok && isErr {
				status = "error"
			}
			tc := session.ToolCall{
				ID:     firstString(raw, "tool_use_id", "tool_call_id", "call_id", "id"),
				Name:   firstString(raw, "name", "tool", "tool_name"),
				Output: stringifyField(raw["output"], raw["content"], raw["result"]),
				Status: status,
			}
			upsertTool(tc)

		case "result", "done", "complete", "stats":
			if u := extractUsage(raw); u != nil {
				res.Message.Usage = u
				emit(ChatEvent{Type: "usage", Usage: u})
			}
			// Some CLI versions only send final result text inside `result`
			if textBuilder.Len() == 0 {
				if finalTxt := firstString(raw, "result", "text", "content", "output"); finalTxt != "" {
					textBuilder.WriteString(finalTxt)
					emit(ChatEvent{Type: "text", Delta: finalTxt})
				}
			}

		case "error":
			errMsg := firstString(raw, "error", "message", "detail")
			if errMsg == "" {
				errMsg = line
			}
			res.Message.Error = errMsg
			emit(ChatEvent{Type: "error", Error: errMsg})

		default:
			// Check for nested usage or text even in unrecognized event types
			if u := extractUsage(raw); u != nil {
				res.Message.Usage = u
				emit(ChatEvent{Type: "usage", Usage: u})
			}
			if delta := extractTextField(raw); delta != "" {
				textBuilder.WriteString(delta)
				emit(ChatEvent{Type: "text", Delta: delta})
			}
		}
	}

	waitErr := cmd.Wait()
	<-stderrDone

	res.Message.Content = textBuilder.String()
	res.Message.Thinking = thinkingBuilder.String()

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

func handleAssistantPayload(
	raw map[string]any,
	textBuilder *strings.Builder,
	thinkingBuilder *strings.Builder,
	upsertTool func(session.ToolCall),
	emit func(ChatEvent),
) {
	msgObj, _ := raw["message"].(map[string]any)
	if msgObj == nil {
		msgObj = raw
	}

	if contentStr, ok := msgObj["content"].(string); ok && contentStr != "" {
		textBuilder.WriteString(contentStr)
		emit(ChatEvent{Type: "text", Delta: contentStr})
		return
	}

	blocks, ok := msgObj["content"].([]any)
	if !ok {
		if delta := extractTextField(msgObj); delta != "" {
			textBuilder.WriteString(delta)
			emit(ChatEvent{Type: "text", Delta: delta})
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
				thinkingBuilder.WriteString(t)
				emit(ChatEvent{Type: "thinking", Delta: t})
			}
		case "text":
			if t := firstString(bm, "text", "content"); t != "" {
				textBuilder.WriteString(t)
				emit(ChatEvent{Type: "text", Delta: t})
			}
		case "tool_use", "tool_call":
			upsertTool(session.ToolCall{
				ID:     firstString(bm, "id", "call_id"),
				Name:   firstString(bm, "name", "tool"),
				Input:  stringifyField(bm["input"], bm["arguments"]),
				Status: "running",
			})
		}
	}
}

func extractTextField(m map[string]any) string {
	if s := firstString(m, "delta", "text", "content", "thinking", "thought"); s != "" {
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
