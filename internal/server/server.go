package server

import (
	"context"

	"codemap/internal/graph"
	"codemap/internal/lsp"
	"codemap/internal/scanner"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	scanner      *scanner.Scanner
	store        *graph.Store
	lsp          *lsp.Service
	mcpServer    *mcp.Server
	systemPrompt string
}

func New(scn *scanner.Scanner, store *graph.Store, lspSvc *lsp.Service, systemPrompt string) *Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "codemap",
		Version: "0.1.0",
	}, nil)

	srv := &Server{
		scanner:      scn,
		store:        store,
		lsp:          lspSvc,
		mcpServer:    s,
		systemPrompt: systemPrompt,
	}
	srv.registerTools()
	srv.registerResources()
	srv.registerPrompts()
	return srv
}

func (s *Server) Run(ctx context.Context) error {
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
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
