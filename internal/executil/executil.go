// Package executil provides helpers for running external commands safely.
package executil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var defaultSafeDirs = []string{
	"/usr/local/bin",
	"/usr/bin",
	"/bin",
	"/usr/sbin",
	"/sbin",
	"/opt/homebrew/bin",
}

// Command builds an exec.Cmd using a sanitized PATH and a resolved executable.
func Command(name string, args ...string) (*exec.Cmd, error) {
	path, env, err := resolveCommand(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, args...)
	cmd.Env = env
	return cmd, nil
}

// CommandContext builds an exec.Cmd with context using a sanitized PATH and a resolved executable.
func CommandContext(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	path, env, err := resolveCommand(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	return cmd, nil
}

// SafeEnv returns the current environment with PATH replaced by a sanitized value.
func SafeEnv() []string {
	return safeEnv(safePathDirs())
}

func resolveCommand(name string) (string, []string, error) {
	safeDirs := safePathDirs()
	path, err := findExecutable(name, safeDirs)
	if err != nil {
		return "", nil, err
	}
	return path, safeEnv(safeDirs), nil
}

func safeEnv(dirs []string) []string {
	if len(dirs) == 0 {
		return os.Environ()
	}
	safePath := strings.Join(dirs, string(os.PathListSeparator))
	return replaceEnv(os.Environ(), "PATH", safePath)
}

func safePathDirs() []string {
	seen := make(map[string]struct{})
	dirs := make([]string, 0, len(defaultSafeDirs))

	addDir := func(dir string, requireSafe bool) {
		if dir == "" {
			return
		}
		dir = filepath.Clean(dir)
		if !filepath.IsAbs(dir) {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			if requireSafe {
				return
			}
		} else if requireSafe && !isSafeDir(info) {
			return
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}

	for _, dir := range defaultSafeDirs {
		addDir(dir, true)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		addDir(dir, true)
	}
	if len(dirs) == 0 {
		for _, dir := range defaultSafeDirs {
			addDir(dir, false)
		}
	}
	return dirs
}

func isSafeDir(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o022 == 0
}

func findExecutable(name string, dirs []string) (string, error) {
	if filepath.IsAbs(name) {
		return name, nil
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		cleaned := filepath.Clean(name)
		if isExecutable(cleaned) {
			return cleaned, nil
		}
		return "", fmt.Errorf("executable not found: %s", name)
	}

	for _, dir := range dirs {
		for _, candidate := range candidatePaths(dir, name) {
			if isExecutable(candidate) {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("executable not found in safe PATH: %s", name)
}

func candidatePaths(dir, name string) []string {
	if runtime.GOOS != "windows" {
		return []string{filepath.Join(dir, name)}
	}
	if filepath.Ext(name) != "" {
		return []string{filepath.Join(dir, name)}
	}
	exts := strings.Split(strings.ToLower(os.Getenv("PATHEXT")), ";")
	if len(exts) == 0 || exts[0] == "" {
		return []string{filepath.Join(dir, name)}
	}
	paths := make([]string, 0, len(exts))
	for _, ext := range exts {
		if ext == "" {
			continue
		}
		paths = append(paths, filepath.Join(dir, name+ext))
	}
	return paths
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		out = append(out, entry)
	}
	if value != "" {
		out = append(out, prefix+value)
	}
	return out
}
