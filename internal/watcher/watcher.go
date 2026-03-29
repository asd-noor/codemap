// Package watcher watches a directory tree for file changes and drives
// incremental re-indexing of the codemap graph.
package watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	gitignore "github.com/sabhiram/go-gitignore"

	"codemap/internal/graph"
	"codemap/internal/lsp"
	"codemap/internal/scanner"
)

const (
	debounceDelay = 500 * time.Millisecond
	pollInterval  = 100 * time.Millisecond
	// IdleTimeout is the duration of inactivity after which the watcher stops
	// itself by cancelling the context via the cancel func passed to Run.
	IdleTimeout = 5 * time.Minute
)

// skipDirs is the set of directory names the watcher never descends into.
var skipDirs = map[string]struct{}{
	"vendor":       {},
	"node_modules": {},
	"__pycache__":  {},
	".git":         {},
	"zig-cache":    {},
	"zig-out":      {},
}

// Handler is the callback type for per-file re-index events.
type Handler func(ctx context.Context, path string, removed bool)

// Watcher watches a project directory tree and fires re-indexing on file changes
// after debouncing.
type Watcher struct {
	rootDir string
	sc      *scanner.Scanner
	lspSvc  *lsp.Service
	store   *graph.Store
	ignore  *gitignore.GitIgnore
	inner   *fsnotify.Watcher

	mu         sync.Mutex
	pending    map[string]time.Time // path → deadline
	removedSet map[string]struct{}  // paths that were removed/renamed
}

// New creates a Watcher for rootDir.
func New(rootDir string, sc *scanner.Scanner, lspSvc *lsp.Service, store *graph.Store) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	ig, _ := gitignore.CompileIgnoreFile(filepath.Join(rootDir, ".gitignore"))
	w := &Watcher{
		rootDir:    rootDir,
		sc:         sc,
		lspSvc:     lspSvc,
		store:      store,
		ignore:     ig,
		inner:      fw,
		pending:    make(map[string]time.Time),
		removedSet: make(map[string]struct{}),
	}
	// Add the entire directory tree.
	if err := w.addTree(rootDir); err != nil {
		fw.Close()
		return nil, err
	}
	return w, nil
}

// addTree recursively adds all directories under root to the fsnotify watcher.
func (w *Watcher) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		if _, ok := skipDirs[name]; ok {
			return filepath.SkipDir
		}
		return w.inner.Add(path)
	})
}

// Run starts the event loop. It blocks until ctx is done or the idle timeout
// elapses. cancel is the CancelFunc for ctx; Run calls it when the idle timer
// fires so that the parent can detect the shutdown cleanly.
func (w *Watcher) Run(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	idle := time.NewTimer(IdleTimeout)
	defer idle.Stop()

	for {
		select {
		case <-ctx.Done():
			w.inner.Close()
			return

		case <-idle.C:
			// No file activity for IdleTimeout — self-terminate.
			w.inner.Close()
			cancel()
			return

		case event, ok := <-w.inner.Events:
			if !ok {
				return
			}
			w.handleEvent(ctx, event)

		case <-w.inner.Errors:
			// Ignore watcher errors.

		case <-ticker.C:
			if w.flush(ctx) {
				// Activity detected — reset the idle timer.
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(IdleTimeout)
			}
		}
	}
}

// handleEvent processes a single fsnotify event.
func (w *Watcher) handleEvent(ctx context.Context, event fsnotify.Event) {
	path := event.Name
	if w.ignore != nil {
		rel, _ := filepath.Rel(w.rootDir, path)
		if w.ignore.MatchesPath(rel) {
			return
		}
	}

	// If a new directory was created, watch its subtree.
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			w.addTree(path) //nolint:errcheck
			return
		}
	}

	removed := event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)

	w.mu.Lock()
	if removed {
		w.removedSet[path] = struct{}{}
	} else {
		delete(w.removedSet, path)
	}
	w.pending[path] = time.Now().Add(debounceDelay)
	w.mu.Unlock()
}

// flush processes all pending files whose debounce deadline has passed.
// It returns true if at least one file was processed (activity occurred).
func (w *Watcher) flush(ctx context.Context) bool {
	now := time.Now()
	w.mu.Lock()
	var ready []string
	var readyRemoved []string
	for path, deadline := range w.pending {
		if now.After(deadline) {
			ready = append(ready, path)
			if _, ok := w.removedSet[path]; ok {
				readyRemoved = append(readyRemoved, path)
			}
			delete(w.pending, path)
			delete(w.removedSet, path)
		}
	}
	w.mu.Unlock()

	for _, path := range readyRemoved {
		w.store.DeleteNodesByFile(ctx, path) //nolint:errcheck
	}

	// Determine non-removed paths.
	removedSet := make(map[string]struct{}, len(readyRemoved))
	for _, p := range readyRemoved {
		removedSet[p] = struct{}{}
	}
	for _, path := range ready {
		if _, ok := removedSet[path]; ok {
			continue
		}
		w.reindexFile(ctx, path)
	}
	return len(ready) > 0
}

// reindexFile performs the full re-index sequence for a single modified file:
// delete stale nodes → scan → upsert nodes → enrich → upsert edges.
func (w *Watcher) reindexFile(ctx context.Context, path string) {
	// 1. Delete stale nodes (cascade deletes edges).
	w.store.DeleteNodesByFile(ctx, path) //nolint:errcheck

	// 2. Re-scan with Tree-sitter.
	nodes, err := w.sc.ScanFile(ctx, path)
	if err != nil || len(nodes) == 0 {
		return
	}

	// 3. Upsert new nodes.
	if err := w.store.BulkUpsertNodes(ctx, nodes); err != nil {
		return
	}

	// 4. LSP enrichment.
	edges, err := w.lspSvc.Enrich(ctx, nodes, w.store)
	if err != nil || len(edges) == 0 {
		return
	}

	// 5. Upsert edges.
	w.store.BulkUpsertEdges(ctx, edges) //nolint:errcheck
}
