package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"codemap/internal/daemon"
	"codemap/internal/server"
	"codemap/internal/ui"
	"codemap/util"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed SYSTEM_PROMPT.md
var systemPrompt string

func main() {
	var (
		projectDir string
		dbDir      string
		dbName     string
	)

	root := &cobra.Command{
		Use:   "codemap",
		Short: "Code Exploration Engine — MCP server and CLI",
		Long: `codemap indexes your codebase with Tree-sitter and LSP, stores a semantic
graph in SQLite, and exposes it both as an MCP server (for AI agents) and as a
human-friendly CLI.

		All sub-commands share --project-dir, --db-dir, and --db-name:

		  codemap [--project-dir DIR] [--db-dir DIR] [--db-name NAME] serve
		  codemap [--project-dir DIR] [--db-dir DIR] [--db-name NAME] watch
		  codemap [--project-dir DIR] [--db-dir DIR] [--db-name NAME] index
		  codemap [--project-dir DIR] [--db-dir DIR] [--db-name NAME] status
		  codemap [--project-dir DIR] [--db-dir DIR] [--db-name NAME] symbols <file>
		  codemap [--project-dir DIR] [--db-dir DIR] [--db-name NAME] symbol  <name>
		  codemap [--project-dir DIR] [--db-dir DIR] [--db-name NAME] impact  <name>
		  codemap [--project-dir DIR] [--db-dir DIR] [--db-name NAME] diagnostics`,
		// Default action when no sub-command is given: run as MCP server.
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := resolveStorePaths(projectDir, dbDir, dbName)
			return runServe(projectDir, dbPath)
		},
	}

	root.PersistentFlags().StringVar(&projectDir, "project-dir", "", "Project directory (default: auto-detected git root from CWD)")
	root.PersistentFlags().StringVar(&dbDir, "db-dir", "", "Database directory override. If set, DB is at <db-dir>/codemap.sqlite; default is <project-dir>/.codemap")
	root.PersistentFlags().StringVar(&dbName, "db-name", "codemap", "Database file name stem used with --db-dir")

	root.AddCommand(
		newServeCmd(&projectDir, &dbDir, &dbName),
		newWatchCmd(&projectDir, &dbDir, &dbName),
		newIndexCmd(&projectDir, &dbDir, &dbName),
		newStatusCmd(&projectDir, &dbDir, &dbName),
		newSymbolsCmd(&projectDir, &dbDir, &dbName),
		newSymbolCmd(&projectDir, &dbDir, &dbName),
		newImpactCmd(&projectDir, &dbDir, &dbName),
		newDiagnosticsCmd(&projectDir, &dbDir, &dbName),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// resolveStorePaths computes the SQLite file path and PID file path from flags.
//
//	--db-dir not set  →  dbPath = <project-dir>/.codemap        pidPath = <project-dir>/.codemap.pid
//	--db-dir=<d>      →  dbPath = <d>/<db-name>.sqlite          pidPath = <d>/<db-name>.pid
func resolveStorePaths(projectDir, dbDirFlag, dbName string) (dbPath, pidPath string) {
	if dbDirFlag == "" {
		base := resolveProjectDir(projectDir)
		return filepath.Join(base, ".codemap"),
			filepath.Join(base, ".codemap.pid")
	}
	abs, err := filepath.Abs(dbDirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codemap: failed to resolve --db-dir: %v\n", err)
		os.Exit(1)
	}
	if dbName == "" {
		dbName = "codemap"
	}
	return filepath.Join(abs, dbName+".sqlite"),
		filepath.Join(abs, dbName+".pid")
}

// resolveProjectDir returns the absolute project root directory.
func resolveProjectDir(projectDir string) string {
	if projectDir != "" {
		abs, err := filepath.Abs(projectDir)
		if err == nil {
			return abs
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// rootDir returns the git root for the given project dir flag.
func rootDir(projectDir string) string {
	return util.FindGitRoot(resolveProjectDir(projectDir))
}

// ── serve ──────────────────────────────────────────────────────────────────────────

func newServeCmd(projectDir, dbDir, dbName *string) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run as an MCP server over stdio (default when no sub-command is given)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := resolveStorePaths(*projectDir, *dbDir, *dbName)
			return runServe(*projectDir, dbPath)
		},
	}
}

func runServe(projectDir, dbPath string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ui.Banner()
	ui.StatusStarting(rootDir(projectDir), dbPath)

	// Setup visualization callback with spinner
	spinner := ui.NewSpinner()
	var (
		mu      sync.Mutex
		lastIdx int
	)

	callback := func(nodes, edges int, elapsed time.Duration) {
		mu.Lock()
		defer mu.Unlock()

		if edges == 0 && nodes > lastIdx {
			// During scanning phase
			lastIdx = nodes
			ui.StatusIndexing(spinner.Frame())
		} else if edges > 0 {
			// After enrichment phase
			if edges > lastIdx {
				ui.StatusIndexed(nodes, edges)
				lastIdx = edges
			}
		}
	}

	srv, err := server.NewWithCallback(sigCtx, rootDir(projectDir), dbPath, systemPrompt, callback)
	if err != nil {
		ui.StatusFailed(err)
		return fmt.Errorf("serve: %w", err)
	}
	defer srv.Close()

	ui.StatusReady()

	if err := srv.MCPServer().Run(sigCtx, &mcp.StdioTransport{}); err != nil && sigCtx.Err() == nil {
		return fmt.Errorf("serve: mcp: %w", err)
	}
	return nil
}

// ── watch ──────────────────────────────────────────────────────────────────────

func newWatchCmd(projectDir, dbDir, dbName *string) *cobra.Command {
	var daemonMode bool
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Start the codemap daemon in the background (auto-stops after 5 min of inactivity)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, pidPath := resolveStorePaths(*projectDir, *dbDir, *dbName)
			if daemonMode {
				return runWatchDaemon(*projectDir, dbPath, pidPath)
			}
			return runWatch(*projectDir, dbPath, pidPath)
		},
	}
	cmd.Flags().BoolVar(&daemonMode, "daemon", false, "Run in foreground daemon mode (internal use)")
	cmd.Flags().MarkHidden("daemon") //nolint:errcheck
	return cmd
}

func runWatch(projectDir, dbPath, pidPath string) error {
	if daemon.IsAlive(pidPath) {
		ui.StatusDaemonAlreadyRunning(pidPath)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("codemap watch: resolve executable: %w", err)
	}
	if err := daemon.Spawn(exe, rootDir(projectDir)); err != nil {
		return fmt.Errorf("codemap watch: %w", err)
	}
	ui.StatusWatcherStarted(pidPath)
	return nil
}

func runWatchDaemon(projectDir, dbPath, pidPath string) error {
	if daemon.IsAlive(pidPath) {
		return nil
	}
	if err := daemon.WritePID(pidPath); err != nil {
		return fmt.Errorf("codemap watch: write pid: %w", err)
	}
	defer daemon.RemovePID(pidPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := server.NewWatch(ctx, cancel, rootDir(projectDir), dbPath, systemPrompt)
	if err != nil {
		return fmt.Errorf("codemap watch: %w", err)
	}
	defer srv.Close()

	<-ctx.Done()
	return nil
}

// ── index ──────────────────────────────────────────────────────────────────────

func newIndexCmd(projectDir, dbDir, dbName *string) *cobra.Command {
	return &cobra.Command{
		Use:   "index",
		Short: "Build or rebuild the symbol index",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := resolveStorePaths(*projectDir, *dbDir, *dbName)
			return runIndex(*projectDir, dbPath)
		},
	}
}

func runIndex(projectDir, dbPath string) error {
	ctx := context.Background()

	ui.Banner()
	ui.StatusStarting(rootDir(projectDir), dbPath)

	srv, err := server.New(ctx, rootDir(projectDir), dbPath, "")
	if err != nil {
		ui.StatusFailed(err)
		return err
	}
	defer srv.Close()

	if err := srv.ForceIndex(ctx); err != nil {
		ui.StatusFailed(err)
		return err
	}
	if err := srv.WaitForIndex(ctx); err != nil {
		ui.StatusFailed(err)
		return err
	}

	nodes, err := srv.Store().NodeCount(ctx)
	if err != nil {
		return err
	}
	edges, err := srv.Store().EdgeCount(ctx)
	if err != nil {
		return err
	}

	ui.StatusIndexed(nodes, edges)
	return nil
}

// ── daemon.Spawn args ─────────────────────────────────────────────────────────
// Spawn passes: exe, "watch", "--project-dir", root, "--daemon"
// which matches the hidden --daemon flag on the watch sub-command above.
