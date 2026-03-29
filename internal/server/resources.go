package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerResources() {
	s.mcpSrv.AddResource(&mcp.Resource{
		URI:         "codemap://usage-guidelines",
		Name:        "Usage Guidelines",
		Description: "System prompt and usage guidelines for the CodeMap MCP server",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      "codemap://usage-guidelines",
					MIMEType: "text/markdown",
					Text:     s.sysprompt,
				},
			},
		}, nil
	})

	// Build a map of tool name -> schema JSON for dynamic dispatch.
	schemaMap := buildSchemaMap()

	// Register a single resource template that matches codemap://schemas/{tool_name}.
	s.mcpSrv.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "codemap://schemas/{tool_name}",
		Name:        "Tool Schema",
		Description: "JSON schema for the named tool's arguments",
		MIMEType:    "application/schema+json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		toolName := strings.TrimPrefix(uri, "codemap://schemas/")
		schemaJSON, ok := schemaMap[toolName]
		if !ok {
			return nil, fmt.Errorf("unknown tool schema: %q", toolName)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      uri,
					MIMEType: "application/schema+json",
					Text:     schemaJSON,
				},
			},
		}, nil
	})
}

// buildSchemaMap constructs a map from tool name to its JSON schema string.
func buildSchemaMap() map[string]string {
	m := make(map[string]string)
	addSchema[IndexArgs](m, "index")
	addSchema[IndexStatusArgs](m, "index_status")
	addSchema[GetSymbolsInFileArgs](m, "get_symbols_in_file")
	addSchema[FindImpactArgs](m, "find_impact")
	addSchema[GetSymbolArgs](m, "get_symbol")
	addSchema[GetDiagnosticsArgs](m, "get_diagnostics")
	return m
}

func addSchema[T any](m map[string]string, name string) {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		return
	}
	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return
	}
	m[name] = string(schemaJSON)
}
