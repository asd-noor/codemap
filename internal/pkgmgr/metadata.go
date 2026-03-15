package pkgmgr

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// LSPMetadata defines version and download information for an LSP server.
type LSPMetadata struct {
	Name            string
	Version         string            // Used as fallback if version resolution fails
	BinaryName      string            // name of the executable in the archive
	DownloadURLs    map[string]string // platform -> download URL template (use {version} placeholder)
	Checksums       map[string]string // platform -> SHA256 checksum
	IsArchive       bool              // whether download is an archive (tar.gz/zip)
	ArchivePath     string            // path to binary within archive (if applicable)
	VersionResolver VersionResolver   // Optional: resolver for fetching latest version dynamically
}

// GetLSPMetadata returns metadata for a given language's LSP server.
// It resolves the latest version dynamically if a VersionResolver is configured.
func GetLSPMetadata(lang string) (*LSPMetadata, error) {
	metadata, ok := lspMetadata[lang]
	if !ok {
		return nil, fmt.Errorf("no metadata for language: %s", lang)
	}

	// Clone metadata to avoid modifying the original
	resolved := &LSPMetadata{
		Name:            metadata.Name,
		Version:         metadata.Version,
		BinaryName:      metadata.BinaryName,
		DownloadURLs:    make(map[string]string),
		Checksums:       metadata.Checksums,
		IsArchive:       metadata.IsArchive,
		ArchivePath:     metadata.ArchivePath,
		VersionResolver: metadata.VersionResolver,
	}

	// Resolve latest version if resolver is configured
	if metadata.VersionResolver != nil {
		ctx := context.Background()
		latestVersion, err := metadata.VersionResolver.ResolveLatestVersion(ctx)
		if err != nil {
			log.Printf("[%s] Warning: failed to resolve latest version, using fallback %s: %v",
				lang, metadata.Version, err)
		} else {
			resolved.Version = latestVersion
			log.Printf("[%s] Resolved latest version: %s", lang, latestVersion)
		}
	}

	// Substitute {version} in download URLs
	for platform, urlTemplate := range metadata.DownloadURLs {
		resolved.DownloadURLs[platform] = strings.ReplaceAll(urlTemplate, "{version}", resolved.Version)
	}

	return resolved, nil
}

// LSP metadata with dynamic version resolution
var lspMetadata = map[string]*LSPMetadata{
	"go": {
		Name:       "gopls",
		Version:    "v0.21.1", // Fallback version
		BinaryName: "gopls",
		DownloadURLs: map[string]string{
			"linux-amd64":   "https://github.com/golang/tools/releases/download/gopls/{version}/gopls-{version}-linux-amd64.tar.gz",
			"linux-arm64":   "https://github.com/golang/tools/releases/download/gopls/{version}/gopls-{version}-linux-arm64.tar.gz",
			"darwin-amd64":  "https://github.com/golang/tools/releases/download/gopls/{version}/gopls-{version}-darwin-amd64.tar.gz",
			"darwin-arm64":  "https://github.com/golang/tools/releases/download/gopls/{version}/gopls-{version}-darwin-arm64.tar.gz",
			"windows-amd64": "https://github.com/golang/tools/releases/download/gopls/{version}/gopls-{version}-windows-amd64.zip",
		},
		Checksums: map[string]string{
			"linux-amd64":   "",
			"linux-arm64":   "",
			"darwin-amd64":  "",
			"darwin-arm64":  "",
			"windows-amd64": "",
		},
		IsArchive:       true,
		ArchivePath:     "gopls",
		VersionResolver: NewGitHubResolver("golang", "tools", ""),
	},
	"python": {
		Name:       "pyright",
		Version:    "1.1.408", // Fallback version
		BinaryName: "pyright-langserver",
		DownloadURLs: map[string]string{
			"linux-amd64":   "https://registry.npmjs.org/pyright/-/pyright-{version}.tgz",
			"linux-arm64":   "https://registry.npmjs.org/pyright/-/pyright-{version}.tgz",
			"darwin-amd64":  "https://registry.npmjs.org/pyright/-/pyright-{version}.tgz",
			"darwin-arm64":  "https://registry.npmjs.org/pyright/-/pyright-{version}.tgz",
			"windows-amd64": "https://registry.npmjs.org/pyright/-/pyright-{version}.tgz",
		},
		Checksums: map[string]string{
			"linux-amd64":   "",
			"linux-arm64":   "",
			"darwin-amd64":  "",
			"darwin-arm64":  "",
			"windows-amd64": "",
		},
		IsArchive:       true,
		ArchivePath:     "package/langserver.index.js",
		VersionResolver: NewNPMResolver("pyright"),
	},
	"typescript": {
		Name:       "typescript-language-server",
		Version:    "5.1.3", // Fallback version
		BinaryName: "typescript-language-server",
		DownloadURLs: map[string]string{
			"linux-amd64":   "https://registry.npmjs.org/typescript-language-server/-/typescript-language-server-{version}.tgz",
			"linux-arm64":   "https://registry.npmjs.org/typescript-language-server/-/typescript-language-server-{version}.tgz",
			"darwin-amd64":  "https://registry.npmjs.org/typescript-language-server/-/typescript-language-server-{version}.tgz",
			"darwin-arm64":  "https://registry.npmjs.org/typescript-language-server/-/typescript-language-server-{version}.tgz",
			"windows-amd64": "https://registry.npmjs.org/typescript-language-server/-/typescript-language-server-{version}.tgz",
		},
		Checksums: map[string]string{
			"linux-amd64":   "",
			"linux-arm64":   "",
			"darwin-amd64":  "",
			"darwin-arm64":  "",
			"windows-amd64": "",
		},
		IsArchive:       true,
		ArchivePath:     "package/lib/cli.mjs",
		VersionResolver: NewNPMResolver("typescript-language-server"),
	},
	"lua": {
		Name:       "lua-language-server",
		Version:    "3.17.1", // Fallback version
		BinaryName: "lua-language-server",
		DownloadURLs: map[string]string{
			"linux-amd64":   "https://github.com/LuaLS/lua-language-server/releases/download/{version}/lua-language-server-{version}-linux-x64.tar.gz",
			"linux-arm64":   "https://github.com/LuaLS/lua-language-server/releases/download/{version}/lua-language-server-{version}-linux-arm64.tar.gz",
			"darwin-amd64":  "https://github.com/LuaLS/lua-language-server/releases/download/{version}/lua-language-server-{version}-darwin-x64.tar.gz",
			"darwin-arm64":  "https://github.com/LuaLS/lua-language-server/releases/download/{version}/lua-language-server-{version}-darwin-arm64.tar.gz",
			"windows-amd64": "https://github.com/LuaLS/lua-language-server/releases/download/{version}/lua-language-server-{version}-win32-x64.zip",
		},
		Checksums: map[string]string{
			"linux-amd64":   "",
			"linux-arm64":   "",
			"darwin-amd64":  "",
			"darwin-arm64":  "",
			"windows-amd64": "",
		},
		IsArchive:       true,
		ArchivePath:     "bin/lua-language-server",
		VersionResolver: NewGitHubResolver("LuaLS", "lua-language-server", ""),
	},
	"zig": {
		Name:       "zls",
		Version:    "0.15.1", // Fallback version
		BinaryName: "zls",
		DownloadURLs: map[string]string{
			"linux-amd64":   "https://github.com/zigtools/zls/releases/download/{version}/zls-linux-x86_64-{version}.tar.gz",
			"linux-arm64":   "https://github.com/zigtools/zls/releases/download/{version}/zls-linux-aarch64-{version}.tar.gz",
			"darwin-amd64":  "https://github.com/zigtools/zls/releases/download/{version}/zls-macos-x86_64-{version}.tar.gz",
			"darwin-arm64":  "https://github.com/zigtools/zls/releases/download/{version}/zls-macos-aarch64-{version}.tar.gz",
			"windows-amd64": "https://github.com/zigtools/zls/releases/download/{version}/zls-windows-x86_64-{version}.zip",
		},
		Checksums: map[string]string{
			"linux-amd64":   "",
			"linux-arm64":   "",
			"darwin-amd64":  "",
			"darwin-arm64":  "",
			"windows-amd64": "",
		},
		IsArchive:       true,
		ArchivePath:     "zls",
		VersionResolver: NewGitHubResolver("zigtools", "zls", ""),
	},
	"templ": {
		Name:       "templ",
		Version:    "v0.3.1001", // Fallback version
		BinaryName: "templ",
		DownloadURLs: map[string]string{
			"linux-x86_64":   "https://github.com/a-h/templ/releases/download/{version}/templ_Linux_x86_64.tar.gz",
			"linux-arm64":    "https://github.com/a-h/templ/releases/download/{version}/templ_Linux_arm64.tar.gz",
			"darwin-x86_64":  "https://github.com/a-h/templ/releases/download/{version}/templ_Darwin_x86_64.tar.gz",
			"darwin-arm64":   "https://github.com/a-h/templ/releases/download/{version}/templ_Darwin_arm64.tar.gz",
			"windows-x86_64": "https://github.com/a-h/templ/releases/download/{version}/templ_Windows_x86_64.tar.gz",
			"windows-arm64":  "https://github.com/a-h/templ/releases/download/{version}/templ_Windows_arm64.tar.gz",
		},
		Checksums: map[string]string{
			"linux-x86_64":   "",
			"linux-arm64":    "",
			"darwin-x86_64":  "",
			"darwin-arm64":   "",
			"windows-x86_64": "",
			"windows-arm64":  "",
		},
		IsArchive:       true,
		ArchivePath:     "templ",
		VersionResolver: NewGitHubResolver("a-h", "templ", ""),
	},
}

// GetLanguageByBinaryName maps a binary name back to its language identifier.
func GetLanguageByBinaryName(binaryName string) string {
	for lang, meta := range lspMetadata {
		if meta.BinaryName == binaryName {
			return lang
		}
	}
	return ""
}
