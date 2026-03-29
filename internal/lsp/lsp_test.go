package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"codemap/internal/graph"
	"codemap/internal/pkgmgr"
	"codemap/util"
)

// isCommandAvailable checks if a command is available in PATH.
func isCommandAvailable(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func TestLSP_BasicWorkflow(t *testing.T) {
	if !isCommandAvailable("gopls") {
		t.Skip("gopls not available, skipping LSP tests")
	}

	// Create test directory with Go code.
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.go")
	helperFile := filepath.Join(tmpDir, "helper.go")

	mainCode := `package main

func MainFunc() {
	Helper()
}

func AnotherFunc() {
	Helper()
}
`
	helperCode := `package main

func Helper() {
	// Does something
}
`
	if err := os.WriteFile(mainFile, []byte(mainCode), 0644); err != nil {
		t.Fatalf("Failed to write main.go: %v", err)
	}
	if err := os.WriteFile(helperFile, []byte(helperCode), 0644); err != nil {
		t.Fatalf("Failed to write helper.go: %v", err)
	}

	// Create LSP service.
	svc := NewService(tmpDir, pkgmgr.New())
	defer svc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Get the go client (lazily starts gopls).
	client, err := svc.getClient(ctx, "go")
	if err != nil {
		t.Fatalf("Failed to start gopls client: %v", err)
	}

	// Open the helper file.
	helperURI := util.PathToURI(helperFile)
	client.mu.Lock()
	if err := client.didOpen(helperURI, "go", helperCode); err != nil {
		client.mu.Unlock()
		t.Fatalf("Failed to open helper.go: %v", err)
	}
	client.mu.Unlock()

	// Test references for Helper function (line 3, col 5 in helperCode → 0-indexed: 2, 4).
	refs, err := client.references(ctx, helperURI, Position{Line: 2, Character: 5})
	if err != nil {
		t.Logf("GetReferences failed (gopls may still be indexing): %v", err)
	} else {
		t.Logf("Found %d references to Helper", len(refs))
	}

	// Open main file and close both.
	mainURI := util.PathToURI(mainFile)
	client.mu.Lock()
	client.didOpen(mainURI, "go", mainCode) //nolint:errcheck
	client.didClose(helperURI)              //nolint:errcheck
	client.didClose(mainURI)               //nolint:errcheck
	client.mu.Unlock()
}

func TestLSP_Enrich(t *testing.T) {
	if !isCommandAvailable("gopls") {
		t.Skip("gopls not available, skipping LSP tests")
	}

	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.go")
	helperFile := filepath.Join(tmpDir, "helper.go")

	mainCode := `package main

func MainFunc() {
	Helper()
}
`
	helperCode := `package main

func Helper() {}
`
	if err := os.WriteFile(mainFile, []byte(mainCode), 0644); err != nil {
		t.Fatalf("Failed to write main.go: %v", err)
	}
	if err := os.WriteFile(helperFile, []byte(helperCode), 0644); err != nil {
		t.Fatalf("Failed to write helper.go: %v", err)
	}

	// Open an in-memory store for the test.
	store, err := graph.Open(tmpDir)
	if err != nil {
		t.Fatalf("Failed to open store: %v", err)
	}
	defer store.Close()

	// Seed nodes.
	nodes := []graph.Node{
		{
			ID:        util.NodeID(mainFile, "MainFunc", "function"),
			Name:      "MainFunc",
			Kind:      "function",
			FilePath:  mainFile,
			LineStart: 3, LineEnd: 5,
			ColStart: 1, ColEnd: 1,
			NameLine: 3, NameCol: 6,
		},
		{
			ID:        util.NodeID(helperFile, "Helper", "function"),
			Name:      "Helper",
			Kind:      "function",
			FilePath:  helperFile,
			LineStart: 3, LineEnd: 3,
			ColStart: 1, ColEnd: 16,
			NameLine: 3, NameCol: 6,
		},
	}

	ctx := context.Background()
	if err := store.BulkUpsertNodes(ctx, nodes); err != nil {
		t.Fatalf("Failed to seed nodes: %v", err)
	}

	svc := NewService(tmpDir, pkgmgr.New())
	defer svc.Close()

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	edges, err := svc.Enrich(runCtx, nodes, store)
	if err != nil {
		t.Fatalf("Enrich failed: %v", err)
	}

	t.Logf("Found %d edges", len(edges))
	for _, e := range edges {
		t.Logf("Edge: %s --%s--> %s", e.SourceID, e.Relation, e.TargetID)
	}
}

func TestExtToLangID(t *testing.T) {
	tests := []struct {
		ext  string
		want string
		ok   bool
	}{
		{".go", "go", true},
		{".py", "python", true},
		{".js", "javascript", true},
		{".ts", "typescript", true},
		{".tsx", "typescript", true},
		{".jsx", "javascript", true},
		{".lua", "lua", true},
		{".zig", "zig", true},
		{".txt", "", false},
	}
	for _, tt := range tests {
		got, ok := extToLangID[tt.ext]
		if ok != tt.ok || got != tt.want {
			t.Errorf("extToLangID[%q] = %q, %v; want %q, %v", tt.ext, got, ok, tt.want, tt.ok)
		}
	}
}
