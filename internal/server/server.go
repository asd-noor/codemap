package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"codemap/internal/graph"
	"codemap/internal/lsp"
	"codemap/internal/scanner"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	scanner   *scanner.Scanner
	store     *graph.Store
	lsp       *lsp.Service
	mcpServer *mcp.Server
}

func New(scn *scanner.Scanner, store *graph.Store, lspSvc *lsp.Service) *Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "code-graph",
		Version: "0.1.0",
	}, nil)

	srv := &Server{
		scanner:   scn,
		store:     store,
		lsp:       lspSvc,
		mcpServer: s,
	}
	srv.registerTools()
	return srv
}

func (s *Server) Run(ctx context.Context) error {
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

// Arguments structs
type IndexArgs struct {
	Force bool `json:"force"`
}

type GetSymbolsInFileArgs struct {
	FilePath string `json:"file_path" jsonschema:"required"`
}

type FindImpactArgs struct {
	SymbolName string `json:"symbol_name" jsonschema:"required"`
}

type GetSymbolLocationArgs struct {
	SymbolName string `json:"symbol_name" jsonschema:"required"`
}

func (s *Server) registerTools() {
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "index",
		Description: "Scans the workspace and updates the code graph",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args IndexArgs) (*mcp.CallToolResult, any, error) {
		cwd, _ := os.Getwd()

		nodes, err := s.scanner.Scan(ctx, cwd)
		if err != nil {
			return errorResult(fmt.Sprintf("Scan failed: %v", err)), nil, nil
		}

		for _, n := range nodes {
			if err := s.store.UpsertNode(ctx, n); err != nil {
				return errorResult(fmt.Sprintf("Failed to store node %s: %v", n.ID, err)), nil, nil
			}
		}

		edges, err := s.lsp.Enrich(ctx, nodes, s.store)
		if err != nil {
			return errorResult(fmt.Sprintf("Enrich failed: %v", err)), nil, nil
		}

		for _, e := range edges {
			if err := s.store.UpsertEdge(ctx, e); err != nil {
				return errorResult(fmt.Sprintf("Failed to store edge: %v", err)), nil, nil
			}
		}

		msg := fmt.Sprintf("Indexed %d nodes and %d edges", len(nodes), len(edges))
		return textResult(msg), nil, nil
	})

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "get_symbols_in_file",
		Description: "Returns the structure of a file",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetSymbolsInFileArgs) (*mcp.CallToolResult, any, error) {
		nodes, err := s.store.GetSymbolsInFile(ctx, args.FilePath)
		if err != nil {
			return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
		}

		type SimpleNode struct {
			Name  string `json:"name"`
			Kind  string `json:"kind"`
			Range string `json:"range"`
		}
		var simple []SimpleNode
		for _, n := range nodes {
			simple = append(simple, SimpleNode{
				Name:  n.Name,
				Kind:  n.Kind,
				Range: fmt.Sprintf("%d:%d-%d:%d", n.LineStart, n.ColStart, n.LineEnd, n.ColEnd),
			})
		}

		jsonBytes, _ := json.MarshalIndent(simple, "", "  ")
		return textResult(string(jsonBytes)), nil, nil
	})

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "find_impact",
		Description: "Finds downstream dependents of a symbol",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args FindImpactArgs) (*mcp.CallToolResult, any, error) {
		nodes, err := s.store.FindImpact(ctx, args.SymbolName)
		if err != nil {
			return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
		}

		if len(nodes) == 0 {
			return textResult("No impacted symbols found."), nil, nil
		}

		type ImpactNode struct {
			Name     string `json:"name"`
			FilePath string `json:"file_path"`
			Kind     string `json:"kind"`
		}
		var impacted []ImpactNode
		for _, n := range nodes {
			impacted = append(impacted, ImpactNode{
				Name:     n.Name,
				FilePath: n.FilePath,
				Kind:     n.Kind,
			})
		}

		jsonBytes, _ := json.MarshalIndent(impacted, "", "  ")
		return textResult(string(jsonBytes)), nil, nil
	})

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "get_symbol_location",
		Description: "Finds the location of a symbol",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetSymbolLocationArgs) (*mcp.CallToolResult, any, error) {
		nodes, err := s.store.GetSymbolLocation(ctx, args.SymbolName)
		if err != nil {
			return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
		}

		if len(nodes) == 0 {
			return textResult("Symbol not found."), nil, nil
		}

		jsonBytes, _ := json.MarshalIndent(nodes, "", "  ")
		return textResult(string(jsonBytes)), nil, nil
	})
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: text,
			},
		},
	}
}

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: "ERROR: " + text,
			},
		},
		IsError: true,
	}
}
