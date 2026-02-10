package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"codemap/internal/db"
	"codemap/internal/graph"
	"codemap/internal/scanner"
)

func TestIntegration_ReindexAndQuery(t *testing.T) {
	// 1. Setup Temp DB
	tmpDbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.New(tmpDbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer database.Close()
	store := graph.NewStore(database)

	// 2. Setup Temp Workspace with Polyglot Code
	wsDir := t.TempDir()
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

MyTable = {}
MyTable.Method = function() end
`)

	// 3. Init Scanner
	scn, err := scanner.New()
	if err != nil {
		t.Fatalf("Failed to init scanner: %v", err)
	}

	// 4. Run Scan
	nodes, err := scn.Scan(context.Background(), wsDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// 5. Store Nodes
	for _, n := range nodes {
		if err := store.UpsertNode(context.Background(), n); err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// 6. Verify Queries

	// Check Go Symbol
	locs, err := store.GetSymbolLocation(context.Background(), "MainFunc")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for MainFunc, got %d", len(locs))
	} else {
		if locs[0].Kind != "function_declaration" {
			t.Errorf("Expected kind function_declaration, got %s", locs[0].Kind)
		}
	}

	// Check Python Symbol
	locs, err = store.GetSymbolLocation(context.Background(), "MyClass")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for MyClass, got %d", len(locs))
	} else {
		if locs[0].Kind != "class_definition" {
			t.Errorf("Expected kind class_definition, got %s", locs[0].Kind)
		}
	}

	// Check TS Symbol
	locs, err = store.GetSymbolLocation(context.Background(), "User")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for User, got %d", len(locs))
	} else {
		if locs[0].Kind != "interface_declaration" {
			t.Errorf("Expected kind interface_declaration, got %s", locs[0].Kind)
		}
	}

	// Check JS Method
	locs, err = store.GetSymbolLocation(context.Background(), "log")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for log, got %d", len(locs))
	} else {
		if locs[0].Kind != "method_definition" {
			t.Errorf("Expected kind method_definition, got %s", locs[0].Kind)
		}
	}

	// Check Lua Symbol
	locs, err = store.GetSymbolLocation(context.Background(), "GlobalFunc")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for GlobalFunc, got %d", len(locs))
	}

	locs, err = store.GetSymbolLocation(context.Background(), "LocalFunc")
	if err != nil {
		t.Fatalf("GetSymbolLocation failed: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("Expected 1 location for LocalFunc, got %d", len(locs))
	}

	// 7. Verify File Map
	scriptPath := filepath.Join(wsDir, "script.py")
	fileNodes, err := store.GetSymbolsInFile(context.Background(), scriptPath)
	if err != nil {
		t.Fatalf("GetSymbolsInFile failed: %v", err)
	}
	if len(fileNodes) != 2 {
		t.Errorf("Expected 2 symbols in script.py, got %d", len(fileNodes))
	}
}

func createFile(t *testing.T, dir, name, content string) {
	err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
}
