// Package daemon provides helpers for managing the codemap background daemon:
// writing/reading PID files, checking whether the daemon is alive, and
// spawning a detached daemon process.
package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// WritePID writes the current process PID to pidPath.
func WritePID(pidPath string) error {
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

// RemovePID deletes the PID file at pidPath (best-effort; ignores errors).
func RemovePID(pidPath string) {
	os.Remove(pidPath) //nolint:errcheck
}

// IsAlive reads the PID from pidPath and returns true if a process with that
// PID is currently running. Returns false if the file does not exist, cannot
// be parsed, or the process is not found.
func IsAlive(pidPath string) bool {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; we must send signal 0 to test.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// Spawn launches `<exe> watch --project <projectRoot> --daemon` as a
// detached background process. The child inherits no file descriptors
// (stdin/stdout/stderr are all /dev/null) and is placed in its own process
// group so it outlives the caller. exe is the path to the codemap binary
// (os.Executable() from the caller).
func Spawn(exe, projectRoot string) error {
	cmd := exec.Command(exe, "watch", "--project-dir", projectRoot, "--daemon")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	// Detach from the terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("daemon: spawn: %w", err)
	}
	// Do not wait — let the child run independently.
	go cmd.Wait() //nolint:errcheck
	return nil
}
