package main

import (
	"context"
	_ "embed"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"codemap/internal/daemon"
	"codemap/internal/db"
	"codemap/internal/server"
	"codemap/util"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed SYSTEM_PROMPT.md
var systemPrompt string

func main() {
	projectDir := flag.String("project-dir", "", "Project directory to index (default: auto-detected git root)")
	daemonMode := flag.Bool("daemon", false, "Run in foreground daemon mode (internal use)")
	watchMode := flag.Bool("watch", false, "Start a background watcher daemon and exit")
	flag.Parse()

	// Resolve project root.
	startDir := "."
	if *projectDir != "" {
		abs, err := filepath.Abs(*projectDir)
		if err != nil {
			log.Fatalf("Failed to resolve project directory: %v", err)
		}
		startDir = abs
	}
	rootDir := util.FindGitRoot(startDir)

	// ── watch mode: spawn background daemon and exit ──────────────────────────
	if *watchMode {
		pidPath, err := db.PIDPath(rootDir)
		if err != nil {
			log.Fatalf("Failed to resolve PID path: %v", err)
		}
		if daemon.IsAlive(pidPath) {
			log.Printf("Watcher daemon already running (PIDFILE: %s)", pidPath)
			return
		}
		exe, err := os.Executable()
		if err != nil {
			log.Fatalf("Failed to resolve executable path: %v", err)
		}
		if err := daemon.Spawn(exe, rootDir); err != nil {
			log.Fatalf("Failed to spawn daemon: %v", err)
		}
		log.Printf("Watcher daemon started (PIDFILE: %s)", pidPath)
		return
	}

	// ── daemon mode: write PID, run watcher + index, auto-stop on idle ───────
	if *daemonMode {
		pidPath, err := db.PIDPath(rootDir)
		if err != nil {
			log.Fatalf("Failed to resolve PID path: %v", err)
		}
		if daemon.IsAlive(pidPath) {
			return // already running
		}
		if err := daemon.WritePID(pidPath); err != nil {
			log.Fatalf("Failed to write PID file: %v", err)
		}
		defer daemon.RemovePID(pidPath)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)

		srv, err := server.NewWatch(ctx, cancel, rootDir, systemPrompt)
		if err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
		defer srv.Close()

		// Block until idle timeout fires (cancel called by watcher) or signal.
		<-ctx.Done()
		return
	}

	// ── normal MCP server mode ────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := server.New(sigCtx, rootDir, systemPrompt)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Close()

	// Serve MCP over stdio.
	mcpSrv := srv.MCPServer()
	if err := mcpSrv.Run(sigCtx, &mcp.StdioTransport{}); err != nil && sigCtx.Err() == nil {
		log.Fatalf("MCP server error: %v", err)
	}
}
