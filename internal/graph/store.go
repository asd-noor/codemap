// Package graph implements the SQLite-backed semantic graph store for the
// codemap engine: nodes (symbols) and edges (relationships).
package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"

	dbutil "codemap/internal/db"
)

// ErrNotIndexed is returned by OpenReadOnly when no index database exists yet
// for the project. Callers should instruct the user to run `codemap index`.
var ErrNotIndexed = errors.New("project has not been indexed yet — run: codemap index")

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS nodes (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL,
    file_path  TEXT NOT NULL,
    line_start INTEGER NOT NULL,
    line_end   INTEGER NOT NULL,
    col_start  INTEGER NOT NULL,
    col_end    INTEGER NOT NULL,
    name_line  INTEGER NOT NULL DEFAULT 0,
    name_col   INTEGER NOT NULL DEFAULT 0,
    symbol_uri TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_nodes_file_path ON nodes(file_path);
CREATE INDEX IF NOT EXISTS idx_nodes_name      ON nodes(name);

CREATE TABLE IF NOT EXISTS edges (
    source_id  TEXT NOT NULL,
    target_id  TEXT NOT NULL,
    relation   TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (source_id, target_id, relation),
    FOREIGN KEY (source_id) REFERENCES nodes(id) ON DELETE CASCADE,
    FOREIGN KEY (target_id) REFERENCES nodes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);

-- LSP diagnostics per file (overwritten on each indexing pass).
CREATE TABLE IF NOT EXISTS diagnostics (
    id         TEXT PRIMARY KEY,
    file_path  TEXT    NOT NULL,
    line       INTEGER NOT NULL,
    col        INTEGER NOT NULL,
    severity   INTEGER NOT NULL, -- 1=error 2=warning 3=information 4=hint
    code       TEXT    NOT NULL DEFAULT '',
    source     TEXT    NOT NULL DEFAULT '',
    message    TEXT    NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_diagnostics_file_path ON diagnostics(file_path);
CREATE INDEX IF NOT EXISTS idx_diagnostics_severity  ON diagnostics(severity);

-- Edges linking a diagnostic to the enclosing symbol node.
CREATE TABLE IF NOT EXISTS diagnostic_edges (
    diagnostic_id TEXT NOT NULL,
    node_id       TEXT NOT NULL,
    PRIMARY KEY (diagnostic_id, node_id),
    FOREIGN KEY (diagnostic_id) REFERENCES diagnostics(id) ON DELETE CASCADE,
    FOREIGN KEY (node_id)       REFERENCES nodes(id)       ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_diagnostic_edges_node ON diagnostic_edges(node_id);
`

// Store is the SQLite-backed semantic graph store.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the codemap SQLite database at dbPath,
// applies the schema, and returns the store.
func Open(dbPath string) (*Store, error) {
	if err := dbutil.EnsureDir(dbPath); err != nil {
		return nil, fmt.Errorf("graph: ensure db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?cache=shared&mode=rwc&_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("graph: open db: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("graph: apply schema: %w", err)
	}

	return &Store{db: db}, nil
}

// OpenReadOnly opens an existing codemap database at dbPath in read-only mode.
// If the file does not exist it returns ErrNotIndexed.
func OpenReadOnly(dbPath string) (*Store, error) {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, ErrNotIndexed
	}

	db, err := sql.Open("sqlite3", dbPath+"?mode=ro&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("graph: open db: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }

// BulkUpsertNodes inserts or replaces a batch of nodes inside a single transaction.
func (s *Store) BulkUpsertNodes(ctx context.Context, nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR REPLACE INTO nodes
		 (id, name, kind, file_path, line_start, line_end, col_start, col_end, name_line, name_col, symbol_uri)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, n := range nodes {
		if _, err := stmt.ExecContext(ctx,
			n.ID, n.Name, n.Kind, n.FilePath,
			n.LineStart, n.LineEnd, n.ColStart, n.ColEnd,
			n.NameLine, n.NameCol, n.SymbolURI,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// BulkUpsertEdges inserts or ignores a batch of edges inside a single transaction.
func (s *Store) BulkUpsertEdges(ctx context.Context, edges []Edge) error {
	if len(edges) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO edges (source_id, target_id, relation) VALUES (?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range edges {
		if _, err := stmt.ExecContext(ctx, e.SourceID, e.TargetID, e.Relation); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetSymbolsInFile returns all nodes for the given file path, ordered by line_start.
func (s *Store) GetSymbolsInFile(ctx context.Context, filePath string) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, kind, file_path, line_start, line_end, col_start, col_end,
		        name_line, name_col, COALESCE(symbol_uri,'')
		 FROM nodes WHERE file_path = ? ORDER BY line_start`,
		filePath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// GetSymbolLocation returns all nodes matching the given name, ordered by file_path.
func (s *Store) GetSymbolLocation(ctx context.Context, name string) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, kind, file_path, line_start, line_end, col_start, col_end,
		        name_line, name_col, COALESCE(symbol_uri,'')
		 FROM nodes WHERE name = ? ORDER BY file_path`,
		name,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// FindNode returns the smallest node that encloses (filePath, line, col).
// "Smallest" means the node with the smallest (line_end - line_start) span.
func (s *Store) FindNode(ctx context.Context, filePath string, line, col int) (*Node, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, kind, file_path, line_start, line_end, col_start, col_end,
		        name_line, name_col, COALESCE(symbol_uri,'')
		 FROM nodes
		 WHERE file_path = ?
		   AND line_start <= ?
		   AND line_end   >= ?
		 ORDER BY (line_end - line_start) ASC
		 LIMIT 1`,
		filePath, line, line,
	)
	n := &Node{}
	err := row.Scan(&n.ID, &n.Name, &n.Kind, &n.FilePath,
		&n.LineStart, &n.LineEnd, &n.ColStart, &n.ColEnd,
		&n.NameLine, &n.NameCol, &n.SymbolURI)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

// FindImpact returns all nodes that transitively depend on any node named symbolName,
// using a recursive CTE traversing edges in reverse (target → source direction).
func (s *Store) FindImpact(ctx context.Context, symbolName string) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE impacted AS (
		    SELECT source_id
		    FROM edges
		    WHERE target_id IN (SELECT id FROM nodes WHERE name = ?)
		    UNION
		    SELECT e.source_id
		    FROM edges e
		    INNER JOIN impacted i ON e.target_id = i.source_id
		)
		SELECT DISTINCT n.id, n.name, n.kind, n.file_path,
		                n.line_start, n.line_end, n.col_start, n.col_end,
		                n.name_line, n.name_col,
		                COALESCE(n.symbol_uri,'')
		FROM nodes n
		JOIN impacted i ON n.id = i.source_id`,
		symbolName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// DeleteNodesByFile removes all nodes (and cascaded edges) for the given file path.
func (s *Store) DeleteNodesByFile(ctx context.Context, filePath string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE file_path = ?`, filePath)
	return err
}

// PruneStaleFiles removes DB entries for files that are no longer in the given
// set of live file paths.
func (s *Store) PruneStaleFiles(ctx context.Context, livePaths map[string]struct{}) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT file_path FROM nodes`)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		if _, ok := livePaths[p]; !ok {
			stale = append(stale, p)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range stale {
		if err := s.DeleteNodesByFile(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// Clear truncates all nodes, edges, and diagnostics.
func (s *Store) Clear(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM edges; DELETE FROM nodes; DELETE FROM diagnostics;`)
	return err
}

// NodeCount returns the total number of nodes in the store.
func (s *Store) NodeCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes`).Scan(&n)
	return n, err
}

// EdgeCount returns the total number of edges in the store.
func (s *Store) EdgeCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges`).Scan(&n)
	return n, err
}

// scanNodes reads all rows into a Node slice.
func scanNodes(rows *sql.Rows) ([]Node, error) {
	var nodes []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.Name, &n.Kind, &n.FilePath,
			&n.LineStart, &n.LineEnd, &n.ColStart, &n.ColEnd,
			&n.NameLine, &n.NameCol, &n.SymbolURI); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// ── Diagnostics ───────────────────────────────────────────────────────────────

// UpsertDiagnosticsForFile replaces all diagnostics for the given file with
// the provided slice (atomic delete + insert inside one transaction).
func (s *Store) UpsertDiagnosticsForFile(ctx context.Context, filePath string, diags []Diagnostic) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// Remove stale diagnostics (cascade deletes diagnostic_edges too).
	if _, err := tx.ExecContext(ctx, `DELETE FROM diagnostics WHERE file_path = ?`, filePath); err != nil {
		return err
	}

	if len(diags) > 0 {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT OR REPLACE INTO diagnostics (id, file_path, line, col, severity, code, source, message)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, d := range diags {
			if _, err := stmt.ExecContext(ctx,
				d.ID, d.FilePath, d.Line, d.Col, d.Severity, d.Code, d.Source, d.Message,
			); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// BulkUpsertDiagnosticEdges inserts diagnostic→node edges, ignoring duplicates.
func (s *Store) BulkUpsertDiagnosticEdges(ctx context.Context, edges []DiagnosticEdge) error {
	if len(edges) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO diagnostic_edges (diagnostic_id, node_id) VALUES (?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range edges {
		if _, err := stmt.ExecContext(ctx, e.DiagnosticID, e.NodeID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetAllDiagnostics returns every diagnostic in the store, ordered by
// severity (most severe first) then file_path, then line.
func (s *Store) GetAllDiagnostics(ctx context.Context) ([]Diagnostic, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, file_path, line, col, severity, code, source, message
		 FROM diagnostics
		 ORDER BY severity ASC, file_path ASC, line ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiagnostics(rows)
}

// GetDiagnosticsForFile returns all diagnostics for a specific file.
func (s *Store) GetDiagnosticsForFile(ctx context.Context, filePath string) ([]Diagnostic, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, file_path, line, col, severity, code, source, message
		 FROM diagnostics WHERE file_path = ?
		 ORDER BY line ASC`,
		filePath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiagnostics(rows)
}

// DiagnosticCount returns the total number of diagnostics in the store.
func (s *Store) DiagnosticCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM diagnostics`).Scan(&n)
	return n, err
}

// scanDiagnostics reads all rows into a Diagnostic slice.
func scanDiagnostics(rows *sql.Rows) ([]Diagnostic, error) {
	var diags []Diagnostic
	for rows.Next() {
		var d Diagnostic
		if err := rows.Scan(&d.ID, &d.FilePath, &d.Line, &d.Col,
			&d.Severity, &d.Code, &d.Source, &d.Message); err != nil {
			return nil, err
		}
		diags = append(diags, d)
	}
	return diags, rows.Err()
}
