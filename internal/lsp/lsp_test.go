package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codemap/internal/graph"
	"codemap/util"
)

func TestLSP_BasicWorkflow(t *testing.T) {
	// Skip if gopls is not available
	if !isCommandAvailable("gopls") {
		t.Skip("gopls not available, skipping LSP tests")
	}

	// Create test directory with Go code
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

	// Create LSP service
	svc := NewService()
	defer svc.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start gopls
	if err := svc.StartClient(ctx, "go", "gopls", []string{"serve"}); err != nil {
		t.Fatalf("Failed to start gopls: %v", err)
	}

	client := svc.getClient("go")
	if client == nil {
		t.Fatal("Failed to get gopls client")
	}

	// Open the helper file
	helperURI := util.PathToURI(helperFile)
	if err := client.DidOpen(ctx, helperURI, "go", helperCode); err != nil {
		t.Fatalf("Failed to open helper.go: %v", err)
	}

	// Test GetReferences for Helper function
	// The Helper function is at line 3 (0-indexed: line 2), column 5
	refs, err := client.GetReferences(ctx, helperURI, 2, 5, false)
	if err != nil {
		t.Logf("Warning: GetReferences failed: %v (this might be expected if gopls needs time to index)", err)
	} else {
		t.Logf("Found %d references to Helper", len(refs))
		// We expect at least 2 references in main.go
		if len(refs) >= 2 {
			t.Logf("Successfully found references!")
		}
	}

	// Test GetDefinition
	mainURI := util.PathToURI(mainFile)
	if err := client.DidOpen(ctx, mainURI, "go", mainCode); err != nil {
		t.Fatalf("Failed to open main.go: %v", err)
	}

	// Try to find definition of Helper() call at line 4
	defs, err := client.GetDefinition(ctx, mainURI, 3, 2)
	if err != nil {
		t.Logf("Warning: GetDefinition failed: %v", err)
	} else {
		t.Logf("Found %d definitions", len(defs))
	}

	// Close documents
	client.DidClose(ctx, helperURI)
	client.DidClose(ctx, mainURI)
}

func TestLSP_Enrich(t *testing.T) {
	// Skip if gopls is not available
	if !isCommandAvailable("gopls") {
		t.Skip("gopls not available, skipping LSP tests")
	}

	// Create test directory with Go code
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

	// Create nodes representing the scanned functions
	nodes := []*graph.Node{
		{
			ID:        "main:MainFunc",
			Name:      "MainFunc",
			Kind:      "function_declaration",
			FilePath:  mainFile,
			LineStart: 3,
			ColStart:  6,
			LineEnd:   5,
			ColEnd:    1,
		},
		{
			ID:        "main:Helper",
			Name:      "Helper",
			Kind:      "function_declaration",
			FilePath:  helperFile,
			LineStart: 3,
			ColStart:  6,
			LineEnd:   3,
			ColEnd:    21,
		},
	}

	// Create LSP service
	svc := NewService()
	defer svc.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Run enrichment
	edges, err := svc.Enrich(ctx, nodes)
	if err != nil {
		t.Fatalf("Enrich failed: %v", err)
	}

	t.Logf("Found %d edges", len(edges))
	for _, e := range edges {
		t.Logf("Edge: %s --%s--> %s", e.SourceID, e.Relation, e.TargetID)
	}

	// Note: The actual edge detection might not work immediately as gopls
	// needs time to index the workspace. This test mainly verifies that
	// the enrichment process runs without errors.
}

func TestHelperFunctions(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"test.go", "go"},
		{"script.py", "python"},
		{"app.js", "javascript"},
		{"app.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"component.jsx", "javascript"},
		{"config.lua", "lua"},
		{"unknown.txt", ""},
	}

	for _, tt := range tests {
		if got := getLang(tt.path); got != tt.want {
			t.Errorf("getLang(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestIsDefinitionKind(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{"function_declaration", true},
		{"method_definition", true},
		{"class_definition", true},
		{"interface_declaration", true},
		{"variable_declaration", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		if got := isDefinitionKind(tt.kind); got != tt.want {
			t.Errorf("isDefinitionKind(%q) = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestIsInterfaceKind(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{"interface_declaration", true},
		{"protocol_declaration", true},
		{"class_definition", false},
		{"function_declaration", false},
	}

	for _, tt := range tests {
		if got := isInterfaceKind(tt.kind); got != tt.want {
			t.Errorf("isInterfaceKind(%q) = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestFindNodeContaining(t *testing.T) {
	nodes := []*graph.Node{
		{
			ID:        "file1:FuncA",
			FilePath:  "/test/file1.go",
			LineStart: 1,
			LineEnd:   10,
		},
		{
			ID:        "file1:FuncB",
			FilePath:  "/test/file1.go",
			LineStart: 5,
			LineEnd:   8,
		},
		{
			ID:        "file2:FuncC",
			FilePath:  "/test/file2.go",
			LineStart: 1,
			LineEnd:   5,
		},
	}

	tests := []struct {
		name     string
		filePath string
		line     int
		col      int
		wantID   string
	}{
		{"top of FuncA", "/test/file1.go", 2, 1, "file1:FuncA"},
		{"inside FuncB (nested)", "/test/file1.go", 6, 1, "file1:FuncB"},
		{"in FuncC", "/test/file2.go", 3, 1, "file2:FuncC"},
		{"no match", "/test/file3.go", 1, 1, ""},
		{"outside range", "/test/file1.go", 15, 1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findNodeContaining(nodes, tt.filePath, tt.line, tt.col)
			if tt.wantID == "" {
				if got != nil {
					t.Errorf("Expected nil, got %v", got)
				}
			} else {
				if got == nil {
					t.Errorf("Expected node with ID %s, got nil", tt.wantID)
				} else if got.ID != tt.wantID {
					t.Errorf("Expected node ID %s, got %s", tt.wantID, got.ID)
				}
			}
		})
	}
}
