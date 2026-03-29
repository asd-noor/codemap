// Package graph defines the core data types and constants for the codemap
// semantic graph: nodes (symbols) and edges (relationships).
package graph

// Relation constants for Edge.Relation.
const (
	RelationCalls      = "calls"
	RelationImplements = "implements"
	RelationReferences = "references"
	RelationImports    = "imports"
)

// Diagnostic severity constants (mirrors LSP DiagnosticSeverity).
const (
	DiagnosticSeverityError       = 1
	DiagnosticSeverityWarning     = 2
	DiagnosticSeverityInformation = 3
	DiagnosticSeverityHint        = 4
)

// Diagnostic represents an LSP diagnostic stored in the semantic graph.
type Diagnostic struct {
	ID       string // stable hash of (FilePath + ":" + line + ":" + col + ":" + message)
	FilePath string
	Line     int
	Col      int
	Severity int    // 1=error, 2=warning, 3=information, 4=hint
	Code     string // language-specific diagnostic code (may be empty)
	Source   string // tool/linter that produced the diagnostic (may be empty)
	Message  string
}

// DiagnosticEdge links a Diagnostic to the enclosing symbol Node.
type DiagnosticEdge struct {
	DiagnosticID string
	NodeID       string
}

// Node represents a named symbol in the codebase.
type Node struct {
	ID        string // stable SHA256 hash of (FilePath + ":" + Name + ":" + Kind)
	Name      string
	Kind      string // function | method | type | class | interface | ...
	FilePath  string
	LineStart int
	LineEnd   int
	ColStart  int
	ColEnd    int
	NameLine  int    // line of the name identifier (1-indexed); used for LSP position
	NameCol   int    // column of the name identifier (1-indexed); used for LSP position
	SymbolURI string // LSP file:// URI, populated during LSP enrichment
}

// Edge represents a directed relationship between two symbols.
type Edge struct {
	SourceID string
	TargetID string
	Relation string // one of the Relation* constants
}
