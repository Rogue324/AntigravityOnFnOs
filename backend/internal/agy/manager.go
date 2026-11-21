package agy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/config"
)

const OfficialInstallURL = "https://antigravity.google/cli/install.sh"

type Status struct {
	Installed     bool   `json:"installed"`
	BinaryPath    string `json:"binary_path"`
	Version       string `json:"version"`
	Authenticated bool   `json:"authenticated"`
	HomeDir       string `json:"home_dir"`
	ProxyActive   bool   `json:"proxy_active"`
}

type ProxyTestResult struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code,omitempty"`
	LatencyMs  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
	TargetURL  string `json:"target_url"`
}

type Manager struct {
	cfgStore   *config.Store
	installMu  sync.Mutex
	installing bool
}

func NewManager(cfgStore *config.Store) *Manager {
	return &Manager{cfgStore: cfgStore}
}

// ResolveBinary locates the agy executable across custom, app-local, and system paths.
func (m *Manager) ResolveBinary() string {
	cfg := m.cfgStore.Get()
	paths := m.cfgStore.Paths()

	candidates := []string{}
	if cfg.AgyBinaryPath != "" {
		candidates = append(candidates, cfg.AgyBinaryPath)
	}
	candidates = append(candidates,
		filepath.Join(paths.BinDir, "agy"),
		filepath.Join(paths.HomeDir, ".local", "bin", "agy"),
		filepath.Join(paths.HomeDir, ".antigravity", "bin", "agy"),
	)

	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return c
		}
	}

	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	return ""
}

// BuildEnv constructs the isolated environment for agy child processes with proxy injection.
func (m *Manager) BuildEnv(extra ...string) []string {
	cfg := m.cfgStore.Get()
	paths := m.cfgStore.Paths()

	basePath := os.Getenv("PATH")
	pathParts := []string{
		paths.BinDir,
		filepath.Join(paths.HomeDir, ".local", "bin"),
		filepath.Join(paths.HomeDir, ".antigravity", "bin"),
	}
	if basePath != "" {
		pathParts = append(pathParts, basePath)
	} else {
		pathParts = append(pathParts, "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}

	envMap := make(map[string]string)
	for _, kv := range os.Environ() {
		if idx := strings.IndexByte(kv, '='); idx > 0 {
			envMap[kv[:idx]] = kv[idx+1:]
		}
	}

	envMap["HOME"] = paths.HomeDir
	envMap["XDG_CONFIG_HOME"] = filepath.Join(paths.HomeDir, ".config")
	envMap["XDG_DATA_HOME"] = filepath.Join(paths.HomeDir, ".local", "share")
	envMap["XDG_CACHE_HOME"] = filepath.Join(paths.HomeDir, ".cache")
	envMap["PATH"] = strings.Join(pathParts, ":")
	envMap["TERM"] = "xterm-256color"
	envMap["COLORTERM"] = "truecolor"
	envMap["LANG"] = "C.UTF-8"

	// Inject proxy settings cleanly
	if cfg.HTTPProxy != "" {
		envMap["HTTP_PROXY"] = cfg.HTTPProxy
		envMap["http_proxy"] = cfg.HTTPProxy
	} else {
		delete(envMap, "HTTP_PROXY")
		delete(envMap, "http_proxy")
	}

	httpsProxy := cfg.HTTPSProxy
	if httpsProxy == "" {
		httpsProxy = cfg.HTTPProxy
	}
	if httpsProxy != "" {
		envMap["HTTPS_PROXY"] = httpsProxy
		envMap["https_proxy"] = httpsProxy
	} else {
		delete(envMap, "HTTPS_PROXY")
		delete(envMap, "https_proxy")
	}

	if cfg.NoProxy != "" {
		envMap["NO_PROXY"] = cfg.NoProxy
		envMap["no_proxy"] = cfg.NoProxy
	}

	for _, kv := range extra {
		if idx := strings.IndexByte(kv, '='); idx > 0 {
			envMap[kv[:idx]] = kv[idx+1:]
		}
	}

	out := make([]string, 0, len(envMap))
	for k, v := range envMap {
		out = append(out, k+"="+v)
	}
	return out
}

// GetStatus inspects agy installation, version, and local auth credential presence.
func (m *Manager) GetStatus(ctx context.Context) Status {
	cfg := m.cfgStore.Get()
	paths := m.cfgStore.Paths()
	bin := m.ResolveBinary()

	st := Status{
		Installed:     bin != "",
		BinaryPath:    bin,
		HomeDir:       paths.HomeDir,
		ProxyActive:   cfg.HTTPProxy != "" || cfg.HTTPSProxy != "",
		Authenticated: m.hasCredentials(paths.HomeDir),
	}

	if bin != "" {
		verCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		cmd := exec.CommandContext(verCtx, bin, "--version")
		cmd.Env = m.BuildEnv()
		if out, err := cmd.CombinedOutput(); err == nil {
			st.Version = strings.TrimSpace(string(out))
		}
	}

	return st
}

func (m *Manager) hasCredentials(homeDir string) bool {
	// Check common credential directories created by agy / Gemini CLI
	checkDirs := []string{
		filepath.Join(homeDir, ".gemini"),
		filepath.Join(homeDir, ".config", "antigravity"),
		filepath.Join(homeDir, ".antigravity"),
	}
	for _, dir := range checkDirs {
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

// InstallOrUpgrade downloads and executes the official agy install script, streaming log lines.
func (m *Manager) InstallOrUpgrade(ctx context.Context, onLog func(line string)) error {
	m.installMu.Lock()
	if m.installing {
		m.installMu.Unlock()
		return errors.New("installation already in progress")
	}
	m.installing = true
	m.installMu.Unlock()

	defer func() {
		m.installMu.Lock()
		m.installing = false
		m.installMu.Unlock()
	}()

	paths := m.cfgStore.Paths()
	if err := os.MkdirAll(paths.BinDir, 0o750); err != nil {
		return err
	}

	onLog(fmt.Sprintf("[info] Downloading installer from %s ...", OfficialInstallURL))
	onLog(fmt.Sprintf("[info] Target HOME=%s, BIN=%s", paths.HomeDir, paths.BinDir))

	script := fmt.Sprintf(
		`set -euo pipefail
export INSTALL_DIR=%q
curl -fsSL %q | bash
`,
		paths.BinDir,
		OfficialInstallURL,
	)

	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Env = m.BuildEnv()
	cmd.Dir = paths.HomeDir

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return err
	}

	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
		for scanner.Scan() {
			onLog(scanner.Text())
		}
	}()

	waitErr := cmd.Wait()
	_ = pw.Close()
	<-scanDone
	_ = pr.Close()

	if waitErr != nil {
		return fmt.Errorf("installer failed: %w", waitErr)
	}

	// Ensure a symlink exists in ${TRIM_PKGVAR}/bin/agy if installed into ~/.local/bin or similar
	if resolved := m.ResolveBinary(); resolved != "" {
		targetLink := filepath.Join(paths.BinDir, "agy")
		if resolved != targetLink {
			_ = os.Remove(targetLink)
			_ = os.Symlink(resolved, targetLink)
		}
		onLog(fmt.Sprintf("[ok] agy installed at %s", resolved))
	} else {
		onLog("[warn] Installer exited 0, but agy binary was not detected in expected paths.")
	}

	return nil
}

// TestProxy checks outbound connectivity through the provided proxy URLs.
func (m *Manager) TestProxy(ctx context.Context, httpProxy, httpsProxy string) ProxyTestResult {
	target := "https://antigravity.google"
	start := time.Now()

	proxyStr := strings.TrimSpace(httpsProxy)
	if proxyStr == "" {
		proxyStr = strings.TrimSpace(httpProxy)
	}

	transport := &http.Transport{
		Proxy: nil,
	}
	if proxyStr != "" {
		parsed, err := url.Parse(proxyStr)
		if err != nil {
			return ProxyTestResult{
				OK:        false,
				LatencyMs: time.Since(start).Milliseconds(),
				Error:     "代理地址格式无效: " + err.Error(),
				TargetURL: target,
			}
		}
		transport.Proxy = http.ProxyURL(parsed)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   8 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return ProxyTestResult{
			OK:        false,
			LatencyMs: time.Since(start).Milliseconds(),
			Error:     err.Error(),
			TargetURL: target,
		}
	}

	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return ProxyTestResult{
			OK:        false,
			LatencyMs: latency,
			Error:     err.Error(),
			TargetURL: target,
		}
	}
	defer resp.Body.Close()

	return ProxyTestResult{
		OK:         resp.StatusCode < 500,
		StatusCode: resp.StatusCode,
		LatencyMs:  latency,
		TargetURL:  target,
	}
}
