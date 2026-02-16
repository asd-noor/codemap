package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerResources() {
	s.mcpServer.AddResource(&mcp.Resource{
		URI:         "mcp://usage-guidelines",
		Name:        "Usage Guidelines",
		Description: "System prompt and usage guidelines for the CodeMap MCP server",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      "mcp://usage-guidelines",
					MIMEType: "text/markdown",
					Text:     s.systemPrompt,
				},
			},
		}, nil
	})
}
