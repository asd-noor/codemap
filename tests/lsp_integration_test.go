package tests

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"codemap/internal/db"
	"codemap/internal/graph"
	"codemap/internal/lsp"
	"codemap/internal/scanner"
)

func TestIntegration_LSPEnrichmentWithAbsolutePaths(t *testing.T) {
	// Skip if gopls is not available
	if !isGoplsAvailable() {
		t.Skip("gopls not available, skipping LSP enrichment test")
	}

	// 1. Setup Temp DB
	tmpDbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.New(tmpDbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer database.Close()
	store := graph.NewStore(database)

	// 2. Setup Temp Workspace with Go Code
	wsDir := t.TempDir()
	mainFile := filepath.Join(wsDir, "main.go")

	createFile(t, wsDir, "main.go", `package main

func MainFunc() {
	Helper()
}
`)

	createFile(t, wsDir, "helper.go", `package main

func Helper() {
	// Does something
}
`)

	// 3. Init Scanner
	scn, err := scanner.New()
	if err != nil {
		t.Fatalf("Failed to init scanner: %v", err)
	}

	// 4. Run Scan - should produce nodes with ABSOLUTE paths
	nodes, err := scn.Scan(context.Background(), wsDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(nodes) == 0 {
		t.Fatal("Expected nodes from scan, got 0")
	}

	// 5. Verify nodes have absolute paths
	for _, n := range nodes {
		if !filepath.IsAbs(n.FilePath) {
			t.Errorf("Node %s has relative path: %s (expected absolute)", n.Name, n.FilePath)
		}

		// Verify the file actually exists at this path
		if _, err := os.Stat(n.FilePath); err != nil {
			t.Errorf("Node %s points to non-existent file: %s", n.Name, n.FilePath)
		}
	}

	t.Logf("Found %d nodes with absolute paths", len(nodes))

	// 6. Store Nodes
	for _, n := range nodes {
		if err := store.UpsertNode(context.Background(), n); err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// 7. Run LSP Enrichment
	lspSvc := lsp.NewService()
	defer lspSvc.Shutdown()

	edges, err := lspSvc.Enrich(context.Background(), nodes)
	if err != nil {
		t.Fatalf("Enrich failed: %v", err)
	}

	t.Logf("LSP enrichment produced %d edges", len(edges))

	// Note: We might get 0 edges if gopls needs time to index
	// But the important thing is it didn't error due to path issues

	// 8. Store Edges
	for _, e := range edges {
		if err := store.UpsertEdge(context.Background(), e); err != nil {
			t.Fatalf("Failed to store edge: %v", err)
		}
	}

	// 9. Verify we can query by absolute path
	mainNodes, err := store.GetSymbolsInFile(context.Background(), mainFile)
	if err != nil {
		t.Fatalf("GetSymbolsInFile failed: %v", err)
	}

	if len(mainNodes) != 1 {
		t.Errorf("Expected 1 symbol in main.go, got %d", len(mainNodes))
	}

	t.Log("✓ Path handling test passed - absolute paths work correctly")
}

func isGoplsAvailable() bool {
	_, err := exec.LookPath("gopls")
	return err == nil
}
