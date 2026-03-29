package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"codemap/internal/graph"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── Argument structs ──────────────────────────────────────────────────────────

type IndexArgs struct {
	Force bool `json:"force" jsonschema:"description:Force a full re-index even if one is already in progress"`
}

type IndexStatusArgs struct{}

type GetSymbolsInFileArgs struct {
	FilePath string `json:"file_path" jsonschema:"required,description:The absolute path to the file to analyse"`
}

type FindImpactArgs struct {
	SymbolName string `json:"symbol_name" jsonschema:"required,description:The name of the symbol to analyse for transitive impact"`
}

type GetSymbolArgs struct {
	SymbolName string `json:"symbol_name" jsonschema:"required,description:The name of the symbol to locate"`
	WithSource bool   `json:"with_source" jsonschema:"description:If true, includes the source code of the symbol in the response"`
}

type GetDiagnosticsArgs struct {
	FilePath string `json:"file_path" jsonschema:"description:Optional file path to restrict results to a single file"`
	Severity int    `json:"severity"  jsonschema:"description:Optional minimum severity filter: 1=error 2=warning 3=info 4=hint. Omit or 0 for all."`
}

// ── ReadLines ─────────────────────────────────────────────────────────────────

// ReadLines reads lines lineStart..lineEnd (1-indexed, inclusive) from path.
// Returns an empty string if the file cannot be opened or the range is invalid.
func ReadLines(path string, lineStart, lineEnd int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var out strings.Builder
	sc := bufio.NewScanner(f)
	line := 1
	for sc.Scan() {
		if line > lineEnd {
			break
		}
		if line >= lineStart {
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(sc.Text())
		}
		line++
	}
	return out.String()
}

// absPath resolves p to an absolute path using AbsFilePath.
// Returns p unchanged on error.
func absPath(p string) string {
	abs, err := AbsFilePath(p)
	if err != nil {
		return p
	}
	return abs
}

// ── Helper result builders ────────────────────────────────────────────────────

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("failed to marshal result: %v", err))
	}
	return textResult(string(b))
}

// ── Tool registration ─────────────────────────────────────────────────────────

func (s *Server) registerTools() {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "codemap",
		Version: "0.2.0",
	}, nil)
	s.mcpSrv = srv

	// index — trigger (re-)index
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "index",
		Description: "Scans the workspace and builds/refreshes the code graph. Run this after large changes.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args IndexArgs) (*mcp.CallToolResult, any, error) {
		if err := s.ForceIndex(ctx); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		// Wait for completion (up to 30 s).
		start := time.Now()
		if err := s.WaitForIndex(ctx); err != nil {
			return errorResult(fmt.Sprintf("indexing failed: %v", err)), nil, nil
		}
		dur := time.Since(start)

		nodes, _ := s.store.NodeCount(ctx)
		edges, _ := s.store.EdgeCount(ctx)
		diags, _ := s.store.DiagnosticCount(ctx)
		msg := fmt.Sprintf("Indexed %d nodes, %d edges, %d diagnostics in %.2fs",
			nodes, edges, diags, dur.Seconds())
		return textResult(msg), nil, nil
	})

	// index_status — query current index status
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "index_status",
		Description: "Returns the current indexing status of the workspace",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args IndexStatusArgs) (*mcp.CallToolResult, any, error) {
		status, dur, err := s.Status()
		result := map[string]any{
			"status":   status.String(),
			"duration": dur.String(),
		}
		if err != nil {
			result["error"] = err.Error()
		}
		if status == IndexStatusReady {
			nodes, _ := s.store.NodeCount(ctx)
			edges, _ := s.store.EdgeCount(ctx)
			diags, _ := s.store.DiagnosticCount(ctx)
			result["nodes"] = nodes
			result["edges"] = edges
			result["diagnostics"] = diags
		}
		return jsonResult(result), nil, nil
	})

	// get_symbols_in_file — list all symbols in a file
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_symbols_in_file",
		Description: "Lists all symbols (functions, types, classes, …) defined in a given source file",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetSymbolsInFileArgs) (*mcp.CallToolResult, any, error) {
		if err := s.WaitForIndex(ctx); err != nil {
			return errorResult(fmt.Sprintf("index not ready: %v", err)), nil, nil
		}
		nodes, err := s.store.GetSymbolsInFile(ctx, absPath(args.FilePath))
		if err != nil {
			return errorResult(fmt.Sprintf("query failed: %v", err)), nil, nil
		}
		if len(nodes) == 0 {
			return textResult(fmt.Sprintf("No symbols found in %s", args.FilePath)), nil, nil
		}
		return jsonResult(nodes), nil, nil
	})

	// find_impact — transitive reverse-dependency analysis
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_impact",
		Description: "Finds all symbols that transitively depend on the given symbol — shows the blast radius of a change",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args FindImpactArgs) (*mcp.CallToolResult, any, error) {
		if err := s.WaitForIndex(ctx); err != nil {
			return errorResult(fmt.Sprintf("index not ready: %v", err)), nil, nil
		}
		nodes, err := s.store.FindImpact(ctx, args.SymbolName)
		if err != nil {
			return errorResult(fmt.Sprintf("query failed: %v", err)), nil, nil
		}
		if len(nodes) == 0 {
			return textResult(fmt.Sprintf("No dependents found for %q", args.SymbolName)), nil, nil
		}
		return jsonResult(nodes), nil, nil
	})

	// get_symbol — locate a symbol across the project (with optional source)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_symbol",
		Description: "Locates a named symbol across the project, optionally returning its source code",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetSymbolArgs) (*mcp.CallToolResult, any, error) {
		if err := s.WaitForIndex(ctx); err != nil {
			return errorResult(fmt.Sprintf("index not ready: %v", err)), nil, nil
		}
		nodes, err := s.store.GetSymbolLocation(ctx, args.SymbolName)
		if err != nil {
			return errorResult(fmt.Sprintf("query failed: %v", err)), nil, nil
		}
		if len(nodes) == 0 {
			return textResult(fmt.Sprintf("Symbol %q not found", args.SymbolName)), nil, nil
		}

		type nodeWithSource = NodeWithSource
		results := make([]nodeWithSource, len(nodes))
		for i, n := range nodes {
			results[i] = NodeToWithSource(n, args.WithSource)
		}
		return jsonResult(results), nil, nil
	})

	// get_diagnostics — return LSP diagnostics from the last index
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_diagnostics",
		Description: "Returns LSP diagnostics (errors, warnings, hints) captured during the last index run. Optionally filter by file path or minimum severity.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetDiagnosticsArgs) (*mcp.CallToolResult, any, error) {
		if err := s.WaitForIndex(ctx); err != nil {
			return errorResult(fmt.Sprintf("index not ready: %v", err)), nil, nil
		}

		var (
			diags []graph.Diagnostic
			err   error
		)
		if args.FilePath != "" {
			diags, err = s.store.GetDiagnosticsForFile(ctx, absPath(args.FilePath))
		} else {
			diags, err = s.store.GetAllDiagnostics(ctx)
		}
		if err != nil {
			return errorResult(fmt.Sprintf("query failed: %v", err)), nil, nil
		}

		// Optional severity filter.
		if args.Severity > 0 {
			var filtered []graph.Diagnostic
			for _, d := range diags {
				if d.Severity <= args.Severity {
					filtered = append(filtered, d)
				}
			}
			diags = filtered
		}

		if len(diags) == 0 {
			return textResult("No diagnostics found"), nil, nil
		}
		return jsonResult(diags), nil, nil
	})
}
