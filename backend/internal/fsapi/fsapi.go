package fsapi

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/config"
)

type RootItem struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Kind  string `json:"kind"` // share | volume | home
}

type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

type BrowseResponse struct {
	Path    string     `json:"path"`
	Parent  string     `json:"parent"`
	Entries []DirEntry `json:"entries"`
	Roots   []RootItem `json:"roots"`
}

// ListRoots discovers accessible NAS storage roots (fnOS shares, /vol*, and app home).
func ListRoots(paths config.Paths) []RootItem {
	seen := make(map[string]bool)
	var roots []RootItem

	add := func(label, p, kind string) {
		clean := filepath.Clean(p)
		if seen[clean] {
			return
		}
		st, err := os.Stat(clean)
		if err != nil || !st.IsDir() {
			return
		}
		seen[clean] = true
		roots = append(roots, RootItem{Label: label, Path: clean, Kind: kind})
	}

	for i, share := range paths.ShareDirs {
		label := "应用共享目录"
		if i > 0 {
			label = filepath.Base(share)
		}
		add(label, share, "share")
	}

	// Scan /vol1, /vol2, ... on fnOS
	if matches, err := filepath.Glob("/vol*"); err == nil {
		sort.Strings(matches)
		for _, m := range matches {
			add("存储空间 "+filepath.Base(m), m, "volume")
		}
	}

	// Also check /var/apps/antigravity-cli/shares if present
	if matches, err := filepath.Glob("/var/apps/*/shares/*"); err == nil {
		for _, m := range matches {
			if strings.Contains(m, "antigravity-cli") {
				add("授权目录 "+filepath.Base(m), m, "share")
			}
		}
	}

	add("应用主目录", paths.HomeDir, "home")
	return roots
}

// Browse returns subdirectories inside targetPath (or the first available root if empty).
func Browse(paths config.Paths, targetPath string, showHidden bool) (*BrowseResponse, error) {
	roots := ListRoots(paths)

	if strings.ContainsRune(targetPath, 0) {
		return nil, errors.New("invalid path")
	}

	if strings.TrimSpace(targetPath) == "" {
		if len(roots) > 0 {
			targetPath = roots[0].Path
		} else {
			targetPath = paths.HomeDir
		}
	}

	cleanPath := filepath.Clean(targetPath)
	if !filepath.IsAbs(cleanPath) {
		return nil, errors.New("path must be absolute")
	}

	st, err := os.Stat(cleanPath)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("not a directory")
	}

	dirEntries, err := os.ReadDir(cleanPath)
	if err != nil {
		return nil, err
	}

	entries := make([]DirEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(cleanPath, name)
		isDir := de.IsDir()
		if de.Type()&os.ModeSymlink != 0 {
			if targetStat, err := os.Stat(full); err == nil {
				isDir = targetStat.IsDir()
			}
		}
		if !isDir {
			continue
		}
		entries = append(entries, DirEntry{
			Name:  name,
			Path:  full,
			IsDir: true,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	parent := filepath.Dir(cleanPath)
	if parent == cleanPath {
		parent = ""
	}

	return &BrowseResponse{
		Path:    cleanPath,
		Parent:  parent,
		Entries: entries,
		Roots:   roots,
	}, nil
}
