// Package db provides a small utility for ensuring a file's parent directory
// exists before the file is created.
package db

import (
	"os"
	"path/filepath"
)

// EnsureDir creates the parent directory of filePath (including all parents)
// if it does not already exist.
func EnsureDir(filePath string) error {
	return os.MkdirAll(filepath.Dir(filePath), 0o755)
}
