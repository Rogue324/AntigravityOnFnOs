package agy

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type PTYOptions struct {
	Mode           string // agy | shell
	Workspace      string
	ConversationID string
	Cols           uint16
	Rows           uint16
}

type wsControlMsg struct {
	Type string `json:"type"` // input | resize | ping
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// ServePTY attaches a WebSocket connection to an interactive PTY running agy or bash.
func (m *Manager) ServePTY(conn *websocket.Conn, opts PTYOptions) error {
	cfg := m.cfgStore.Get()
	cwd := opts.Workspace
	if cwd == "" {
		cwd = cfg.DefaultWorkspace
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		cwd = m.cfgStore.Paths().HomeDir
	}

	var cmd *exec.Cmd
	if opts.Mode == "shell" {
		cmd = exec.Command("bash", "-l")
	} else {
		bin := m.ResolveBinary()
		if bin != "" {
			args := []string{}
			if opts.ConversationID != "" {
				args = append(args, "--conversation", opts.ConversationID)
			}
			cmd = exec.Command(bin, args...)
		} else {
			// Helpful fallback shell if agy isn't installed yet
			banner := `echo -e "\033[1;33m[Antigravity for fnOS]\033[0m 未检测到 agy，可前往「设置中心」一键安装，或直接在此运行：\n  curl -fsSL https://antigravity.google/cli/install.sh | bash\n"; exec bash`
			cmd = exec.Command("bash", "-c", banner)
		}
	}

	cmd.Dir = cwd
	cmd.Env = m.BuildEnv()

	cols := opts.Cols
	rows := opts.Rows
	if cols == 0 {
		cols = 120
	}
	if rows == 0 {
		rows = 34
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return fmt.Errorf("failed to start PTY: %w", err)
	}

	var closeOnce sync.Once
	cleanup := func() {
		closeOnce.Do(func() {
			_ = ptmx.Close()
			if cmd.Process != nil {
				// Kill the PTY session process group so child processes don't linger
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
				go func(pid int) {
					time.Sleep(500 * time.Millisecond)
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}(cmd.Process.Pid)
			}
			_ = cmd.Wait()
		})
	}
	defer cleanup()

	var wsWriteMu sync.Mutex
	writeWS := func(msgType int, data []byte) error {
		wsWriteMu.Lock()
		defer wsWriteMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
		return conn.WriteMessage(msgType, data)
	}

	// Keepalive ping loop
	stopPing := make(chan struct{})
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := writeWS(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-stopPing:
				return
			}
		}
	}()
	defer close(stopPing)

	// PTY -> WebSocket
	ptyDone := make(chan struct{})
	go func() {
		defer close(ptyDone)
		buf := make([]byte, 8192)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				if err := writeWS(websocket.BinaryMessage, buf[:n]); err != nil {
					return
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					_ = writeWS(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[31m[终端已退出: %v]\x1b[0m\r\n", readErr)))
				}
				return
			}
		}
	}()

	// WebSocket -> PTY
	wsDone := make(chan struct{})
	go func() {
		defer close(wsDone)
		for {
			msgType, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				var ctrl wsControlMsg
				if json.Unmarshal(payload, &ctrl) == nil && ctrl.Type != "" {
					switch ctrl.Type {
					case "resize":
						if ctrl.Cols > 0 && ctrl.Rows > 0 {
							_ = pty.Setsize(ptmx, &pty.Winsize{Rows: ctrl.Rows, Cols: ctrl.Cols})
						}
					case "input":
						_, _ = ptmx.Write([]byte(ctrl.Data))
					case "ping":
						_ = writeWS(websocket.TextMessage, []byte(`{"type":"pong"}`))
					}
					continue
				}
			}
			_, _ = ptmx.Write(payload)
		}
	}()

	select {
	case <-ptyDone:
	case <-wsDone:
	}

	return nil
}
