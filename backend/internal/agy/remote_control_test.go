package agy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/config"
)

func TestRemoteControlSupervisorLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	paths := config.Paths{
		DataDir:      tmpDir,
		HomeDir:      filepath.Join(tmpDir, "home"),
		BinDir:       filepath.Join(tmpDir, "bin"),
		ConfigFile:   filepath.Join(tmpDir, "config.json"),
		SessionsFile: filepath.Join(tmpDir, "sessions.json"),
	}
	store, err := config.NewStore(paths)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Create a mock `agy` binary that verifies arguments and prints env info before sleeping.
	fakeBin := filepath.Join(paths.BinDir, "agy")
	script := `#!/bin/sh
if [ "$1" = "remote-control" ] && [ "$2" = "serve" ]; then
  echo "mock remote-control serve ready proxy=${HTTPS_PROXY}"
  trap 'exit 0' TERM INT
  while true; do sleep 1; done
fi
exit 1
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agy failed: %v", err)
	}

	if err := store.Update(func(c *config.Config) {
		c.AgyBinaryPath = fakeBin
		c.HTTPSProxy = "http://127.0.0.1:7890"
		c.RemoteControlEnabled = true
	}); err != nil {
		t.Fatalf("config update failed: %v", err)
	}

	mgr := NewManager(store)
	defer mgr.CloseAll()

	mgr.StartBackgroundServices()

	// Wait until supervisor reports running and log contains expected proxy output.
	deadline := time.Now().Add(4 * time.Second)
	var st RemoteControlStatus
	for time.Now().Before(deadline) {
		st = mgr.RemoteControlStatus()
		if st.Running && st.PID > 0 && containsLog(st.Logs, "mock remote-control serve ready proxy=http://127.0.0.1:7890") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !st.Running || st.PID == 0 {
		t.Fatalf("expected remote control running, got: %+v", st)
	}
	firstPID := st.PID

	// Restart should replace the PID.
	if !mgr.RestartRemoteControl() {
		t.Fatalf("expected RestartRemoteControl to return true")
	}
	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		st = mgr.RemoteControlStatus()
		if st.Running && st.PID > 0 && st.PID != firstPID {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !st.Running || st.PID == firstPID {
		t.Fatalf("expected new PID after restart (old=%d), got: %+v", firstPID, st)
	}

	// Disable in config and sync -> should stop cleanly.
	if err := store.Update(func(c *config.Config) {
		c.RemoteControlEnabled = false
	}); err != nil {
		t.Fatalf("config update failed: %v", err)
	}
	mgr.SyncRemoteControl()

	st = mgr.RemoteControlStatus()
	if st.Enabled || st.Running || st.State != "stopped" {
		t.Fatalf("expected stopped state after disable, got: %+v", st)
	}
}

func containsLog(logs []string, substr string) bool {
	for _, l := range logs {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}
