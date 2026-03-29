// Package pkgmgr — metadata.go
// Per-language-server install metadata.
package pkgmgr

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// installFn is a function that installs a binary and returns its path.
type installFn func(ctx context.Context, name string) (string, error)

// versionFn returns the currently installed version of a binary given its resolved path.
type versionFn func(ctx context.Context, binaryPath string) (string, error)

// latestFn returns the latest available version string for a binary.
type latestFn func(ctx context.Context) (string, error)

// meta bundles install information for a single binary.
type meta struct {
	install   installFn
	installed versionFn // detect currently installed version
	latest    latestFn  // fetch latest available version
}

// binaryMeta maps binary names to their install metadata.
var binaryMeta = map[string]meta{
	"gopls": {
		install:   goInstall("golang.org/x/tools/gopls@latest"),
		installed: versionFromArgs("version"),
		latest:    latestGoModule("golang.org/x/tools/gopls"),
	},
	"pylsp": {
		install:   pipInstall("python-lsp-server"),
		installed: versionFromArgs("--version"),
		latest:    latestPipPackage("python-lsp-server"),
	},
	"typescript-language-server": {
		install:   npmInstall("typescript-language-server", "typescript"),
		installed: versionFromArgs("--version"),
		latest:    latestNpmPackage("typescript-language-server"),
	},
	"lua-language-server": {
		install:   luaLSInstall(),
		installed: versionFromArgs("--version"),
		latest:    latestGitHubRelease("LuaLS", "lua-language-server"),
	},
	"zls": {
		install:   zlsInstall(),
		installed: versionFromArgs("--version"),
		latest:    latestGitHubRelease("zigtools", "zls"),
	},
	"templ": {
		install:   goInstall("github.com/a-h/templ/cmd/templ@latest"),
		installed: versionFromArgs("version"),
		latest:    latestGoModule("github.com/a-h/templ/cmd/templ"),
	},
}

// Install installs a binary by name and returns the path to the installed binary.
func Install(ctx context.Context, name string) (string, error) {
	m, ok := binaryMeta[name]
	if !ok {
		return "", fmt.Errorf("pkgmgr: no install recipe for %q", name)
	}
	return m.install(ctx, name)
}

// goInstall returns an installFn that runs `go install <pkg>`.
func goInstall(pkg string) installFn {
	return func(ctx context.Context, name string) (string, error) {
		if err := runCmdCtx(ctx, "go", "install", pkg); err != nil {
			return "", fmt.Errorf("go install %s: %w", pkg, err)
		}
		path, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("go install %s succeeded but binary not found: %w", pkg, err)
		}
		return path, nil
	}
}

// pipInstall returns an installFn that runs `pip install <pkg>`.
func pipInstall(pkg string) installFn {
	return func(ctx context.Context, name string) (string, error) {
		pip := "pip3"
		if runtime.GOOS == "windows" {
			pip = "pip"
		}
		if err := runCmdCtx(ctx, pip, "install", pkg); err != nil {
			return "", fmt.Errorf("pip install %s: %w", pkg, err)
		}
		path, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("pip install %s succeeded but binary not found: %w", pkg, err)
		}
		return path, nil
	}
}

// npmInstall returns an installFn that runs `npm install -g <pkgs...>`.
func npmInstall(pkgs ...string) installFn {
	return func(ctx context.Context, name string) (string, error) {
		args := append([]string{"install", "-g"}, pkgs...)
		if err := runCmdCtx(ctx, "npm", args...); err != nil {
			return "", fmt.Errorf("npm install %s: %w", strings.Join(pkgs, " "), err)
		}
		path, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("npm install succeeded but binary not found: %w", err)
		}
		return path, nil
	}
}

// luaLSInstall returns an installFn that downloads lua-language-server from GitHub releases.
func luaLSInstall() installFn {
	const releaseURL = "https://github.com/LuaLS/lua-language-server/releases"
	return func(ctx context.Context, name string) (string, error) {
		var version string
		if lf := latestGitHubRelease("LuaLS", "lua-language-server"); lf != nil {
			if v, err := lf(ctx); err == nil {
				version = v
			}
		}
		url := luaLSDownloadURL(version)
		if url == "" {
			return "", fmt.Errorf(
				"pkgmgr: cannot auto-install %s on %s/%s — please install manually: %s",
				name, runtime.GOOS, runtime.GOARCH, releaseURL,
			)
		}
		return downloadBinary(ctx, name, url)
	}
}

// zlsInstall returns an installFn that downloads ZLS from GitHub releases.
func zlsInstall() installFn {
	return func(ctx context.Context, _ string) (string, error) {
		var version string
		if lf := latestGitHubRelease("zigtools", "zls"); lf != nil {
			if v, err := lf(ctx); err == nil {
				version = v
			}
		}
		url := zlsDownloadURL(version)
		if url == "" {
			return "", fmt.Errorf(
				"pkgmgr: cannot auto-install zls on %s/%s — please install manually: https://github.com/zigtools/zls/releases",
				runtime.GOOS, runtime.GOARCH,
			)
		}
		return downloadBinary(ctx, "zls", url)
	}
}

// luaLSDownloadURL returns a direct download URL for lua-language-server releases.
func luaLSDownloadURL(version string) string {
	const fallback = "3.7.4"
	if version == "" {
		version = fallback
	}
	osMap := map[string]string{"darwin": "darwin", "linux": "linux", "windows": "win32"}
	archMap := map[string]string{"amd64": "x64", "arm64": "arm64"}
	osStr, ok1 := osMap[runtime.GOOS]
	archStr, ok2 := archMap[runtime.GOARCH]
	if !ok1 || !ok2 {
		return ""
	}
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf(
		"https://github.com/LuaLS/lua-language-server/releases/download/%s/lua-language-server-%s-%s-%s.%s",
		version, version, osStr, archStr, ext,
	)
}

// zlsDownloadURL returns a direct download URL for ZLS releases.
func zlsDownloadURL(version string) string {
	const fallback = "0.15.1"
	if version == "" {
		version = fallback
	}
	osMap := map[string]string{"darwin": "macos", "linux": "linux", "windows": "windows"}
	archMap := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}
	osStr, ok1 := osMap[runtime.GOOS]
	archStr, ok2 := archMap[runtime.GOARCH]
	if !ok1 || !ok2 {
		return ""
	}
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf(
		"https://github.com/zigtools/zls/releases/download/%s/zls-%s-%s-%s.%s",
		version, osStr, archStr, version, ext,
	)
}

// downloadBinary fetches url (a .tar.gz or .zip archive), extracts the named
// binary from it, writes it to the install dir, and makes it executable.
func downloadBinary(ctx context.Context, name, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("pkgmgr: download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("pkgmgr: download %s: HTTP %d", url, resp.StatusCode)
	}

	// Write the archive to a temp file.
	archiveTmp, err := os.CreateTemp("", "codemap-"+name+"-archive-*")
	if err != nil {
		return "", err
	}
	archiveName := archiveTmp.Name()
	defer os.Remove(archiveName) //nolint:errcheck

	if _, err := io.Copy(archiveTmp, resp.Body); err != nil {
		archiveTmp.Close()
		return "", fmt.Errorf("pkgmgr: write archive: %w", err)
	}
	archiveTmp.Close()

	dest, err := binaryPath(name)
	if err != nil {
		return "", err
	}

	// Extract based on URL suffix.
	if strings.HasSuffix(url, ".tar.gz") || strings.HasSuffix(url, ".tgz") {
		if err := extractTarGz(archiveName, name, dest); err != nil {
			return "", fmt.Errorf("pkgmgr: extract tar.gz: %w", err)
		}
	} else if strings.HasSuffix(url, ".zip") {
		if err := extractZip(archiveName, name, dest); err != nil {
			return "", fmt.Errorf("pkgmgr: extract zip: %w", err)
		}
	} else {
		return "", fmt.Errorf("pkgmgr: unsupported archive format for %s", url)
	}

	return dest, nil
}

// extractTarGz extracts the first file named `binaryName` from a .tar.gz archive.
func extractTarGz(archivePath, binaryName, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != binaryName {
			continue
		}
		return writeExecutable(tr, destPath)
	}
	return fmt.Errorf("binary %q not found in archive", binaryName)
}

// extractZip extracts the first file named `binaryName` from a .zip archive.
func extractZip(archivePath, binaryName, destPath string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if filepath.Base(f.Name) != binaryName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeExecutable(rc, destPath)
	}
	return fmt.Errorf("binary %q not found in archive", binaryName)
}

// writeExecutable writes r to destPath with mode 0755, using a temp file + rename for atomicity.
func writeExecutable(r io.Reader, destPath string) error {
	tmp, err := os.CreateTemp(filepath.Dir(destPath), "codemap-bin-*")
	if err != nil {
		tmp, err = os.CreateTemp("", "codemap-bin-*")
		if err != nil {
			return err
		}
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return copyFile(tmpName, destPath)
	}
	return nil
}

// copyFile copies src to dst with mode 0755.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// runCmdCtx runs an external command, discarding output.
func runCmdCtx(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}
