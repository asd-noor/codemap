package watcher

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	ignore "github.com/sabhiram/go-gitignore"

	"codemap/internal/graph"
	"codemap/internal/lsp"
	"codemap/internal/scanner"
)

// Watcher monitors file system changes and triggers re-indexing.
type Watcher struct {
	scanner   *scanner.Scanner
	store     *graph.Store
	lsp       *lsp.Service
	watcher   *fsnotify.Watcher
	root      string
	gitignore *ignore.GitIgnore

	// Debouncing
	debounceTime time.Duration
	pendingFiles map[string]time.Time
	mu           sync.Mutex
}

// New creates a new file watcher.
func New(scn *scanner.Scanner, store *graph.Store, lspSvc *lsp.Service, root string) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	// Load gitignore
	ign, _ := ignore.CompileIgnoreFile(filepath.Join(root, ".gitignore"))

	w := &Watcher{
		scanner:      scn,
		store:        store,
		lsp:          lspSvc,
		watcher:      fw,
		root:         root,
		gitignore:    ign,
		debounceTime: 500 * time.Millisecond,
		pendingFiles: make(map[string]time.Time),
	}

	return w, nil
}

// Watch starts watching the directory tree for changes.
func (w *Watcher) Watch(ctx context.Context) error {
	// Add all directories to watch recursively
	if err := w.addDirectoriesRecursively(w.root); err != nil {
		return fmt.Errorf("failed to add directories: %w", err)
	}

	log.Printf("Watching %s for file changes...", w.root)

	// Start debounce processor
	go w.processDebounced(ctx)

	// Process events
	for {
		select {
		case <-ctx.Done():
			return w.watcher.Close()

		case event, ok := <-w.watcher.Events:
			if !ok {
				return nil
			}
			w.handleEvent(ctx, event)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func (w *Watcher) handleEvent(ctx context.Context, event fsnotify.Event) {
	relPath, err := filepath.Rel(w.root, event.Name)
	if err != nil {
		return
	}

	if w.gitignore != nil && w.gitignore.MatchesPath(relPath) {
		return
	}

	if !w.isSourceFile(event.Name) {
		if event.Op&fsnotify.Create != 0 {
			info, err := os.Stat(event.Name)
			if err == nil && info.IsDir() {
				w.addDirectoriesRecursively(event.Name)
			}
		}
		return
	}

	switch {
	case event.Op&fsnotify.Write != 0:
		log.Printf("File modified: %s", relPath)
		w.debounceFile(event.Name)
	case event.Op&fsnotify.Create != 0:
		log.Printf("File created: %s", relPath)
		w.debounceFile(event.Name)
	case event.Op&fsnotify.Remove != 0:
		log.Printf("File deleted: %s", relPath)
		w.handleFileDeleted(ctx, event.Name)
	case event.Op&fsnotify.Rename != 0:
		log.Printf("File renamed: %s", relPath)
		w.handleFileDeleted(ctx, event.Name)
	}
}

func (w *Watcher) debounceFile(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pendingFiles[path] = time.Now().Add(w.debounceTime)
}

func (w *Watcher) processDebounced(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processPendingFiles(ctx)
		}
	}
}

func (w *Watcher) processPendingFiles(ctx context.Context) {
	w.mu.Lock()
	now := time.Now()
	var ready []string

	for path, deadline := range w.pendingFiles {
		if now.After(deadline) {
			ready = append(ready, path)
			delete(w.pendingFiles, path)
		}
	}
	w.mu.Unlock()

	for _, path := range ready {
		if err := w.reindexFile(ctx, path); err != nil {
			log.Printf("Failed to reindex %s: %v", path, err)
		}
	}
}

func (w *Watcher) reindexFile(ctx context.Context, path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return w.handleFileDeleted(ctx, path)
	}

	log.Printf("Re-indexing: %s", path)

	nodes, err := w.scanner.ScanFile(ctx, path)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	if err := w.store.DeleteNodesByFile(ctx, path); err != nil {
		return fmt.Errorf("delete old nodes failed: %w", err)
	}

	for _, n := range nodes {
		if err := w.store.UpsertNode(ctx, n); err != nil {
			return fmt.Errorf("store node failed: %w", err)
		}
	}

	edges, err := w.lsp.Enrich(ctx, nodes)
	if err != nil {
		log.Printf("LSP enrichment failed for %s: %v", path, err)
	}

	for _, e := range edges {
		if err := w.store.UpsertEdge(ctx, e); err != nil {
			log.Printf("Store edge failed: %v", err)
		}
	}

	log.Printf("✓ Re-indexed %s: %d nodes, %d edges", filepath.Base(path), len(nodes), len(edges))
	return nil
}

func (w *Watcher) handleFileDeleted(ctx context.Context, path string) error {
	log.Printf("Removing nodes for deleted file: %s", path)
	return w.store.DeleteNodesByFile(ctx, path)
}

func (w *Watcher) addDirectoriesRecursively(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			return nil
		}

		name := info.Name()
		if strings.HasPrefix(name, ".") && name != "." {
			return filepath.SkipDir
		}

		if name == "node_modules" || name == "vendor" || name == "__pycache__" {
			return filepath.SkipDir
		}

		relPath, err := filepath.Rel(w.root, path)
		if err == nil && w.gitignore != nil && w.gitignore.MatchesPath(relPath) {
			return filepath.SkipDir
		}

		if err := w.watcher.Add(path); err != nil {
			log.Printf("Warning: failed to watch %s: %v", path, err)
		}

		return nil
	})
}

func (w *Watcher) isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".lua":
		return true
	default:
		return false
	}
}

func (w *Watcher) Close() error {
	return w.watcher.Close()
}
