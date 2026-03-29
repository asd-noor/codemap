package tests

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"codemap/internal/graph"
	"codemap/internal/lsp"
	"codemap/internal/pkgmgr"
	"codemap/internal/scanner"
)

func TestIntegration_LSPEnrichmentWithAbsolutePaths(t *testing.T) {
	if !isGoplsAvailable() {
		t.Skip("gopls not available, skipping LSP enrichment test")
	}

	// 1. Setup Temp Workspace with Go Code.
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

	// 2. Open store.
	store, err := graph.Open(filepath.Join(wsDir, "test.db"))
	if err != nil {
		t.Fatalf("Failed to open store: %v", err)
	}
	defer store.Close()

	// 3. Init Scanner and scan.
	scn := scanner.New(wsDir)
	nodes, err := scn.Scan(context.Background(), wsDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("Expected nodes from scan, got 0")
	}

	// 4. Verify nodes have absolute paths.
	for _, n := range nodes {
		if !filepath.IsAbs(n.FilePath) {
			t.Errorf("Node %s has relative path: %s", n.Name, n.FilePath)
		}
		if _, err := os.Stat(n.FilePath); err != nil {
			t.Errorf("Node %s points to non-existent file: %s", n.Name, n.FilePath)
		}
	}
	t.Logf("Found %d nodes with absolute paths", len(nodes))

	// 5. Store Nodes.
	if err := store.BulkUpsertNodes(context.Background(), nodes); err != nil {
		t.Fatalf("BulkUpsertNodes failed: %v", err)
	}

	// 6. Run LSP Enrichment.
	lspSvc := lsp.NewService(wsDir, pkgmgr.New())
	defer lspSvc.Close()

	edges, err := lspSvc.Enrich(context.Background(), nodes, store)
	if err != nil {
		t.Fatalf("Enrich failed: %v", err)
	}
	t.Logf("LSP enrichment produced %d edges", len(edges))

	// 7. Store Edges.
	if err := store.BulkUpsertEdges(context.Background(), edges); err != nil {
		t.Fatalf("BulkUpsertEdges failed: %v", err)
	}

	// 8. Verify query by absolute path.
	mainFile, _ = filepath.Abs(mainFile)
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
