package agy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

// persistentWorker holds a warm `agy --input-format stream-json --output-format stream-json` process
// for a single chat session, automatically closing after idleTimeout of inactivity.
type persistentWorker struct {
	sessionID   string
	signature   string
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	lines       chan string
	eofCh       chan struct{}
	stderrBuf   strings.Builder
	stderrMu    sync.Mutex

	turnMu      sync.Mutex
	timerMu     sync.Mutex
	idleTimer   *time.Timer
	closed      bool
	closeOnce   sync.Once
	onClose     func()
}

type workerPool struct {
	mu      sync.Mutex
	workers map[string]*persistentWorker
}

func newWorkerPool() *workerPool {
	return &workerPool{
		workers: make(map[string]*persistentWorker),
	}
}

func (p *workerPool) activeSessionIDs() []string {
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

func (p *workerPool) closeSession(sessionID string) {
	p.mu.Lock()
	w := p.workers[sessionID]
	delete(p.workers, sessionID)
	p.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

func (p *workerPool) closeAll() {
	p.mu.Lock()
	all := make([]*persistentWorker, 0, len(p.workers))
	for id, w := range p.workers {
		all = append(all, w)
		delete(p.workers, id)
	}
	p.mu.Unlock()
	for _, w := range all {
		w.Close()
	}
}

func (w *persistentWorker) isClosed() bool {
	w.timerMu.Lock()
	defer w.timerMu.Unlock()
	return w.closed
}

func (w *persistentWorker) stopIdleTimer() {
	w.timerMu.Lock()
	defer w.timerMu.Unlock()
	if w.idleTimer != nil {
		w.idleTimer.Stop()
		w.idleTimer = nil
	}
}

func (w *persistentWorker) resetIdleTimer(d time.Duration) {
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

func (w *persistentWorker) Close() {
	w.closeOnce.Do(func() {
		w.timerMu.Lock()
		w.closed = true
		if w.idleTimer != nil {
			w.idleTimer.Stop()
			w.idleTimer = nil
		}
		w.timerMu.Unlock()

		_ = w.stdin.Close()

		// Wait briefly for graceful exit after closing stdin, then terminate process group
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

func (w *persistentWorker) stderrText() string {
	w.stderrMu.Lock()
	defer w.stderrMu.Unlock()
	return strings.TrimSpace(w.stderrBuf.String())
}

func buildWorkerSignature(bin, cwd, model, effort, permMode string, allowSkip bool, extraDirs []string, proxyKey string) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%t|%s|%s",
		bin, cwd, model, effort, permMode, allowSkip, strings.Join(extraDirs, ","), proxyKey)
}

func (m *Manager) getOrStartWorker(sessionID string, req ChatRequest, bin, cwd string) (*persistentWorker, error) {
	cfg := m.cfgStore.Get()
	proxyKey := cfg.HTTPProxy + "|" + cfg.HTTPSProxy
	sig := buildWorkerSignature(bin, cwd, req.Model, req.Effort, req.PermissionMode, cfg.AllowDangerousSkip, req.ExtraDirs, proxyKey)

	m.pool.mu.Lock()
	existing := m.pool.workers[sessionID]
	if existing != nil {
		if !existing.isClosed() && existing.signature == sig {
			existing.stopIdleTimer()
			m.pool.mu.Unlock()
			return existing, nil
		}
		delete(m.pool.workers, sessionID)
	}
	m.pool.mu.Unlock()

	if existing != nil {
		existing.Close()
	}

	args := []string{"--input-format", "stream-json", "--output-format", "stream-json"}
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
		return nil, fmt.Errorf("failed to start persistent agy: %w", err)
	}

	w := &persistentWorker{
		sessionID: sessionID,
		signature: sig,
		cmd:       cmd,
		stdin:     stdin,
		lines:     make(chan string, 256),
		eofCh:     make(chan struct{}),
	}
	w.onClose = func() {
		m.pool.mu.Lock()
		if m.pool.workers[sessionID] == w {
			delete(m.pool.workers, sessionID)
		}
		m.pool.mu.Unlock()
	}

	go func() {
		defer close(w.eofCh)
		defer close(w.lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			w.lines <- scanner.Text()
		}
	}()

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

	m.pool.mu.Lock()
	m.pool.workers[sessionID] = w
	m.pool.mu.Unlock()

	return w, nil
}

// runPersistentTurn sends a single user turn to a warm persistent agy worker and streams until `result`.
func (m *Manager) runPersistentTurn(ctx context.Context, req ChatRequest, bin, cwd string, idleTimeout time.Duration, emit func(ChatEvent)) (*ChatResult, error) {
	w, err := m.getOrStartWorker(req.SessionID, req, bin, cwd)
	if err != nil {
		return nil, err
	}

	w.turnMu.Lock()
	defer w.turnMu.Unlock()

	w.stopIdleTimer()

	// Send user message JSON line on stdin
	payload := map[string]any{
		"event": "user",
		"message": map[string]any{
			"content": req.Prompt,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if _, err := w.stdin.Write(append(encoded, '\n')); err != nil {
		w.Close()
		return nil, fmt.Errorf("failed to write prompt to persistent agy: %w", err)
	}

	res := &ChatResult{
		ConversationID: req.ConversationID,
		Message: session.Message{
			Role:      "assistant",
			CreatedAt: time.Now(),
		},
	}
	parser := newStreamParser(res, emit)

	turnCompleted := false
	for !turnCompleted {
		select {
		case <-ctx.Done():
			// Abort cancels the in-flight turn by closing the worker cleanly; next message restarts it
			w.Close()
			res.Message.Content = parser.textBuilder.String()
			res.Message.Thinking = parser.thinkingBuilder.String()
			if res.Message.Error == "" {
				res.Message.Error = "已停止生成"
			}
			return res, ctx.Err()

		case line, ok := <-w.lines:
			if !ok {
				// Process exited unexpectedly before emitting result
				w.Close()
				res.Message.Content = parser.textBuilder.String()
				res.Message.Thinking = parser.thinkingBuilder.String()
				if errText := w.stderrText(); errText != "" && res.Message.Error == "" {
					res.Message.Error = errText
					emit(ChatEvent{Type: "error", Error: errText})
				}
				emit(ChatEvent{Type: "done", ConversationID: res.ConversationID, Usage: res.Message.Usage})
				return res, nil
			}
			parser.ProcessLine(line)
			if isTerminalResultLine(line) {
				turnCompleted = true
			}
		}
	}

	res.Message.Content = parser.textBuilder.String()
	res.Message.Thinking = parser.thinkingBuilder.String()

	// Schedule automatic idle shutdown
	w.resetIdleTimer(idleTimeout)

	emit(ChatEvent{Type: "done", ConversationID: res.ConversationID, Usage: res.Message.Usage})
	return res, nil
}

func isTerminalResultLine(rawLine string) bool {
	var probe struct {
		Event string `json:"event"`
		Type  string `json:"type"`
	}
	if json.Unmarshal([]byte(rawLine), &probe) != nil {
		return false
	}
	ev := strings.ToLower(probe.Event)
	if ev == "" {
		ev = strings.ToLower(probe.Type)
	}
	return ev == "result" || ev == "done" || ev == "complete"
}

// isSlashCommand detects CLI-handled slash commands (like /model, /usage) that must run via -p.
func isSlashCommand(prompt string) bool {
	trimmed := strings.TrimSpace(prompt)
	return strings.HasPrefix(trimmed, "/")
}

// Ensure os import is used for future extensions
var _ = os.Stat
