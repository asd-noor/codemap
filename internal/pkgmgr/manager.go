// Package pkgmgr manages the download and installation of LSP language-server
// binaries on behalf of the codemap engine.
//
// Resolution priority (per binary):
//  1. Binary found on system $PATH
//  2. Cached binary in the codemap install dir (~/.codemap/bin/)
//  3. Auto-download via the package manager
//
// When a binary is found via (1) or (2), a silent background upgrade check is
// fired once per process lifetime.
package pkgmgr

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// installBase returns the base directory for codemap-managed binaries:
// $HOME/.codemap/bin/
func installBase() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".codemap", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// binaryPath returns the full path to a managed binary.
func binaryPath(name string) (string, error) {
	base, err := installBase()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(base, name), nil
}

// Manager provides binary resolution and installation.
type Manager struct{}

// New creates a Manager.
func New() *Manager { return &Manager{} }

// ResolveBinary resolves the given binary name using the priority:
//  1. system $PATH
//  2. codemap-managed install dir
//  3. auto-download (install)
//
// When the binary is found via (1) or (2), a background upgrade check is
// fired once per process lifetime: if a newer version is available it is
// silently reinstalled.
func (m *Manager) ResolveBinary(ctx context.Context, name string) (string, error) {
	// 1. Check system PATH.
	if path, err := exec.LookPath(name); err == nil {
		maybeUpgrade(name, path)
		return path, nil
	}

	// 2. Check codemap-managed install dir.
	cached, err := binaryPath(name)
	if err == nil {
		if _, statErr := os.Stat(cached); statErr == nil {
			maybeUpgrade(name, cached)
			return cached, nil
		}
	}

	// 3. Auto-download.
	installed, err := Install(ctx, name)
	if err != nil {
		return "", fmt.Errorf("pkgmgr: %s not found and auto-install failed: %w", name, err)
	}
	return installed, nil
}
