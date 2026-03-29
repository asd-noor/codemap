// Package db provides shared utilities for resolving per-project SQLite
// database paths under <project-root>/.ctxhub/.
package db

import (
	"os"
	"path/filepath"
)

// DBPath returns the full path for a named SQLite database file inside the
// .ctxhub directory for projectPath.
func DBPath(projectPath, dbName string) (string, error) {
	dir := filepath.Join(projectPath, ".ctxhub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, dbName), nil
}

// PIDPath returns the path to the codemap daemon PID file for projectPath.
func PIDPath(projectPath string) (string, error) {
	return DBPath(projectPath, "codemap.pid")
}
