package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerPrompts() {
	s.mcpServer.AddPrompt(&mcp.Prompt{
		Name:        "analyze-impact",
		Description: "Analyzes the potential impact of modifying a symbol",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "symbol_name",
				Description: "The name of the symbol to analyze",
				Required:    true,
			},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		symbolName := req.Params.Arguments["symbol_name"]
		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Analyze impact of modifying %s", symbolName),
			Messages: []*mcp.PromptMessage{
				{
					Role: "user",
					Content: &mcp.TextContent{
						Text: fmt.Sprintf("I'm planning to modify %s. Please find its definition using get_symbol_location and then use find_impact to identify all downstream symbols that might be broken or affected by this change.", symbolName),
					},
				},
			},
		}, nil
	})

	s.mcpServer.AddPrompt(&mcp.Prompt{
		Name:        "explore-file",
		Description: "Explores the structure and symbols of a file",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "file_path",
				Description: "The path to the file to explore",
				Required:    true,
			},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		filePath := req.Params.Arguments["file_path"]
		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Explore file %s", filePath),
			Messages: []*mcp.PromptMessage{
				{
					Role: "user",
					Content: &mcp.TextContent{
						Text: fmt.Sprintf("Explain the structure of the file at %s. Use get_symbols_in_file to list all symbols and provide a high-level summary of their roles.", filePath),
					},
				},
			},
		}, nil
	})

	s.mcpServer.AddPrompt(&mcp.Prompt{
		Name:        "locate-and-explain",
		Description: "Locates a symbol and explains its context in the file",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "symbol_name",
				Description: "The name of the symbol to locate",
				Required:    true,
			},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		symbolName := req.Params.Arguments["symbol_name"]
		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Locate and explain %s", symbolName),
			Messages: []*mcp.PromptMessage{
				{
					Role: "user",
					Content: &mcp.TextContent{
						Text: fmt.Sprintf("Where is %s defined? Use get_symbol_location to find it, then use get_symbols_in_file on that file to explain what other symbols are related to it in that context.", symbolName),
					},
				},
			},
		}, nil
	})

	s.mcpServer.AddPrompt(&mcp.Prompt{
		Name:        "re-index-workspace",
		Description: "Triggers a re-index of the workspace to refresh the code graph",
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "Re-index the workspace",
			Messages: []*mcp.PromptMessage{
				{
					Role: "user",
					Content: &mcp.TextContent{
						Text: "The codebase has changed. Please run the index tool to update the semantic graph and report how many symbols and relationships are now tracked.",
					},
				},
			},
		}, nil
	})
}
