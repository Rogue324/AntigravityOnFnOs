package agy

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	rcMaxLogLines      = 120
	rcInitialBackoff   = 3 * time.Second
	rcMaxBackoff       = 30 * time.Second
	rcHealthyThreshold = 60 * time.Second
	rcMissingBinDelay  = 15 * time.Second
)

// RemoteControlStatus describes the runtime state of the managed `agy remote-control serve` daemon.
type RemoteControlStatus struct {
	Enabled      bool     `json:"enabled"`
	Running      bool     `json:"running"`
	PID          int      `json:"pid,omitempty"`
	State        string   `json:"state"` // stopped | starting | running | restarting | error
	StartedAt    string   `json:"started_at,omitempty"`
	RestartCount int      `json:"restart_count"`
	LastError    string   `json:"last_error,omitempty"`
	Logs         []string `json:"logs"`
}

type remoteControlSupervisor struct {
	mgr *Manager

	mu           sync.Mutex
	enabled      bool
	running      bool
	pid          int
	state        string
	startedAt    time.Time
	restartCount int
	lastError    string
	logs         []string

	stopCh chan struct{}
	doneCh chan struct{}
	cmd    *exec.Cmd
}

func newRemoteControlSupervisor(mgr *Manager) *remoteControlSupervisor {
	return &remoteControlSupervisor{
		mgr:   mgr,
		state: "stopped",
	}
}

func (s *remoteControlSupervisor) appendLogLocked(line string) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}
	entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), line)
	s.logs = append(s.logs, entry)
	if len(s.logs) > rcMaxLogLines {
		s.logs = append([]string(nil), s.logs[len(s.logs)-rcMaxLogLines:]...)
	}
}

func (s *remoteControlSupervisor) appendLog(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendLogLocked(line)
}

func (s *remoteControlSupervisor) Status() RemoteControlStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	logsCopy := make([]string, len(s.logs))
	copy(logsCopy, s.logs)

	var startedAt string
	if !s.startedAt.IsZero() && s.running {
		startedAt = s.startedAt.UTC().Format(time.RFC3339)
	}

	return RemoteControlStatus{
		Enabled:      s.enabled,
		Running:      s.running,
		PID:          s.pid,
		State:        s.state,
		StartedAt:    startedAt,
		RestartCount: s.restartCount,
		LastError:    s.lastError,
		Logs:         logsCopy,
	}
}

// Start launches the supervised background loop if not already active.
func (s *remoteControlSupervisor) Start() {
	s.mu.Lock()
	if s.enabled && s.doneCh != nil {
		s.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	s.enabled = true
	s.state = "starting"
	s.lastError = ""
	s.restartCount = 0
	s.stopCh = stopCh
	s.doneCh = doneCh
	s.appendLogLocked("[manager] 正在启动后台远程控制守护进程...")
	s.mu.Unlock()

	go s.runLoop(stopCh, doneCh)
}

// Stop gracefully terminates the child process and waits for the supervisor loop to exit.
func (s *remoteControlSupervisor) Stop() {
	s.mu.Lock()
	if !s.enabled && s.doneCh == nil {
		s.mu.Unlock()
		return
	}
	s.enabled = false
	stopCh := s.stopCh
	doneCh := s.doneCh
	cmd := s.cmd
	s.stopCh = nil
	s.doneCh = nil
	s.appendLogLocked("[manager] 正在停止后台远程控制守护进程...")
	s.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	stopRemoteControlProcess(cmd)
	if doneCh != nil {
		<-doneCh
	}

	s.mu.Lock()
	if !s.enabled {
		s.running = false
		s.pid = 0
		s.state = "stopped"
		s.startedAt = time.Time{}
		s.cmd = nil
	}
	s.mu.Unlock()
}

// Restart stops and restarts the supervisor if it is currently enabled.
func (s *remoteControlSupervisor) Restart() bool {
	s.mu.Lock()
	wasEnabled := s.enabled
	s.mu.Unlock()
	if !wasEnabled {
		return false
	}
	s.Stop()
	s.Start()
	return true
}

func (s *remoteControlSupervisor) runLoop(stopCh <-chan struct{}, doneCh chan struct{}) {
	defer close(doneCh)

	backoff := rcInitialBackoff

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		bin := s.mgr.ResolveBinary()
		if bin == "" {
			s.mu.Lock()
			s.running = false
			s.pid = 0
			s.state = "error"
			s.lastError = "未检测到 agy 可执行文件，请先在设置中心一键安装"
			s.appendLogLocked("[error] 未检测到 agy 可执行文件，15 秒后重试")
			s.mu.Unlock()

			if !sleepOrStop(stopCh, rcMissingBinDelay) {
				return
			}
			continue
		}

		cfg := s.mgr.cfgStore.Get()
		cwd := cfg.DefaultWorkspace
		if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
			cwd = s.mgr.cfgStore.Paths().HomeDir
		}

		cmd := exec.Command(bin, "remote-control", "serve")
		cmd.Dir = cwd
		cmd.Env = s.mgr.BuildEnv("NO_COLOR=1")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw

		if err := cmd.Start(); err != nil {
			_ = pr.Close()
			_ = pw.Close()

			s.mu.Lock()
			s.running = false
			s.pid = 0
			s.state = "error"
			s.lastError = fmt.Sprintf("启动失败: %v", err)
			s.restartCount++
			s.appendLogLocked(fmt.Sprintf("[error] 启动 agy remote-control serve 失败: %v", err))
			s.mu.Unlock()

			if !sleepOrStop(stopCh, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		startedAt := time.Now()
		s.mu.Lock()
		// Check if Stop() raced with cmd.Start().
		select {
		case <-stopCh:
			s.mu.Unlock()
			stopRemoteControlProcess(cmd)
			_ = pw.Close()
			_ = pr.Close()
			_ = cmd.Wait()
			return
		default:
		}
		s.cmd = cmd
		s.running = true
		s.pid = cmd.Process.Pid
		s.state = "running"
		s.startedAt = startedAt
		s.lastError = ""
		s.appendLogLocked(fmt.Sprintf("[manager] agy remote-control serve 已启动 (PID %d, dir=%s)", cmd.Process.Pid, cwd))
		s.mu.Unlock()

		var lastOutputMu sync.Mutex
		var lastOutputLine string
		scanDone := make(chan struct{})
		go func() {
			defer close(scanDone)
			scanner := bufio.NewScanner(pr)
			scanner.Buffer(make([]byte, 0, 16*1024), 256*1024)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				lastOutputMu.Lock()
				lastOutputLine = line
				lastOutputMu.Unlock()
				s.appendLog(line)
			}
		}()

		waitErr := cmd.Wait()
		_ = pw.Close()
		<-scanDone
		_ = pr.Close()

		// If stopped intentionally, exit cleanly without scheduling a restart.
		select {
		case <-stopCh:
			s.mu.Lock()
			s.cmd = nil
			s.running = false
			s.pid = 0
			s.state = "stopped"
			s.startedAt = time.Time{}
			s.appendLogLocked("[manager] 后台远程控制守护进程已停止")
			s.mu.Unlock()
			return
		default:
		}

		uptime := time.Since(startedAt)
		if uptime >= rcHealthyThreshold {
			backoff = rcInitialBackoff
		}

		lastOutputMu.Lock()
		hint := lastOutputLine
		lastOutputMu.Unlock()

		errMsg := "进程意外退出"
		if waitErr != nil {
			errMsg = fmt.Sprintf("进程退出: %v", waitErr)
		}
		if hint != "" {
			errMsg = errMsg + " (" + hint + ")"
		}

		s.mu.Lock()
		s.cmd = nil
		s.running = false
		s.pid = 0
		s.startedAt = time.Time{}
		s.restartCount++
		s.state = "restarting"
		s.lastError = errMsg
		s.appendLogLocked(fmt.Sprintf("[warn] %s，将在 %s 后自动重连", errMsg, backoff))
		s.mu.Unlock()

		if !sleepOrStop(stopCh, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
	}
}

func stopRemoteControlProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func(p int) {
		time.Sleep(2 * time.Second)
		_ = syscall.Kill(-p, syscall.SIGKILL)
	}(pid)
}

func sleepOrStop(stopCh <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-stopCh:
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(cur time.Duration) time.Duration {
	next := cur * 2
	if next > rcMaxBackoff {
		return rcMaxBackoff
	}
	return next
}
