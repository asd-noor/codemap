package main

import (
	"context"
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
	dbName := "codegraph.sqlite"
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

	// 6. Run initial index
	log.Printf("Indexing workspace: %s", cwd)
	nodes, err := scn.Scan(ctx, cwd)
	if err != nil {
		log.Fatalf("Initial scan failed: %v", err)
	}

	// COLLECT VALID FILES
	validFiles := make(map[string]bool)
	var validFileList []string
	for _, n := range nodes {
		if !validFiles[n.FilePath] {
			validFiles[n.FilePath] = true
			validFileList = append(validFileList, n.FilePath)
		}
	}

	if err := store.BulkUpsertNodes(ctx, nodes); err != nil {
		log.Printf("Failed to store nodes: %v", err)
	}

	// PRUNE STALE DATA
	if err := store.PruneStaleFiles(ctx, validFileList); err != nil {
		log.Printf("Warning: Failed to prune stale files: %v", err)
	}

	edges, err := lspSvc.Enrich(ctx, nodes, store)
	if err != nil {
		log.Fatalf("LSP enrichment failed: %v", err)
	}

	if err := store.BulkUpsertEdges(ctx, edges); err != nil {
		log.Printf("Failed to store edges: %v", err)
	}

	log.Printf("Initial index complete: %d nodes, %d edges", len(nodes), len(edges))

	// 7. Start file watcher in background
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

	// 8. Start MCP Server (blocks until shutdown)
	srv := server.New(scn, store, lspSvc)

	log.Println("Starting MCP server on stdio...")

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
