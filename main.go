package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"codemap/internal/db"
	"codemap/internal/graph"
	"codemap/internal/lsp"
	"codemap/internal/scanner"
	"codemap/internal/server"
	"codemap/internal/watcher"
	"codemap/util"
)

//go:embed SYSTEM_PROMPT.md
var systemPrompt string

func main() {
	projectDir := flag.String("project-dir", "", "Project directory to index (default: current working directory)")
	flag.Parse()

	if *projectDir != "" {
		absProjectDir, err := filepath.Abs(*projectDir)
		if err != nil {
			log.Fatalf("Failed to resolve project directory: %v", err)
		}
		info, err := os.Stat(absProjectDir)
		if err != nil {
			log.Fatalf("Failed to access project directory: %v", err)
		}
		if !info.IsDir() {
			log.Fatalf("Project directory is not a directory: %s", absProjectDir)
		}
		if err := os.Chdir(absProjectDir); err != nil {
			log.Fatalf("Failed to change to project directory: %v", err)
		}
	}

	// 1. Setup DB
	// Try to find git root for project-specific DB
	projectRoot, err := util.FindGitRoot()
	dbDir := ".ctxhub"
	dbName := "codemap.sqlite"
	var dbPath string
	if err == nil && projectRoot != "" {
		dbPath = filepath.Join(projectRoot, dbDir, dbName)
	} else {
		// Fallback to CWD
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get working directory: %v", err)
		}
		dbPath = filepath.Join(cwd, dbDir, dbName)
	}

	database, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("Failed to init DB at %s: %v", dbPath, err)
	}
	defer database.Close()

	store := graph.NewStore(database)

	// 2. Setup Scanner
	scn, err := scanner.New()
	if err != nil {
		log.Fatalf("Failed to init scanner: %v", err)
	}

	// 3. Setup LSP
	lspSvc := lsp.NewService()
	defer lspSvc.Shutdown()

	// 4. Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, cleaning up...")
		cancel()
	}()

	// 5. Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get working directory: %v", err)
	}

	// 6. Start MCP Server
	srv := server.New(scn, store, lspSvc, systemPrompt)

	log.Println("Starting MCP server on stdio...")

	// 7. Run initial index in background
	go func() {
		log.Printf("Starting background indexing of workspace: %s", cwd)
		srv.RunInitialIndex(ctx, cwd)
		status, indexErr, duration := srv.GetIndexStatus()
		if indexErr != nil {
			log.Printf("Background indexing failed after %.2fs: %v", duration.Seconds(), indexErr)
		} else if status == "ready" {
			log.Printf("Background indexing completed successfully in %.2fs", duration.Seconds())
		}
	}()

	// 8. Start file watcher in background
	w, err := watcher.New(scn, store, lspSvc, cwd)
	if err != nil {
		log.Fatalf("Failed to create watcher: %v", err)
	}
	defer w.Close()

	log.Printf("Watching %s for file changes...", cwd)

	// Start watcher in background goroutine
	watcherErrChan := make(chan error, 1)
	go func() {
		if err := w.Watch(ctx); err != nil && err != context.Canceled {
			watcherErrChan <- fmt.Errorf("watcher error: %w", err)
		}
	}()

	// 9. Run MCP Server (blocks until shutdown)
	log.Println("MCP server ready to accept connections")

	// Run server in goroutine so we can handle watcher errors
	serverErrChan := make(chan error, 1)
	go func() {
		if err := srv.Run(ctx); err != nil && err != context.Canceled {
			serverErrChan <- fmt.Errorf("server error: %w", err)
		}
	}()

	// Wait for either server error, watcher error, or context cancellation
	select {
	case err := <-serverErrChan:
		log.Fatalf("Server error: %v", err)
	case err := <-watcherErrChan:
		log.Fatalf("Watcher error: %v", err)
	case <-ctx.Done():
		log.Println("Shutting down gracefully...")
	}
}
