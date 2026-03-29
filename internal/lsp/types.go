// Package lsp contains the JSON-RPC 2.0 and LSP protocol structs used by the
// codemap LSP client.
package lsp

// -------------------------------------------------------------------
// JSON-RPC 2.0
// -------------------------------------------------------------------

// RequestMessage is an outgoing JSON-RPC request.
type RequestMessage struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// NotificationMessage is an outgoing JSON-RPC notification (no ID).
type NotificationMessage struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// ResponseMessage is an incoming JSON-RPC response.
type ResponseMessage struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      *int           `json:"id"`
	Result  any            `json:"result,omitempty"`
	Error   *ResponseError `json:"error,omitempty"`
}

// ResponseError represents a JSON-RPC error object.
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// -------------------------------------------------------------------
// LSP types
// -------------------------------------------------------------------

// Position is a zero-based line + character offset.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a span in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a URI + range pair.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextDocumentIdentifier identifies a document by URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentItem is used in didOpen.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// -------------------------------------------------------------------
// Initialize
// -------------------------------------------------------------------

// InitializeParams is the parameter object for the initialize request.
type InitializeParams struct {
	ProcessID    int                `json:"processId"`
	RootURI      string             `json:"rootUri"`
	Capabilities ClientCapabilities `json:"capabilities"`
}

// ClientCapabilities is intentionally minimal.
type ClientCapabilities struct{}

// InitializeResult is the server's response to initialize.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities contains the fields we care about.
type ServerCapabilities struct {
	ReferencesProvider     bool `json:"referencesProvider"`
	ImplementationProvider bool `json:"implementationProvider"`
}

// -------------------------------------------------------------------
// textDocument/didOpen
// -------------------------------------------------------------------

// DidOpenTextDocumentParams is the parameter for textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// -------------------------------------------------------------------
// textDocument/didClose
// -------------------------------------------------------------------

// DidCloseTextDocumentParams is the parameter for textDocument/didClose.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// -------------------------------------------------------------------
// textDocument/references
// -------------------------------------------------------------------

// ReferenceContext is used in ReferencesParams.
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// ReferencesParams is the parameter for textDocument/references.
type ReferencesParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context"`
}

// -------------------------------------------------------------------
// textDocument/implementation
// -------------------------------------------------------------------

// ImplementationParams is the parameter for textDocument/implementation.
type ImplementationParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// -------------------------------------------------------------------
// textDocument/publishDiagnostics  (server → client notification)
// -------------------------------------------------------------------

// Diagnostic is a single LSP diagnostic entry.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1=error 2=warning 3=info 4=hint
	Code     string `json:"code"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// PublishDiagnosticsParams is the parameter object for
// textDocument/publishDiagnostics notifications.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}
