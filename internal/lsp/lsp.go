package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"codemap/internal/graph"
	"codemap/util"
)

// Service manages LSP clients for different languages.
type Service struct {
	clients map[string]*Client
	mu      sync.Mutex
	config  ServiceConfig
}

// ServiceConfig allows overriding language server command paths.
type ServiceConfig struct {
	GoPath         string
	PythonPath     string
	TypeScriptPath string
	LuaPath        string
	ZigPath        string
}

// EnrichmentStats provides statistics about the enrichment process.
type EnrichmentStats struct {
	FilesProcessed  int
	FilesSkipped    int
	LanguageServers map[string]bool
	EdgesGenerated  int
	Errors          []string
}

func NewService() *Service {
	return NewServiceWithConfig(ServiceConfig{})
}

func NewServiceWithConfig(config ServiceConfig) *Service {
	return &Service{
		clients: make(map[string]*Client),
		config:  config,
	}
}

// Client represents a connection to a language server.
type Client struct {
	cmd      *exec.Cmd
	lang     string
	stdin    io.Writer
	stdout   *bufio.Reader
	seq      int
	mu       sync.Mutex
	pending  map[int]chan responseOrError
	errChan  chan error
	openDocs map[string]int // URI -> version
	initTime time.Time      // When the server was initialized
}

type responseOrError struct {
	data json.RawMessage
	err  error
}

func (s *Service) getClient(lang string) *Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clients[lang]
}

// StartClient starts an LSP server for the given language.
func (s *Service) StartClient(ctx context.Context, lang string, cmdPath string, args []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If already running, return
	if c, ok := s.clients[lang]; ok && c.cmd.Process != nil {
		return nil
	}

	cmd := exec.CommandContext(ctx, cmdPath, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	// Stderr to parent stderr for debugging
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s lsp: %w", lang, err)
	}

	c := &Client{
		cmd:      cmd,
		lang:     lang,
		stdin:    stdin,
		stdout:   bufio.NewReader(stdout),
		seq:      0,
		pending:  make(map[int]chan responseOrError),
		errChan:  make(chan error, 1),
		openDocs: make(map[string]int),
	}
	s.clients[lang] = c

	// Start background reader
	go c.readLoop()

	// Initialize Handshake
	cwd, _ := os.Getwd()
	initParams := InitializeParams{
		ProcessID:    os.Getpid(),
		RootURI:      util.PathToURI(cwd),
		Capabilities: ClientCapabilities{},
	}

	// Use context with timeout for initialization
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if _, err := c.CallWithContext(initCtx, "initialize", initParams); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	// Send initialized notification
	notif := Request{
		JSONRPC: "2.0",
		Method:  "initialized",
		Params:  struct{}{},
	}
	WriteMessage(c.stdin, notif)

	// Store initialization time for later checks
	c.initTime = time.Now()

	log.Printf("Started %s language server (indexing in background)", lang)

	return nil
}

// Call sends a request and waits for the response with timeout.
func (c *Client) Call(method string, params interface{}) (json.RawMessage, error) {
	return c.CallWithContext(context.Background(), method, params)
}

// CallWithContext sends a request and waits for the response with context cancellation.
func (c *Client) CallWithContext(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	c.seq++
	id := c.seq
	ch := make(chan responseOrError, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := WriteMessage(c.stdin, req); err != nil {
		return nil, err
	}

	// Wait for response, timeout, or server error
	select {
	case res := <-ch:
		return res.data, res.err
	case err := <-c.errChan:
		return nil, fmt.Errorf("LSP server error: %w", err)
	case <-ctx.Done():
		return nil, fmt.Errorf("LSP call timeout: %w", ctx.Err())
	}
}

func (c *Client) readLoop() {
	for {
		msgBytes, err := ReadMessage(c.stdout)
		if err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "closed") {
				log.Printf("LSP read error: %v", err)
				select {
				case c.errChan <- err:
				default:
				}
			}
			return
		}

		// Try to decode as Response
		var rawResp struct {
			Result json.RawMessage `json:"result"`
			Error  *RPCError       `json:"error"`
			ID     interface{}     `json:"id"`
		}

		if err := json.Unmarshal(msgBytes, &rawResp); err == nil {
			// LSP IDs can be int or string
			var id int
			var idSet bool

			switch v := rawResp.ID.(type) {
			case float64:
				id = int(v)
				idSet = true
			case int:
				id = v
				idSet = true
			}

			if idSet {
				c.mu.Lock()
				ch, ok := c.pending[id]
				c.mu.Unlock()

				if ok {
					var resErr error
					if rawResp.Error != nil {
						resErr = fmt.Errorf("RPC error %d: %s", rawResp.Error.Code, rawResp.Error.Message)
					}
					ch <- responseOrError{data: rawResp.Result, err: resErr}
				}
			}
		}
		// Notifications (no ID) or unrecognized messages are ignored for now
	}
}

// Notify sends a notification (request without expecting a response).
func (c *Client) Notify(method string, params interface{}) error {
	notif := Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return WriteMessage(c.stdin, notif)
}

// DidOpen notifies the server that a document has been opened.
func (c *Client) DidOpen(ctx context.Context, uri, languageID, text string) error {
	c.mu.Lock()
	c.openDocs[uri] = 1
	c.mu.Unlock()

	params := DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    1,
			Text:       text,
		},
	}
	return c.Notify("textDocument/didOpen", params)
}

// DidClose notifies the server that a document has been closed.
func (c *Client) DidClose(ctx context.Context, uri string) error {
	c.mu.Lock()
	delete(c.openDocs, uri)
	c.mu.Unlock()

	params := DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}
	return c.Notify("textDocument/didClose", params)
}

// GetDefinition requests the definition location of a symbol.
func (c *Client) GetDefinition(ctx context.Context, uri string, line, char int) ([]Location, error) {
	params := DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: char},
	}

	// Add timeout if context doesn't have one
	ctx, cancel := ensureTimeout(ctx, 10*time.Second)
	defer cancel()

	resBytes, err := c.CallWithContext(ctx, "textDocument/definition", params)
	if err != nil {
		return nil, err
	}

	// Definition can return Location, []Location, or LocationLink[]
	// For simplicity, handle Location and []Location
	var locs []Location

	// Try single Location first
	var singleLoc Location
	if err := json.Unmarshal(resBytes, &singleLoc); err == nil && singleLoc.URI != "" {
		return []Location{singleLoc}, nil
	}

	// Try array of Locations
	if err := json.Unmarshal(resBytes, &locs); err != nil {
		return nil, fmt.Errorf("failed to parse definition response: %w", err)
	}

	return locs, nil
}

// GetImplementation requests the implementation locations of a symbol.
func (c *Client) GetImplementation(ctx context.Context, uri string, line, char int) ([]Location, error) {
	params := ImplementationParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: char},
	}

	ctx, cancel := ensureTimeout(ctx, 10*time.Second)
	defer cancel()

	resBytes, err := c.CallWithContext(ctx, "textDocument/implementation", params)
	if err != nil {
		return nil, err
	}

	var locs []Location
	if err := json.Unmarshal(resBytes, &locs); err != nil {
		return nil, fmt.Errorf("failed to parse implementation response: %w", err)
	}

	return locs, nil
}

// GetReferences requests all references to a symbol.
func (c *Client) GetReferences(ctx context.Context, uri string, line, char int, includeDeclaration bool) ([]Location, error) {
	params := ReferenceParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: char},
		Context:      ReferenceContext{IncludeDeclaration: includeDeclaration},
	}

	ctx, cancel := ensureTimeout(ctx, 10*time.Second)
	defer cancel()

	resBytes, err := c.CallWithContext(ctx, "textDocument/references", params)
	if err != nil {
		return nil, err
	}

	var locs []Location
	if err := json.Unmarshal(resBytes, &locs); err != nil {
		return nil, fmt.Errorf("failed to parse references response: %w", err)
	}

	return locs, nil
}

// GetHover requests hover information for a symbol.
func (c *Client) GetHover(ctx context.Context, uri string, line, char int) (*Hover, error) {
	params := HoverParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: char},
	}

	ctx, cancel := ensureTimeout(ctx, 10*time.Second)
	defer cancel()

	resBytes, err := c.CallWithContext(ctx, "textDocument/hover", params)
	if err != nil {
		return nil, err
	}

	var hover Hover
	if err := json.Unmarshal(resBytes, &hover); err != nil {
		return nil, fmt.Errorf("failed to parse hover response: %w", err)
	}

	return &hover, nil
}

// GetDocumentSymbols requests all symbols in a document.
func (c *Client) GetDocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	params := DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}

	ctx, cancel := ensureTimeout(ctx, 10*time.Second)
	defer cancel()

	resBytes, err := c.CallWithContext(ctx, "textDocument/documentSymbol", params)
	if err != nil {
		return nil, err
	}

	var symbols []DocumentSymbol
	if err := json.Unmarshal(resBytes, &symbols); err != nil {
		return nil, fmt.Errorf("failed to parse document symbols response: %w", err)
	}

	return symbols, nil
}

// NodeResolver is an interface to find nodes by location.
type NodeResolver interface {
	FindNode(ctx context.Context, path string, line, col int) (*graph.Node, error)
}

// Enrich uses LSP to find cross-file references and generate edges.
// Returns edges and statistics about the enrichment process.
func (s *Service) Enrich(ctx context.Context, nodes []*graph.Node, resolver NodeResolver) ([]*graph.Edge, error) {
	stats := &EnrichmentStats{
		LanguageServers: make(map[string]bool),
		Errors:          []string{},
	}

	// Detect required language servers from the codebase
	requiredLangs := s.detectRequiredLanguages(nodes)
	if len(requiredLangs) == 0 {
		log.Printf("No supported languages detected")
		return nil, nil
	}

	// Validate that language servers are installed
	if err := s.validateLanguageServers(requiredLangs); err != nil {
		return nil, err
	}

	// Auto-start language servers based on files we see
	langServers := s.detectAndStartLanguageServers(ctx, nodes)
	stats.LanguageServers = langServers

	if len(langServers) == 0 {
		return nil, fmt.Errorf("failed to start any language servers")
	}

	// Wait adaptively for indexing - only blocks if servers just started
	s.waitForIndexing(langServers)

	// Open documents in LSP
	openedDocs := make(map[string]bool)
	var docsMu sync.Mutex

	defer func() {
		// Close all opened documents
		for uri := range openedDocs {
			if c := s.getClientByURI(uri); c != nil {
				c.DidClose(ctx, uri)
			}
		}
	}()

	// Use a worker pool for enrichment
	const numWorkers = 10
	nodeChan := make(chan *graph.Node, len(nodes))
	edgeChan := make(chan []*graph.Edge, len(nodes))
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range nodeChan {
				lang := getLang(n.FilePath)
				client := s.getClient(lang)
				if client == nil {
					continue
				}

				// Ensure document is open
				uri := util.PathToURI(n.FilePath)
				docsMu.Lock()
				isOpen := openedDocs[uri]
				if !isOpen {
					text, err := os.ReadFile(n.FilePath)
					if err != nil {
						errMsg := fmt.Sprintf("Failed to read file %s: %v", n.FilePath, err)
						log.Println(errMsg)
						docsMu.Unlock()
						continue
					}

					langID := getLanguageID(lang)
					if err := client.DidOpen(ctx, uri, langID, string(text)); err != nil {
						errMsg := fmt.Sprintf("Failed to open document %s: %v", uri, err)
						log.Println(errMsg)
						docsMu.Unlock()
						continue
					}
					openedDocs[uri] = true
				}
				docsMu.Unlock()

				// Only process definitions (functions, classes, methods)
				if n.Name == "" || !isDefinitionKind(n.Kind) {
					continue
				}

				var nodeEdges []*graph.Edge
				// Find references to this symbol
				refEdges := s.findReferenceEdges(ctx, client, n, resolver)
				nodeEdges = append(nodeEdges, refEdges...)

				// Find implementations if this is an interface
				if isInterfaceKind(n.Kind) {
					implEdges := s.findImplementationEdges(ctx, client, n, resolver)
					nodeEdges = append(nodeEdges, implEdges...)
				}
				edgeChan <- nodeEdges
			}
		}()
	}

	// Feed workers
	for _, n := range nodes {
		nodeChan <- n
	}
	close(nodeChan)

	// Collect results
	go func() {
		wg.Wait()
		close(edgeChan)
	}()

	var edges []*graph.Edge
	for eList := range edgeChan {
		edges = append(edges, eList...)
	}

	stats.EdgesGenerated = len(edges)
	log.Printf("Enrichment complete: %d edges generated", len(edges))

	return edges, nil
}

// detectAndStartLanguageServers detects languages and starts appropriate servers.
func (s *Service) detectAndStartLanguageServers(ctx context.Context, nodes []*graph.Node) map[string]bool {
	langSet := make(map[string]bool)
	for _, n := range nodes {
		if lang := getLang(n.FilePath); lang != "" {
			langSet[lang] = true
		}
	}

	started := make(map[string]bool)
	for lang := range langSet {
		cmdPath, args := s.getLanguageServerCommand(lang)
		if cmdPath == "" {
			continue
		}

		if err := s.StartClient(ctx, lang, cmdPath, args); err != nil {
			log.Printf("Warning: Failed to start %s language server: %v", lang, err)
		} else {
			started[lang] = true
			log.Printf("Started %s language server", lang)
		}
	}

	return started
}

// detectRequiredLanguages scans nodes and returns unique languages needed.
func (s *Service) detectRequiredLanguages(nodes []*graph.Node) map[string]bool {
	langSet := make(map[string]bool)
	for _, n := range nodes {
		lang := getLang(n.FilePath)
		if lang != "" {
			langSet[lang] = true
		}
	}
	return langSet
}

// validateLanguageServers checks if required language servers are installed.
func (s *Service) validateLanguageServers(requiredLangs map[string]bool) error {
	var missing []string
	var instructions []string

	for lang := range requiredLangs {
		cmdPath, _ := s.getLanguageServerCommand(lang)
		if cmdPath == "" {
			continue // Language not supported, skip
		}

		if !isCommandAvailable(cmdPath) {
			missing = append(missing, lang)
			if instruction := getLanguageServerInstallInstructions(lang); instruction != "" {
				instructions = append(instructions, fmt.Sprintf("  %s: %s", lang, instruction))
			}
		}
	}

	if len(missing) > 0 {
		var firstCmd string
		if cmdPath, _ := s.getLanguageServerCommand(missing[0]); cmdPath != "" {
			firstCmd = cmdPath
		} else {
			firstCmd = "gopls"
		}

		errorMsg := fmt.Sprintf(
			"❌ Language server(s) not found: %v\n\n"+
				"CodeFinder requires LSP servers for dependency analysis.\n"+
				"Without them, find_impact tool will not work.\n\n"+
				"Install missing servers:\n%s\n\n"+
				"After installation, verify with: which %s",
			missing,
			strings.Join(instructions, "\n"),
			firstCmd,
		)
		return fmt.Errorf("%s", errorMsg)
	}

	return nil
}

// waitForIndexing waits adaptively for language servers to index.
// Only waits if servers were recently started; skips if already had time.
func (s *Service) waitForIndexing(langServers map[string]bool) {
	const minIndexTime = 5 * time.Second // Increased for reliability

	s.mu.Lock()
	defer s.mu.Unlock()

	// Find the most recently started server
	var newestInitTime time.Time
	for lang := range langServers {
		if client, ok := s.clients[lang]; ok {
			if newestInitTime.IsZero() || client.initTime.After(newestInitTime) {
				newestInitTime = client.initTime
			}
		}
	}

	if newestInitTime.IsZero() {
		return // No servers to wait for
	}

	elapsed := time.Since(newestInitTime)
	if elapsed < minIndexTime {
		waitTime := minIndexTime - elapsed
		log.Printf("[Background] Waiting %.1fs for language servers to index workspace...", waitTime.Seconds())
		time.Sleep(waitTime)
	} else {
		log.Printf("[Background] Language servers already had %.1fs to index, proceeding immediately", elapsed.Seconds())
	}
}

// findReferenceEdges finds all references to a symbol and creates edges.
func (s *Service) findReferenceEdges(ctx context.Context, client *Client, n *graph.Node, resolver NodeResolver) []*graph.Edge {
	var edges []*graph.Edge

	uri := util.PathToURI(n.FilePath)
	locs, err := client.GetReferences(ctx, uri, n.LineStart-1, n.ColStart-1, false)
	if err != nil {
		// Not all symbols have references, this is expected
		return edges
	}

	for _, loc := range locs {
		targetPath := util.URIToPath(loc.URI)
		// Look up the node that contains this reference (the caller)
		sourceNode, err := resolver.FindNode(ctx, targetPath, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
		if err != nil {
			continue // Skip if lookup fails
		}

		if sourceNode != nil && sourceNode.ID != n.ID {
			edges = append(edges, &graph.Edge{
				SourceID: sourceNode.ID,
				TargetID: n.ID,
				Relation: "references",
			})
		}
	}

	return edges
}

// findImplementationEdges finds implementations of an interface.
func (s *Service) findImplementationEdges(ctx context.Context, client *Client, n *graph.Node, resolver NodeResolver) []*graph.Edge {
	var edges []*graph.Edge

	uri := util.PathToURI(n.FilePath)
	locs, err := client.GetImplementation(ctx, uri, n.LineStart-1, n.ColStart-1)
	if err != nil {
		return edges
	}

	for _, loc := range locs {
		targetPath := util.URIToPath(loc.URI)
		implNode, err := resolver.FindNode(ctx, targetPath, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
		if err != nil {
			continue
		}

		if implNode != nil && implNode.ID != n.ID {
			edges = append(edges, &graph.Edge{
				SourceID: implNode.ID,
				TargetID: n.ID,
				Relation: "implements",
			})
		}
	}

	return edges
}

// getClientByURI returns the client for a given URI.
func (s *Service) getClientByURI(uri string) *Client {
	// Extract language from URI (simplified)
	path := util.URIToPath(uri)
	lang := getLang(path)
	return s.getClient(lang)
}

func (s *Service) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		if c.cmd.Process != nil {
			c.cmd.Process.Kill()
		}
	}
}

// ensureTimeout wraps a context with a timeout if it doesn't already have one.
func ensureTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func getLang(path string) string {
	// Simple mapping based on file extension
	if len(path) > 3 && path[len(path)-3:] == ".go" {
		return "go"
	}
	if len(path) > 3 && path[len(path)-3:] == ".py" {
		return "python"
	}
	if len(path) > 3 && path[len(path)-3:] == ".js" {
		return "javascript"
	}
	if len(path) > 3 && path[len(path)-3:] == ".ts" {
		return "typescript"
	}
	if len(path) > 4 && path[len(path)-4:] == ".tsx" {
		return "typescript"
	}
	if len(path) > 4 && path[len(path)-4:] == ".jsx" {
		return "javascript"
	}
	if len(path) > 4 && path[len(path)-4:] == ".lua" {
		return "lua"
	}
	if len(path) > 4 && path[len(path)-4:] == ".zig" {
		return "zig"
	}
	return ""
}

func getLanguageID(lang string) string {
	// LSP language IDs
	switch lang {
	case "go":
		return "go"
	case "python":
		return "python"
	case "javascript":
		return "javascript"
	case "typescript":
		return "typescript"
	case "lua":
		return "lua"
	case "zig":
		return "zig"
	default:
		return lang
	}
}

func (s *Service) getLanguageServerCommand(lang string) (string, []string) {
	// Returns command path and args for starting a language server
	switch lang {
	case "go":
		if path := strings.TrimSpace(s.config.GoPath); path != "" {
			return path, []string{"serve"}
		}
		return "gopls", []string{"serve"}
	case "python":
		if path := strings.TrimSpace(s.config.PythonPath); path != "" {
			return path, []string{"--stdio"}
		}
		return "pyright-langserver", []string{"--stdio"}
	case "javascript", "typescript":
		if path := strings.TrimSpace(s.config.TypeScriptPath); path != "" {
			return path, []string{"--stdio"}
		}
		return "typescript-language-server", []string{"--stdio"}
	case "lua":
		if path := strings.TrimSpace(s.config.LuaPath); path != "" {
			return path, []string{"--stdio"}
		}
		return "lua-language-server", []string{"--stdio"}
	case "zig":
		if path := strings.TrimSpace(s.config.ZigPath); path != "" {
			return path, nil
		}
		return "zls", nil
	default:
		return "", nil
	}
}

func getLanguageServerInstallInstructions(lang string) string {
	// Returns installation instructions for a language server
	switch lang {
	case "go":
		return "go install golang.org/x/tools/gopls@latest"
	case "python":
		return "pip install pyright"
	case "javascript", "typescript":
		return "npm install -g typescript-language-server typescript"
	case "lua":
		return "brew install lua-language-server  # or download from github.com/LuaLS/lua-language-server"
	case "zig":
		return "brew install zls  # or build from github.com/zigtools/zls"
	default:
		return ""
	}
}

func isCommandAvailable(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func isDefinitionKind(kind string) bool {
	// Check if this node kind represents a definition we want to track
	definitionKinds := map[string]bool{
		"function_declaration":  true,
		"method_declaration":    true,
		"method_definition":     true,
		"function_definition":   true,
		"class_definition":      true,
		"class_declaration":     true,
		"interface_declaration": true,
		"type_definition":       true,
	}
	return definitionKinds[kind]
}

func isInterfaceKind(kind string) bool {
	// Check if this is an interface/protocol that can be implemented
	return kind == "interface_declaration" || kind == "protocol_declaration"
}
