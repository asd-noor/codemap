package util

import (
	"os"
	"path/filepath"
)

// FindGitRoot walks up from dir until it finds a .git directory or hits the
// filesystem root. Returns dir itself if no .git is found.
func FindGitRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	cur := abs
	for {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return abs
}
