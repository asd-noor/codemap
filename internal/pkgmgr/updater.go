package pkgmgr

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

// UpdateCheckInterval defines how often to check for updates (24 hours).
const UpdateCheckInterval = 24 * time.Hour

// LastUpdateCheck tracks when we last checked for updates.
type LastUpdateCheck struct {
	Timestamp time.Time `json:"timestamp"`
}

// getLastCheckPath returns the path to the last update check file.
func getLastCheckPath() (string, error) {
	home, err := GetCodeMapHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".last_update_check"), nil
}

// shouldCheckForUpdates determines if we should check for updates based on last check time.
func shouldCheckForUpdates() bool {
	checkPath, err := getLastCheckPath()
	if err != nil {
		return true // If we can't determine, check anyway
	}

	data, err := os.ReadFile(checkPath)
	if err != nil {
		return true // No file means never checked
	}

	var lastCheck LastUpdateCheck
	if err := json.Unmarshal(data, &lastCheck); err != nil {
		return true // Corrupt file, check anyway
	}

	return time.Since(lastCheck.Timestamp) > UpdateCheckInterval
}

// recordUpdateCheck records that we checked for updates.
func recordUpdateCheck() error {
	checkPath, err := getLastCheckPath()
	if err != nil {
		return err
	}

	lastCheck := LastUpdateCheck{
		Timestamp: time.Now(),
	}

	data, err := json.MarshalIndent(lastCheck, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(checkPath, data, 0644)
}

// CheckAndUpdateInBackground checks for newer versions of installed LSPs and updates them in background.
// This is non-blocking and safe to call on startup.
func (m *Manager) CheckAndUpdateInBackground(ctx context.Context) {
	// Check if we should update (throttle to once per day)
	if !shouldCheckForUpdates() {
		return
	}

	// Run update check in background goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Auto-Update] Panic during background update: %v", r)
			}
		}()

		// Use a timeout context for the entire update process
		updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		log.Println("[Auto-Update] Checking for LSP updates in background...")

		// Get all installed packages
		packages, err := m.ListInstalled()
		if err != nil {
			log.Printf("[Auto-Update] Failed to list installed packages: %v", err)
			return
		}

		if len(packages) == 0 {
			// No packages installed yet, nothing to update
			return
		}

		updatedCount := 0
		for _, pkg := range packages {
			select {
			case <-updateCtx.Done():
				log.Printf("[Auto-Update] Update check cancelled after updating %d packages", updatedCount)
				return
			default:
			}

			// Get latest metadata for this language
			metadata, err := GetLSPMetadata(pkg.Name)
			if err != nil {
				log.Printf("[Auto-Update] Failed to get metadata for %s: %v", pkg.Name, err)
				continue
			}

			// Check if there's a newer version available
			if metadata.Version == pkg.Version {
				continue // Already on latest version
			}

			log.Printf("[Auto-Update] Updating %s from %s to %s...", pkg.Name, pkg.Version, metadata.Version)

			// Install the new version
			installer := NewInstaller(m)
			if err := installer.Install(updateCtx, pkg.Name, metadata); err != nil {
				log.Printf("[Auto-Update] Failed to update %s: %v", pkg.Name, err)
				continue
			}

			updatedCount++
			log.Printf("[Auto-Update] Successfully updated %s to %s", pkg.Name, metadata.Version)
		}

		if updatedCount > 0 {
			log.Printf("[Auto-Update] Updated %d package(s) in background. Changes will take effect on next launch.", updatedCount)
		} else {
			log.Println("[Auto-Update] All packages are up to date")
		}

		// Record that we checked for updates
		if err := recordUpdateCheck(); err != nil {
			log.Printf("[Auto-Update] Failed to record update check: %v", err)
		}
	}()
}
