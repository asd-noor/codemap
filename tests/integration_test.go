package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"codemap/internal/graph"
	"codemap/internal/scanner"
)

func TestIntegration_ReindexAndQuery(t *testing.T) {
	// 1. Setup Temp DB within a temp dir (graph.Open creates .ctxhub/ inside it).
	wsDir := t.TempDir()

	store, err := graph.Open(wsDir)
	if err != nil {
		t.Fatalf("Failed to open store: %v", err)
	}
	defer store.Close()

	// 2. Setup Temp Workspace with Polyglot Code.
	createFile(t, wsDir, "main.go", `package main
func MainFunc() {
	Helper()
}`)
	createFile(t, wsDir, "helper.go", `package main
func Helper() {}`)

	createFile(t, wsDir, "script.py", `
def my_python_func():
    pass
class MyClass:
    pass
`)

	createFile(t, wsDir, "types.ts", `
export interface User {
	name: string;
}
`)

	createFile(t, wsDir, "app.js", `
class Logger {
  log(msg) {
    console.log(msg);
  }
}
`)

	createFile(t, wsDir, "config.lua", `
function GlobalFunc(x)
  return x
end

local function LocalFunc()
end
`)

	// 3. Init Scanner.
	scn := scanner.New(wsDir)

	// 4. Run Scan.
	nodes, err := scn.Scan(context.Background(), wsDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// 5. Store Nodes.
	if err := store.BulkUpsertNodes(context.Background(), nodes); err != nil {
		t.Fatalf("BulkUpsertNodes failed: %v", err)
	}

	ctx := context.Background()

	// 6. Verify Queries.

	// Check Go symbols.
	locs, err := store.GetSymbolLocation(ctx, "MainFunc")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for MainFunc, got %d", len(locs))
	} else if locs[0].Kind != "function" {
		t.Errorf("Expected kind function, got %s", locs[0].Kind)
	}

	// Check Python symbol.
	locs, err = store.GetSymbolLocation(ctx, "MyClass")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for MyClass, got %d", len(locs))
	} else if locs[0].Kind != "class" {
		t.Errorf("Expected kind class, got %s", locs[0].Kind)
	}

	// Check TS interface.
	locs, err = store.GetSymbolLocation(ctx, "User")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for User, got %d", len(locs))
	} else if locs[0].Kind != "interface" {
		t.Errorf("Expected kind interface, got %s", locs[0].Kind)
	}

	// Check JS method.
	locs, err = store.GetSymbolLocation(ctx, "log")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for log, got %d", len(locs))
	} else if locs[0].Kind != "method" {
		t.Errorf("Expected kind method, got %s", locs[0].Kind)
	}

	// Check Lua symbols.
	locs, err = store.GetSymbolLocation(ctx, "GlobalFunc")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for GlobalFunc, got %d", len(locs))
	}

	locs, err = store.GetSymbolLocation(ctx, "LocalFunc")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for LocalFunc, got %d", len(locs))
	}

	// 7. Verify file map.
	scriptPath := filepath.Join(wsDir, "script.py")
	// ScanFile returns absolute paths, resolve to match.
	scriptPath, _ = filepath.Abs(scriptPath)
	fileNodes, err := store.GetSymbolsInFile(ctx, scriptPath)
	if err != nil {
		t.Fatalf("GetSymbolsInFile failed: %v", err)
	}
	if len(fileNodes) != 2 {
		t.Errorf("Expected 2 symbols in script.py, got %d", len(fileNodes))
	}
}

func createFile(t *testing.T, dir, name, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
}
