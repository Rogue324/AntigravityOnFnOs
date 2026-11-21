package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Config holds persistent application settings stored in ${TRIM_PKGVAR}/config.json.
type Config struct {
	PasswordHash          string `json:"password_hash,omitempty"`
	HTTPProxy             string `json:"http_proxy"`
	HTTPSProxy            string `json:"https_proxy"`
	NoProxy               string `json:"no_proxy"`
	DefaultModel          string `json:"default_model"`
	DefaultPermissionMode string `json:"default_permission_mode"` // default | accept-edits | plan | bypass
	AllowDangerousSkip    bool   `json:"allow_dangerous_skip"`
	DefaultWorkspace      string `json:"default_workspace"`
	AgyBinaryPath         string `json:"agy_binary_path"`
}

// PublicConfig is the sanitized configuration returned to the frontend.
type PublicConfig struct {
	HTTPProxy             string `json:"http_proxy"`
	HTTPSProxy            string `json:"https_proxy"`
	NoProxy               string `json:"no_proxy"`
	DefaultModel          string `json:"default_model"`
	DefaultPermissionMode string `json:"default_permission_mode"`
	AllowDangerousSkip    bool   `json:"allow_dangerous_skip"`
	DefaultWorkspace      string `json:"default_workspace"`
	AgyBinaryPath         string `json:"agy_binary_path"`
	DataDir               string `json:"data_dir"`
	HomeDir               string `json:"home_dir"`
	BinDir                string `json:"bin_dir"`
}

// Paths describes resolved runtime directories for the application.
type Paths struct {
	DataDir      string
	HomeDir      string
	BinDir       string
	ConfigFile   string
	SessionsFile string
	ShareDirs    []string
}

// Store provides thread-safe access to Config and runtime Paths.
type Store struct {
	mu    sync.RWMutex
	paths Paths
	cfg   Config
}

// ResolvePaths determines runtime directories from fnOS or local dev environment variables.
func ResolvePaths() Paths {
	dataDir := os.Getenv("AGY_DATA_DIR")
	if dataDir == "" {
		dataDir = os.Getenv("TRIM_PKGVAR")
	}
	if dataDir == "" {
		wd, _ := os.Getwd()
		dataDir = filepath.Join(wd, ".local-data")
	}

	homeDir := os.Getenv("AGY_HOME_DIR")
	if homeDir == "" {
		homeDir = filepath.Join(dataDir, "home")
	}

	binDir := filepath.Join(dataDir, "bin")

	var shareDirs []string
	if rawShares := os.Getenv("TRIM_DATA_SHARE_PATHS"); rawShares != "" {
		for _, p := range strings.Split(rawShares, ":") {
			p = strings.TrimSpace(p)
			if p != "" {
				shareDirs = append(shareDirs, p)
			}
		}
	}

	return Paths{
		DataDir:      dataDir,
		HomeDir:      homeDir,
		BinDir:       binDir,
		ConfigFile:   filepath.Join(dataDir, "config.json"),
		SessionsFile: filepath.Join(dataDir, "sessions.json"),
		ShareDirs:    shareDirs,
	}
}

// NewStore initializes directories and loads existing config if present.
func NewStore(paths Paths) (*Store, error) {
	for _, dir := range []string{paths.DataDir, paths.HomeDir, paths.BinDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
	}

	defaultWS := ""
	if len(paths.ShareDirs) > 0 {
		defaultWS = paths.ShareDirs[0]
	} else if st, err := os.Stat("/vol1"); err == nil && st.IsDir() {
		defaultWS = "/vol1"
	} else {
		defaultWS = paths.HomeDir
	}

	s := &Store{
		paths: paths,
		cfg: Config{
			NoProxy:               "localhost,127.0.0.1,::1",
			DefaultModel:          "",
			DefaultPermissionMode: "default",
			AllowDangerousSkip:    false,
			DefaultWorkspace:      defaultWS,
		},
	}

	if err := s.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	return s, nil
}

func (s *Store) Paths() Paths {
	return s.paths
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) Public() PublicConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return PublicConfig{
		HTTPProxy:             s.cfg.HTTPProxy,
		HTTPSProxy:            s.cfg.HTTPSProxy,
		NoProxy:               s.cfg.NoProxy,
		DefaultModel:          s.cfg.DefaultModel,
		DefaultPermissionMode: s.cfg.DefaultPermissionMode,
		AllowDangerousSkip:    s.cfg.AllowDangerousSkip,
		DefaultWorkspace:      s.cfg.DefaultWorkspace,
		AgyBinaryPath:         s.cfg.AgyBinaryPath,
		DataDir:               s.paths.DataDir,
		HomeDir:               s.paths.HomeDir,
		BinDir:                s.paths.BinDir,
	}
}

// Update modifies the config via callback and atomically saves it to disk.
func (s *Store) Update(fn func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
	return s.saveLocked()
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.paths.ConfigFile)
	if err != nil {
		return err
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	if loaded.DefaultPermissionMode == "" {
		loaded.DefaultPermissionMode = "default"
	}
	if loaded.DefaultWorkspace == "" {
		loaded.DefaultWorkspace = s.cfg.DefaultWorkspace
	}
	s.cfg = loaded
	return nil
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.paths.ConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.paths.ConfigFile)
}
