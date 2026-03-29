// Package server — query.go
// Shared query helpers used by both CLI commands and MCP tools.
package server

import (
	"os"
	"path/filepath"

	"codemap/internal/graph"
)

// NodeWithSource wraps a graph.Node with an optional source snippet.
type NodeWithSource struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	FilePath  string `json:"file_path"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	ColStart  int    `json:"col_start"`
	ColEnd    int    `json:"col_end"`
	SymbolURI string `json:"symbol_uri,omitempty"`
	Source    string `json:"source,omitempty"`
}

// NodeToWithSource converts a graph.Node and optionally attaches its source lines.
func NodeToWithSource(n graph.Node, withSource bool) NodeWithSource {
	nws := NodeWithSource{
		ID:        n.ID,
		Name:      n.Name,
		Kind:      n.Kind,
		FilePath:  n.FilePath,
		LineStart: n.LineStart,
		LineEnd:   n.LineEnd,
		ColStart:  n.ColStart,
		ColEnd:    n.ColEnd,
		SymbolURI: n.SymbolURI,
	}
	if withSource {
		nws.Source = ReadLines(n.FilePath, n.LineStart, n.LineEnd)
	}
	return nws
}

// AbsFilePath resolves p to an absolute path using cwd as the base for relative paths.
func AbsFilePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, path), nil
}
