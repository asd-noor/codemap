package pkgmgr

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShouldCheckForUpdates(t *testing.T) {
	// Save original check path
	origHome := os.Getenv("CODEMAP_HOME")
	defer func() {
		if origHome != "" {
			os.Setenv("CODEMAP_HOME", origHome)
		} else {
			os.Unsetenv("CODEMAP_HOME")
		}
	}()

	// Use temp directory for testing
	tmpDir := t.TempDir()
	os.Setenv("CODEMAP_HOME", tmpDir)

	// First check - should return true (no file exists)
	if !shouldCheckForUpdates() {
		t.Error("First check should return true when no file exists")
	}

	// Record an update check
	if err := recordUpdateCheck(); err != nil {
		t.Fatalf("Failed to record update check: %v", err)
	}

	// Immediately after recording, should return false (too soon)
	if shouldCheckForUpdates() {
		t.Error("Should return false immediately after recording check")
	}

	// Verify the file was created
	checkPath := filepath.Join(tmpDir, ".last_update_check")
	if _, err := os.Stat(checkPath); os.IsNotExist(err) {
		t.Error("Update check file was not created")
	}

	// Simulate old timestamp (force check)
	oldCheck := LastUpdateCheck{
		Timestamp: time.Now().Add(-25 * time.Hour), // 25 hours ago
	}
	data, _ := os.ReadFile(checkPath)
	t.Logf("Current check file: %s", string(data))

	// Write old timestamp
	if err := writeLastCheck(oldCheck); err != nil {
		t.Fatalf("Failed to write old timestamp: %v", err)
	}

	// Should return true now (24+ hours passed)
	if !shouldCheckForUpdates() {
		t.Error("Should return true when more than 24 hours have passed")
	}
}

func TestCheckAndUpdateInBackground(t *testing.T) {
	// This is more of an integration test
	// We'll just verify it doesn't crash
	tmpDir := t.TempDir()
	os.Setenv("CODEMAP_HOME", tmpDir)
	defer os.Unsetenv("CODEMAP_HOME")

	// Create manager
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Call CheckAndUpdateInBackground - should not block
	ctx := context.Background()
	mgr.CheckAndUpdateInBackground(ctx)

	// If we get here, it means the function didn't block (good)
	t.Log("CheckAndUpdateInBackground returned immediately (non-blocking)")

	// Give background goroutine a moment to start
	time.Sleep(100 * time.Millisecond)
}

// Helper function to write a LastUpdateCheck with custom timestamp
func writeLastCheck(check LastUpdateCheck) error {
	checkPath, err := getLastCheckPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(checkPath)
	if err != nil {
		return err
	}

	// Parse existing check
	var existing LastUpdateCheck
	if err := unmarshalJSON(data, &existing); err != nil {
		return err
	}

	// Overwrite with new timestamp
	check.Timestamp = check.Timestamp
	newData, err := marshalJSON(check)
	if err != nil {
		return err
	}

	return os.WriteFile(checkPath, newData, 0644)
}

// Helper functions to avoid import cycle
func marshalJSON(v interface{}) ([]byte, error) {
	return []byte(`{"timestamp":"` + v.(LastUpdateCheck).Timestamp.Format(time.RFC3339) + `"}`), nil
}

func unmarshalJSON(data []byte, v interface{}) error {
	// Simple parsing for test purposes
	return nil
}
