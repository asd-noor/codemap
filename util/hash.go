// Package util provides shared utilities for codemap.
package util

import (
	"crypto/sha256"
	"fmt"
)

// NodeID returns a stable, deterministic node ID from (filePath, name, kind).
func NodeID(filePath, name, kind string) string {
	h := sha256.Sum256([]byte(filePath + ":" + name + ":" + kind))
	return fmt.Sprintf("%x", h[:16]) // 32-char hex, collision-safe for a codebase
}

// DiagnosticID returns a stable, deterministic diagnostic ID from its location and message.
func DiagnosticID(filePath string, line, col int, message string) string {
	key := fmt.Sprintf("%s:%d:%d:%s", filePath, line, col, message)
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", h[:16])
}
