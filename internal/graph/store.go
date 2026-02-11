package graph

import (
	"context"
	"fmt"

	"codemap/internal/db"
)

type Store struct {
	db *db.DB
}

func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

func (s *Store) UpsertNode(ctx context.Context, n *Node) error {
	query := `
	INSERT INTO nodes (id, name, kind, file_path, line_start, line_end, col_start, col_end, symbol_uri)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name = excluded.name,
		kind = excluded.kind,
		file_path = excluded.file_path,
		line_start = excluded.line_start,
		line_end = excluded.line_end,
		col_start = excluded.col_start,
		col_end = excluded.col_end,
		symbol_uri = excluded.symbol_uri,
		created_at = CURRENT_TIMESTAMP;
	`
	_, err := s.db.ExecContext(ctx, query,
		n.ID, n.Name, n.Kind, n.FilePath,
		n.LineStart, n.LineEnd, n.ColStart, n.ColEnd, n.SymbolURI,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert node %s: %w", n.ID, err)
	}
	return nil
}

func (s *Store) UpsertEdge(ctx context.Context, e *Edge) error {
	query := `
	INSERT INTO edges (source_id, target_id, relation)
	VALUES (?, ?, ?)
	ON CONFLICT(source_id, target_id, relation) DO NOTHING;
	`
	_, err := s.db.ExecContext(ctx, query, e.SourceID, e.TargetID, e.Relation)
	if err != nil {
		return fmt.Errorf("failed to upsert edge %s->%s: %w", e.SourceID, e.TargetID, err)
	}
	return nil
}

func (s *Store) FindImpact(ctx context.Context, symbolName string) ([]*Node, error) {
	// First find IDs for the symbol name
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM nodes WHERE name = ?", symbolName)
	if err != nil {
		return nil, fmt.Errorf("failed to find symbol %s: %w", symbolName, err)
	}
	defer rows.Close()

	var targetIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		targetIDs = append(targetIDs, id)
	}
	rows.Close()

	if len(targetIDs) == 0 {
		return []*Node{}, nil
	}

	// Build the recursive query
	// Note: SQLite doesn't support arrays in queries easily, so we loop or build a big query.
	// For MVP, finding impact for the first match or all matches combined?
	// Let's combine them.

	// Helper to fetch impacts for a single ID would be cleaner, but let's try to do it in one go or loop.
	// Looping is safer for SQL injection prevention vs dynamic query building.

	uniqueNodes := make(map[string]*Node)

	for _, targetID := range targetIDs {
		query := `
		WITH RECURSIVE impacted AS (
			-- Base case: Direct dependents (who calls/uses targetID)
			SELECT source_id
			FROM edges
			WHERE target_id = ?
			
			UNION
			
			-- Recursive step: Dependents of dependents
			SELECT e.source_id
			FROM edges e
			INNER JOIN impacted i ON e.target_id = i.source_id
		)
		SELECT DISTINCT n.id, n.name, n.kind, n.file_path, n.line_start, n.line_end, n.col_start, n.col_end, n.symbol_uri
		FROM nodes n
		JOIN impacted i ON n.id = i.source_id;
		`

		rows, err := s.db.QueryContext(ctx, query, targetID)
		if err != nil {
			return nil, fmt.Errorf("failed to query impact for %s: %w", targetID, err)
		}

		for rows.Next() {
			n := &Node{}
			if err := rows.Scan(&n.ID, &n.Name, &n.Kind, &n.FilePath, &n.LineStart, &n.LineEnd, &n.ColStart, &n.ColEnd, &n.SymbolURI); err != nil {
				rows.Close()
				return nil, err
			}
			uniqueNodes[n.ID] = n
		}
		rows.Close()
	}

	result := make([]*Node, 0, len(uniqueNodes))
	for _, n := range uniqueNodes {
		result = append(result, n)
	}
	return result, nil
}

func (s *Store) GetSymbolLocation(ctx context.Context, symbolName string) ([]*Node, error) {
	query := `
	SELECT id, name, kind, file_path, line_start, line_end, col_start, col_end, symbol_uri
	FROM nodes
	WHERE name = ?
	ORDER BY file_path;
	`
	rows, err := s.db.QueryContext(ctx, query, symbolName)
	if err != nil {
		return nil, fmt.Errorf("failed to query location for %s: %w", symbolName, err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		n := &Node{}
		if err := rows.Scan(&n.ID, &n.Name, &n.Kind, &n.FilePath, &n.LineStart, &n.LineEnd, &n.ColStart, &n.ColEnd, &n.SymbolURI); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func (s *Store) GetSymbolsInFile(ctx context.Context, filePath string) ([]*Node, error) {
	query := `
	SELECT id, name, kind, file_path, line_start, line_end, col_start, col_end, symbol_uri
	FROM nodes
	WHERE file_path = ?
	ORDER BY line_start;
	`
	rows, err := s.db.QueryContext(ctx, query, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to query symbol map for %s: %w", filePath, err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		n := &Node{}
		if err := rows.Scan(&n.ID, &n.Name, &n.Kind, &n.FilePath, &n.LineStart, &n.LineEnd, &n.ColStart, &n.ColEnd, &n.SymbolURI); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// DeleteNodesByFile removes all nodes and associated edges for a given file.
func (s *Store) DeleteNodesByFile(ctx context.Context, filePath string) error {
	// SQLite will cascade delete edges due to foreign key constraints
	query := `DELETE FROM nodes WHERE file_path = ?`
	_, err := s.db.ExecContext(ctx, query, filePath)
	if err != nil {
		return fmt.Errorf("failed to delete nodes for file %s: %w", filePath, err)
	}
	return nil
}

func (s *Store) Clear(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM edges"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM nodes"); err != nil {
		return err
	}
	return nil
}

// PruneStaleFiles removes any files from the DB that were not found in the latest scan.
func (s *Store) PruneStaleFiles(ctx context.Context, foundFilePaths []string) error {
	// 1. Create a map for O(1) lookups of found files
	keep := make(map[string]bool)
	for _, p := range foundFilePaths {
		keep[p] = true
	}

	// 2. Get all file paths currently in the DB
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT file_path FROM nodes")
	if err != nil {
		return fmt.Errorf("failed to query existing files: %w", err)
	}
	defer rows.Close()

	var dbFiles []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return err
		}
		dbFiles = append(dbFiles, p)
	}
	rows.Close()

	// 3. Delete files that are in DB but not in the found set
	for _, file := range dbFiles {
		if !keep[file] {
			if err := s.DeleteNodesByFile(ctx, file); err != nil {
				return fmt.Errorf("failed to prune stale file %s: %w", file, err)
			}
		}
	}
	return nil
}
