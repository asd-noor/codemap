package server

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"codemap/internal/graph"
	"codemap/internal/lsp"
	"codemap/internal/scanner"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type IndexStatus string

const (
	IndexStatusNotStarted IndexStatus = "not_started"
	IndexStatusInProgress IndexStatus = "in_progress"
	IndexStatusReady      IndexStatus = "ready"
	IndexStatusFailed     IndexStatus = "failed"
)

type Server struct {
	scanner      *scanner.Scanner
	store        *graph.Store
	lsp          *lsp.Service
	mcpServer    *mcp.Server
	systemPrompt string

	indexStatus    IndexStatus
	indexError     error
	indexStartTime time.Time
	indexEndTime   time.Time
	indexMu        sync.RWMutex
	indexReady     chan struct{}
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
		indexStatus:  IndexStatusNotStarted,
		indexReady:   make(chan struct{}),
	}
	srv.registerTools()
	srv.registerResources()
	srv.registerPrompts()
	return srv
}

func (s *Server) GetIndexStatus() (IndexStatus, error, time.Duration) {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	
	var duration time.Duration
	if !s.indexStartTime.IsZero() {
		if s.indexEndTime.IsZero() {
			duration = time.Since(s.indexStartTime)
		} else {
			duration = s.indexEndTime.Sub(s.indexStartTime)
		}
	}
	
	return s.indexStatus, s.indexError, duration
}

func (s *Server) setIndexStatus(status IndexStatus, err error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	
	s.indexStatus = status
	s.indexError = err
	
	if status == IndexStatusInProgress {
		s.indexStartTime = time.Now()
	} else if status == IndexStatusReady || status == IndexStatusFailed {
		s.indexEndTime = time.Now()
		close(s.indexReady)
	}
}

func (s *Server) WaitForIndex(ctx context.Context) error {
	select {
	case <-s.indexReady:
		s.indexMu.RLock()
		err := s.indexError
		s.indexMu.RUnlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) Run(ctx context.Context) error {
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) RunInitialIndex(ctx context.Context, projectRoot string) {
	s.setIndexStatus(IndexStatusInProgress, nil)
	
	nodes, err := s.scanner.Scan(ctx, projectRoot)
	if err != nil {
		s.setIndexStatus(IndexStatusFailed, fmt.Errorf("scan failed: %w", err))
		return
	}

	// COLLECT VALID FILES
	validFiles := make(map[string]bool)
	var validFileList []string
	for _, n := range nodes {
		if !validFiles[n.FilePath] {
			validFiles[n.FilePath] = true
			validFileList = append(validFileList, n.FilePath)
		}
	}

	if err := s.store.BulkUpsertNodes(ctx, nodes); err != nil {
		s.setIndexStatus(IndexStatusFailed, fmt.Errorf("failed to store nodes: %w", err))
		return
	}

	// PRUNE STALE DATA
	if err := s.store.PruneStaleFiles(ctx, validFileList); err != nil {
		// Log warning but don't fail
		fmt.Fprintf(os.Stderr, "Warning: Failed to prune stale files: %v\n", err)
	}

	edges, err := s.lsp.Enrich(ctx, nodes, s.store)
	if err != nil {
		s.setIndexStatus(IndexStatusFailed, fmt.Errorf("LSP enrichment failed: %w", err))
		return
	}

	if err := s.store.BulkUpsertEdges(ctx, edges); err != nil {
		s.setIndexStatus(IndexStatusFailed, fmt.Errorf("failed to store edges: %w", err))
		return
	}

	s.setIndexStatus(IndexStatusReady, nil)
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
