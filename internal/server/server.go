// Package server wires the codemap engine together: scanning, LSP enrichment,
// file watching, index lifecycle, and MCP tool exposure.
package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"codemap/internal/graph"
	"codemap/internal/lsp"
	"codemap/internal/pkgmgr"
	"codemap/internal/scanner"
	"codemap/internal/watcher"
	"codemap/util"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// saveDiagnostics drains the LSP service diagnostic cache, converts every
// entry to graph types, resolves the enclosing symbol node for each diagnostic,
// and persists everything to the store.
func (srv *Server) saveDiagnostics(ctx context.Context) error {
	raw := srv.lspSvc.DrainDiagnostics()
	for uri, lspDiags := range raw {
		filePath := util.URIToPath(uri)

		var diags []graph.Diagnostic
		var edges []graph.DiagnosticEdge

		for _, d := range lspDiags {
			line := d.Range.Start.Line + 1     // LSP is 0-indexed
			col := d.Range.Start.Character + 1 // LSP is 0-indexed
			gd := graph.Diagnostic{
				ID:       util.DiagnosticID(filePath, line, col, d.Message),
				FilePath: filePath,
				Line:     line,
				Col:      col,
				Severity: d.Severity,
				Code:     d.Code,
				Source:   d.Source,
				Message:  d.Message,
			}
			diags = append(diags, gd)

			// Best-effort: link to the smallest enclosing symbol node.
			if n, err := srv.store.FindNode(ctx, filePath, line, col); err == nil && n != nil {
				edges = append(edges, graph.DiagnosticEdge{
					DiagnosticID: gd.ID,
					NodeID:       n.ID,
				})
			}
		}

		if err := srv.store.UpsertDiagnosticsForFile(ctx, filePath, diags); err != nil {
			return fmt.Errorf("upsert diagnostics for %s: %w", filePath, err)
		}
		if err := srv.store.BulkUpsertDiagnosticEdges(ctx, edges); err != nil {
			return fmt.Errorf("upsert diagnostic edges for %s: %w", filePath, err)
		}
	}
	return nil
}

// IndexStatus represents the lifecycle state of the codemap index.
type IndexStatus int

const (
	IndexStatusIdle       IndexStatus = iota
	IndexStatusInProgress             // scanning + enriching
	IndexStatusReady                  // index complete, queries served
	IndexStatusFailed                 // scan or enrich error
)

func (s IndexStatus) String() string {
	switch s {
	case IndexStatusIdle:
		return "idle"
	case IndexStatusInProgress:
		return "in_progress"
	case IndexStatusReady:
		return "ready"
	case IndexStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

const waitForIndexTimeout = 30 * time.Second

// IndexCallback is called to report indexing progress. May be nil.
type IndexCallback func(nodes, edges int, elapsed time.Duration)

// Server holds all codemap engine state.
type Server struct {
	rootDir   string
	store     *graph.Store
	sc        *scanner.Scanner
	lspSvc    *lsp.Service
	mcpSrv    *mcp.Server
	sysprompt string

	mu         sync.Mutex
	status     IndexStatus
	startedAt  time.Time
	finishedAt time.Time
	indexErr   error
	done       chan struct{} // closed when status transitions to Ready or Failed
	closeOnce  *sync.Once    // guards close(done); replaced alongside done

	// Optional callback for UI visualization
	indexCallback IndexCallback
}

// NewWithCallback creates and starts a Server with an optional indexing callback.
func NewWithCallback(ctx context.Context, rootDir, dbPath, systemPrompt string, cb IndexCallback) (*Server, error) {
	return newServer(ctx, rootDir, dbPath, systemPrompt, func() {}, cb)
}

// New creates and starts a Server for the given project root. The initial
// index begins immediately in a background goroutine. The file watcher runs
// until ctx is cancelled.
func New(ctx context.Context, rootDir, dbPath, systemPrompt string) (*Server, error) {
	return newServer(ctx, rootDir, dbPath, systemPrompt, func() {}, nil)
}

// NewWatch is like New but passes cancel to the watcher so that the watcher's
// idle-timeout fires cancel(), unblocking the caller's <-ctx.Done().
func NewWatch(ctx context.Context, cancel context.CancelFunc, rootDir, dbPath, systemPrompt string) (*Server, error) {
	return newServer(ctx, rootDir, dbPath, systemPrompt, cancel, nil)
}

// newServer is the shared constructor used by New and NewWatch.
func newServer(ctx context.Context, rootDir, dbPath, systemPrompt string, cancel context.CancelFunc, cb IndexCallback) (*Server, error) {
	absRoot := util.FindGitRoot(rootDir)

	store, err := graph.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("server: open store at %s: %w", dbPath, err)
	}

	pm := pkgmgr.New()
	sc := scanner.New(absRoot)
	lspSvc := lsp.NewService(absRoot, pm)

	srv := &Server{
		rootDir:       absRoot,
		store:         store,
		sc:            sc,
		lspSvc:        lspSvc,
		sysprompt:     systemPrompt,
		status:        IndexStatusIdle,
		done:          make(chan struct{}),
		closeOnce:     &sync.Once{},
		indexCallback: cb,
	}

	srv.registerTools()
	srv.registerResources()
	srv.registerPrompts()

	// Start initial full index in the background.
	go srv.runIndex(ctx)

	// Start file watcher.
	go func() {
		w, err := watcher.New(absRoot, sc, lspSvc, store)
		if err != nil {
			return
		}
		w.Run(ctx, cancel)
	}()

	return srv, nil
}

// Close shuts down the server's resources.
func (srv *Server) Close() {
	srv.lspSvc.Close()
	srv.store.Close() //nolint:errcheck
}

// Status returns the current index status plus elapsed/total duration and any error.
func (srv *Server) Status() (IndexStatus, time.Duration, error) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	var dur time.Duration
	switch srv.status {
	case IndexStatusInProgress:
		dur = time.Since(srv.startedAt)
	case IndexStatusReady, IndexStatusFailed:
		dur = srv.finishedAt.Sub(srv.startedAt)
	}
	return srv.status, dur, srv.indexErr
}

// WaitForIndex blocks until the index is Ready or Failed (or ctx expires).
func (srv *Server) WaitForIndex(ctx context.Context) error {
	srv.mu.Lock()
	done := srv.done
	st := srv.status
	srv.mu.Unlock()

	if st == IndexStatusReady {
		return nil
	}
	if st == IndexStatusFailed {
		return srv.indexErr
	}

	timeout := time.NewTimer(waitForIndexTimeout)
	defer timeout.Stop()

	select {
	case <-done:
		srv.mu.Lock()
		err := srv.indexErr
		srv.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-timeout.C:
		return fmt.Errorf("server: timed out waiting for index")
	}
}

// ForceIndex triggers a full re-index. Returns an error if one is already running.
func (srv *Server) ForceIndex(ctx context.Context) error {
	srv.mu.Lock()
	if srv.status == IndexStatusInProgress {
		srv.mu.Unlock()
		return fmt.Errorf("server: index already in progress")
	}
	// Reset the done channel and its Once for callers waiting on WaitForIndex.
	srv.done = make(chan struct{})
	srv.closeOnce = &sync.Once{}
	srv.mu.Unlock()

	go srv.runIndex(ctx)
	return nil
}

// runIndex performs a full scan + enrichment cycle, updating status.
func (srv *Server) runIndex(ctx context.Context) {
	srv.mu.Lock()
	srv.status = IndexStatusInProgress
	srv.startedAt = time.Now()
	srv.indexErr = nil
	done := srv.done
	once := srv.closeOnce
	srv.mu.Unlock()

	err := srv.doIndex(ctx)

	srv.mu.Lock()
	srv.finishedAt = time.Now()
	if err != nil {
		srv.status = IndexStatusFailed
		srv.indexErr = err
	} else {
		srv.status = IndexStatusReady
	}
	srv.mu.Unlock()

	once.Do(func() { close(done) })
}

// doIndex runs the actual scan + enrich + store cycle, including diagnostics.
func (srv *Server) doIndex(ctx context.Context) error {
	if err := srv.store.Clear(ctx); err != nil {
		return fmt.Errorf("clear store: %w", err)
	}

	nodes, err := srv.sc.Scan(ctx, srv.rootDir)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	if err := srv.store.BulkUpsertNodes(ctx, nodes); err != nil {
		return fmt.Errorf("upsert nodes: %w", err)
	}

	if srv.indexCallback != nil {
		elapsed := time.Since(srv.startedAt)
		srv.indexCallback(len(nodes), 0, elapsed)
	}

	edges, err := srv.lspSvc.Enrich(ctx, nodes, srv.store)
	if err != nil {
		return fmt.Errorf("enrich: %w", err)
	}

	if err := srv.store.BulkUpsertEdges(ctx, edges); err != nil {
		return fmt.Errorf("upsert edges: %w", err)
	}

	// Persist any LSP diagnostics that arrived during enrichment.
	if err := srv.saveDiagnostics(ctx); err != nil {
		return fmt.Errorf("save diagnostics: %w", err)
	}

	if srv.indexCallback != nil {
		elapsed := time.Since(srv.startedAt)
		srv.indexCallback(len(nodes), len(edges), elapsed)
	}

	return nil
}

// Store returns the underlying graph store (for direct queries from tools).
func (srv *Server) Store() *graph.Store { return srv.store }

// MCPServer returns the underlying MCP server for transport binding.
func (srv *Server) MCPServer() *mcp.Server { return srv.mcpSrv }

// OpenStoreReadOnly opens the codemap database at dbPath in read-only mode.
// Convenience wrapper used by CLI sub-commands that only query the index.
func OpenStoreReadOnly(dbPath string) (*graph.Store, error) {
	return graph.OpenReadOnly(dbPath)
}
