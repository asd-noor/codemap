// Package pkgmgr — upgrade.go
//
// Background upgrade machinery: version detection, latest-version lookup, and
// silent re-install when a newer version is available.
//
// All upgrade work runs in a fire-and-forget goroutine; failures are silently
// discarded so the main code path is never blocked or broken by network issues.
package pkgmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// upgradeTimeout bounds how long a single version-check + re-install may take.
const upgradeTimeout = 5 * time.Minute

// checked tracks which binaries have already had an upgrade check fired this
// process lifetime so we only check once per invocation.
var checked sync.Map // map[string]struct{}

// maybeUpgrade fires a background goroutine that checks whether a newer
// version of the binary at binaryPath is available and, if so, silently
// reinstalls it. It does nothing if an upgrade check has already been
// initiated for this name during the current process.
func maybeUpgrade(name, binaryPath string) {
	if _, already := checked.LoadOrStore(name, struct{}{}); already {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), upgradeTimeout)
		defer cancel()
		silentUpgrade(ctx, name, binaryPath)
	}()
}

// silentUpgrade is the actual upgrade body: compare installed vs latest,
// re-install if outdated. All errors are ignored.
func silentUpgrade(ctx context.Context, name, binaryPath string) {
	m, ok := binaryMeta[name]
	if !ok || m.installed == nil || m.latest == nil {
		return
	}

	installedVer, err := m.installed(ctx, binaryPath)
	if err != nil || installedVer == "" {
		return
	}
	latestVer, err := m.latest(ctx)
	if err != nil || latestVer == "" {
		return
	}

	if normaliseVersion(installedVer) == normaliseVersion(latestVer) {
		return // already up to date
	}

	// Reinstall silently.
	m.install(ctx, name) //nolint:errcheck
}

// normaliseVersion strips a leading "v" and trims whitespace for comparison.
func normaliseVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

// ── version detection helpers ─────────────────────────────────────────────────

// versionFromArgs returns a versionFn that runs `<binary> <args...>` and
// extracts the first semver-like token from the combined output.
func versionFromArgs(args ...string) versionFn {
	return func(ctx context.Context, binaryPath string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binaryPath, args...) //nolint:gosec
		out, _ := cmd.CombinedOutput()                       // ignore exit code; many servers exit 0 or 1
		return extractSemver(string(out)), nil
	}
}

// semverRe matches the first vX.Y.Z or X.Y.Z token in a string.
var semverRe = regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)

// extractSemver returns the first semver-like token found in s, or "".
func extractSemver(s string) string {
	m := semverRe.FindString(s)
	return strings.TrimPrefix(m, "v")
}

// ── latest-version helpers ────────────────────────────────────────────────────

// latestGitHubRelease returns a latestFn that queries the GitHub Releases API
// for the latest release tag of owner/repo.
func latestGitHubRelease(owner, repo string) latestFn {
	return func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		var payload struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", err
		}
		return normaliseVersion(payload.TagName), nil
	}
}

// latestGoModule returns a latestFn that runs `go list -m -json <module>@latest`
// to find the latest published version.
func latestGoModule(module string) latestFn {
	return func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "list", "-m", "-json", module+"@latest") //nolint:gosec
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		var mod struct {
			Version string `json:"Version"`
		}
		if err := json.Unmarshal(out, &mod); err != nil {
			return "", err
		}
		return normaliseVersion(mod.Version), nil
	}
}

// latestNpmPackage returns a latestFn that runs `npm view <pkg> version`.
func latestNpmPackage(pkg string) latestFn {
	return func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "npm", "view", pkg, "version") //nolint:gosec
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
}

// latestPipPackage returns a latestFn that queries the PyPI JSON API for the
// latest version of pkg.
func latestPipPackage(pkg string) latestFn {
	return func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		url := fmt.Sprintf("https://pypi.org/pypi/%s/json", pkg)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, resp.Body); err != nil {
			return "", err
		}
		var payload struct {
			Info struct {
				Version string `json:"version"`
			} `json:"info"`
		}
		if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
			return "", err
		}
		return payload.Info.Version, nil
	}
}
