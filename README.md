# CodeMap

> A high-performance semantic code graph server for AI agents

CodeMap is a [Model Context Protocol (MCP)](https://modelcontextprotocol.io) server that builds and maintains a real-time semantic graph of your codebase. It combines rapid AST parsing with Language Server Protocol integration to provide AI agents with deep code understanding, dependency tracking, and impact analysis.

## Features

🚀 **Automatic Code Graph Generation**
- Tree-sitter AST parsing for Go, Python, JavaScript, TypeScript, and Lua
- LSP integration for cross-file reference resolution
- Real-time graph updates via file watching

⚡ **High Performance**
- Incremental updates (~100ms per file vs 1-2s full scan)
- SQLite with recursive CTEs for efficient graph traversal
- Async LSP initialization with adaptive waiting

🎯 **Production Ready**
- Zero configuration - just run it
- Graceful error handling with actionable messages
- Hard LSP validation prevents incomplete data
- 500ms debouncing for rapid file changes

🔍 **AI-Friendly**
- MCP protocol for seamless AI agent integration
- 4 powerful tools for code analysis
- Always up-to-date graph (auto re-indexes on save)

## Quick Start

### Prerequisites

1. Install [mise-en-place](https://mise.jdx.dev/) (recommended for managing tools and tasks).
2. Install language servers for the languages you use:

```bash
# Go
go install golang.org/x/tools/gopls@latest

# Python
pip install pyright

# TypeScript/JavaScript
npm install -g typescript-language-server typescript

# Lua (macOS)
brew install lua-language-server
```

### Installation

```bash
# Clone and build
git clone https://github.com/yourusername/codemap.git
cd codemap

# Using mise (recommended)
mise run build

# Or using standard Go
go build -o codemap main.go

# Run
./codemap
```

That's it! CodeMap will:
1. Index your workspace (1-2 seconds)
2. Start watching for file changes
3. Launch the MCP server on stdio

## Usage

### Running CodeMap

```bash
# Simply run in your project directory
cd /path/to/your/project
/path/to/codemap

# Or via mise
mise run run

# Output:
# Indexing workspace: /path/to/your/project
# Initial index complete: 47 nodes, 23 edges
# Watching /path/to/your/project for file changes...
# Starting MCP server on stdio...
```

### MCP Configuration

Add to your MCP client configuration:

```json
{
  "mcpServers": {
    "codemap": {
      "command": "/path/to/codemap"
    }
  }
}
```

For Claude Desktop (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "codemap": {
      "command": "/absolute/path/to/codemap",
      "args": []
    }
  }
}
```

### Available Tools

#### 1. `index`
Manually trigger a full re-index of the workspace.

```json
{
  "name": "index",
  "arguments": {
    "force": true
  }
}
```

**Response:** `"Indexed 47 nodes and 23 edges"`

#### 2. `get_symbols_in_file`
List all symbols in a specific file.

```json
{
  "name": "get_symbols_in_file",
  "arguments": {
    "file_path": "/absolute/path/to/main.go"
  }
}
```

**Response:**
```json
[
  {"name": "ProcessOrder", "kind": "function_declaration", "range": "10:0-25:1"},
  {"name": "ValidateOrder", "kind": "function_declaration", "range": "27:0-35:1"},
  {"name": "Order", "kind": "class_definition", "range": "5:0-8:1"}
]
```

#### 3. `find_impact`
Find all downstream dependencies of a symbol (recursive).

```json
{
  "name": "find_impact",
  "arguments": {
    "symbol_name": "ProcessOrder"
  }
}
```

**Response:**
```json
[
  {"name": "CreateInvoice", "file_path": "/path/to/billing.go", "kind": "function_declaration"},
  {"name": "ShipOrder", "file_path": "/path/to/shipping.go", "kind": "function_declaration"},
  {"name": "NotifyCustomer", "file_path": "/path/to/notifications.go", "kind": "function_declaration"}
]
```

#### 4. `get_symbol_location`
Find where a symbol is defined.

```json
{
  "name": "get_symbol_location",
  "arguments": {
    "symbol_name": "ProcessOrder"
  }
}
```

**Response:**
```json
[
  {
    "name": "ProcessOrder",
    "kind": "function_declaration",
    "file_path": "/path/to/orders.go",
    "line_start": 10,
    "line_end": 25,
    "col_start": 0,
    "col_end": 1
  }
]
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     CodeMap                          │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌───────────────────────────────────────────────┐     │
│  │              Initial Index                    │     │
│  │  • Scan workspace with tree-sitter            │     │
│  │  • Store nodes (symbols) in SQLite            │     │
│  │  • LSP enrichment (cross-file references)     │     │
│  │  • Store edges (relationships)                │     │
│  └───────────────────────────────────────────────┘     │
│                       ↓                                 │
│  ┌───────────────────────────────────────────────┐     │
│  │         File Watcher (background)             │     │
│  │  • Monitor file changes (fsnotify)            │     │
│  │  • Debounce (500ms)                           │     │
│  │  • Incremental re-index on save               │     │
│  └───────────────────────────────────────────────┘     │
│                       ↓                                 │
│  ┌───────────────────────────────────────────────┐     │
│  │          MCP Server (foreground)              │     │
│  │  • JSON-RPC over stdio                        │     │
│  │  • 4 tools: index, get_symbols, find_impact,  │     │
│  │    get_location                               │     │
│  └───────────────────────────────────────────────┘     │
│                                                         │
└─────────────────────────────────────────────────────────┘
          ↓                    ↓                    ↓
    ┌──────────┐        ┌──────────┐        ┌──────────┐
    │ Scanner  │        │   LSP    │        │  Store   │
    │(tree-    │        │(gopls,   │        │(SQLite   │
    │sitter)   │        │pyright)  │        │+ CTEs)   │
    └──────────┘        └──────────┘        └──────────┘
```

### Core Components

#### Scanner
- **Technology:** Tree-sitter for AST parsing
- **Languages:** Go, Python, JavaScript, TypeScript, Lua
- **Performance:** Parses ~100 files/second
- **Filtering:** Respects `.gitignore`, skips common ignore dirs

#### LSP Integration
- **Purpose:** Resolve cross-file references and relationships
- **Servers:** gopls, pyright, typescript-language-server, lua-language-server
- **Features:** Definition lookup, implementation tracking, reference finding
- **Validation:** Hard requirement - fails fast if servers missing

#### Graph Store
- **Database:** SQLite with WAL mode
- **Schema:** 
  - `nodes` - Code symbols (functions, classes, etc.)
  - `edges` - Relationships (calls, implements, references)
- **Queries:** Recursive CTEs for dependency traversal
- **Indexing:** Optimized for file_path and symbol_name lookups

#### File Watcher
- **Technology:** fsnotify (cross-platform)
- **Debouncing:** 500ms to avoid rapid re-indexes
- **Incremental:** Only re-scans changed files
- **Events:** CREATE, MODIFY, DELETE, RENAME

### Data Model

**Node:**
```go
{
  "id": "sha256(file_path + symbol_name)",
  "name": "ProcessOrder",
  "kind": "function_declaration",
  "file_path": "/absolute/path/to/orders.go",
  "line_start": 10,
  "line_end": 25,
  "col_start": 0,
  "col_end": 1,
  "symbol_uri": "file:///absolute/path/to/orders.go"
}
```

**Edge:**
```go
{
  "source_id": "node_id_1",
  "target_id": "node_id_2",
  "relation": "calls" | "implements" | "references" | "imports"
}
```

## Performance

| Operation | Time | Notes |
|-----------|------|-------|
| **Initial index (100 files)** | 1-2s | Tree-sitter + LSP enrichment |
| **Re-index single file** | ~100ms | Incremental update |
| **File change detection** | <1ms | OS-level events |
| **Symbol lookup** | <10ms | Indexed query |
| **Recursive dependency query** | <100ms | CTE optimization |
| **Memory usage** | ~50MB | Base + graph data |

## Requirements

### System Requirements
- **OS:** Linux, macOS, or Windows
- **RAM:** 100MB minimum
- **Disk:** Varies by codebase size (SQLite database)
- **Go:** 1.21+ (for building)

### Language Server Requirements

CodeMap **requires** language servers to be installed:

| Language | Server | Installation |
|----------|--------|--------------|
| Go | gopls | `go install golang.org/x/tools/gopls@latest` |
| Python | pyright | `pip install pyright` |
| JavaScript/TypeScript | typescript-language-server | `npm install -g typescript-language-server typescript` |
| Lua | lua-language-server | `brew install lua-language-server` |

**Why required?** Without LSP servers, CodeMap cannot generate edges (relationships between symbols), making the graph incomplete and the `find_impact` tool useless.

### Verification

Check if language servers are installed:

```bash
which gopls                      # Go
which pyright-langserver         # Python
which typescript-language-server # TypeScript/JavaScript
which lua-language-server        # Lua
```

## Examples

### Use Case 1: Impact Analysis

**Question:** "What will break if I change the `ProcessOrder` function?"

```bash
# AI agent calls find_impact
{
  "tool": "find_impact",
  "arguments": {"symbol_name": "ProcessOrder"}
}

# Response shows all downstream dependencies:
# - CreateInvoice (in billing.go)
# - ShipOrder (in shipping.go)  
# - NotifyCustomer (in notifications.go)
```

**Result:** AI knows exactly what to review before making changes.

### Use Case 2: Code Navigation

**Question:** "Where is the `ValidateUser` function defined?"

```bash
{
  "tool": "get_symbol_location",
  "arguments": {"symbol_name": "ValidateUser"}
}

# Response:
# File: /path/to/auth.go
# Lines: 45-67
```

**Result:** AI can read the exact file and lines.

### Use Case 3: File Structure Understanding

**Question:** "What functions are in `main.go`?"

```bash
{
  "tool": "get_symbols_in_file",
  "arguments": {"file_path": "/path/to/main.go"}
}

# Response lists all symbols:
# - main (function)
# - setupServer (function)
# - Config (struct)
```

**Result:** AI understands file organization.

### Use Case 4: Real-time Updates

**Scenario:** Developer edits `orders.go`

```
1. Developer saves file
   ↓
2. CodeMap detects change (via fsnotify)
   ↓
3. Waits 500ms (debounce)
   ↓
4. Re-scans orders.go (~100ms)
   ↓
5. Updates database
   ↓
6. AI's next query sees fresh data
```

**Result:** AI always works with up-to-date code graph.

## Development

### Building from Source

Using `mise` (recommended):
```bash
# Clone repository
git clone https://github.com/yourusername/codemap.git
cd codemap

# Install dependencies and tools
mise install

# Build
mise run build

# Run tests
mise run test

# Format code
mise run fmt

# Lint
mise run vet
```

Using standard Go:
```bash
# Clone repository
git clone https://github.com/yourusername/codemap.git
cd codemap

# Install dependencies
go mod download

# Build
go build -o codemap main.go

# Run tests
go test ./...

# Format code
gofmt -w .

# Lint
go vet ./...
```

### Project Structure

```
codemap/
├── main.go                 # Entry point, orchestrates components
├── go.mod                  # Go module definition
├── go.sum                  # Dependency checksums
├── mise.toml               # Task runner configuration
├── internal/
│   ├── db/                 # SQLite initialization and schema
│   │   └── db.go
│   ├── graph/              # Graph data model and storage
│   │   ├── types.go        # Node and Edge types
│   │   └── store.go        # CRUD operations, recursive queries
│   ├── lsp/                # LSP client implementation
│   │   ├── lsp.go          # Client, Service, enrichment logic
│   │   ├── transport.go    # JSON-RPC message framing
│   │   └── types.go        # LSP protocol types
│   ├── scanner/            # Tree-sitter AST parsing
│   │   ├── scanner.go      # File scanning, node extraction
│   │   └── queries.go      # Tree-sitter query definitions
│   ├── server/             # MCP server implementation
│   │   └── server.go       # Tool registration and handlers
│   └── watcher/            # File system monitoring
│       └── watcher.go      # fsnotify integration, debouncing
├── util/                   # Utility functions
│   ├── git.go              # Git root finding
│   ├── hash.go             # Node ID generation
│   └── uri.go              # File path ↔ URI conversion
└── tests/                  # Integration tests
    ├── integration_test.go
    └── lsp_integration_test.go
```

### Adding a New Language

1. **Add tree-sitter grammar:**
```go
// internal/scanner/scanner.go
import tsrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"

s.languages["rs"] = sitter.NewLanguage(tsrust.Language())
```

2. **Add query:**
```go
// internal/scanner/queries.go
const RustQuery = `
  (function_item name: (identifier) @function_declaration)
  (struct_item name: (type_identifier) @class_definition)
`
Queries["rust"] = RustQuery
```

3. **Add LSP support:**
```go
// internal/lsp/lsp.go
case "rust":
    return "rust-analyzer", []string{}
```

4. **Add installation instructions:**
```go
case "rust":
    return "curl -L https://github.com/rust-analyzer/rust-analyzer/releases/latest/download/rust-analyzer-x86_64-unknown-linux-gnu.gz | gunzip -c - > ~/.local/bin/rust-analyzer"
```

### Running Tests

```bash
# All tests
mise run test

# Specific package
go test ./internal/lsp -v

# Integration tests only
go test ./tests -v

# Race detector (slower but thorough)
go test -race ./...
```

## Troubleshooting

### "Language server(s) not found"

**Problem:** CodeMap can't find required language servers.

**Solution:** Install missing servers:
```bash
# Check which are missing
which gopls pyright-langserver typescript-language-server lua-language-server

# Install missing ones (see Requirements section)
```

### "Failed to init DB"

**Problem:** Cannot create database file.

**Solution:** 
- Check write permissions in current directory
- Ensure `.ctxhub/` directory is writable
- Try running from a different directory

### "Watch limit exceeded" (Linux)

**Problem:** Too many directories to watch.

**Solution:** Increase inotify limit:
```bash
echo fs.inotify.max_user_watches=524288 | sudo tee -a /etc/sysctl.conf
sudo sysctl -p
```

### High Memory Usage

**Problem:** Large codebase causing high memory usage.

**Solution:**
- Check database size: `du -h .ctxhub/codegraph.sqlite`
- Clean old data: `rm -rf .ctxhub/` and re-run
- For very large codebases (>10k files), consider indexing subdirectories separately

### LSP Enrichment Slow

**Problem:** Initial indexing takes >5 seconds.

**Solution:**
- This is normal for language servers indexing the workspace
- Subsequent updates are fast (~100ms)
- Ensure language servers are up-to-date

## FAQ

**Q: Does CodeMap work with monorepos?**  
A: Yes, but consider the size. Very large monorepos (>50k files) may hit system watch limits.

**Q: Can I use CodeMap in CI/CD?**  
A: Yes, but file watching is always on. For CI/CD, you may want a `--index-only` flag (not yet implemented).

**Q: Does it support remote filesystems?**  
A: File watching may not work on network drives. Use locally for best results.

**Q: How much disk space does the database use?**  
A: Roughly 1MB per 1000 nodes. A typical project with 10k symbols = ~10MB database.

**Q: Can multiple CodeMap instances run in the same directory?**  
A: No, SQLite database locking prevents this. Use one instance per workspace.

**Q: Does it preserve the graph between runs?**  
A: Yes! The SQLite database persists in `.ctxhub/codegraph.sqlite`.

**Q: How do I reset the graph?**  
A: Delete the database: `rm -rf .ctxhub/` and restart CodeMap.

## Limitations

- **Language support:** Only Go, Python, JS, TS, Lua (more languages can be added)
- **Single workspace:** Designed for one codebase at a time
- **Local only:** Not designed for remote/distributed use
- **LSP required:** Cannot generate edges without language servers
- **System limits:** File watching subject to OS limits (inotify on Linux)

## Comparison

| Feature | CodeMap | Language Server | IDE Plugin |
|---------|------------|-----------------|------------|
| **Cross-language** | ✅ 5 languages | ❌ Single | ⚠️ Varies |
| **Persistent graph** | ✅ SQLite | ❌ In-memory | ⚠️ Varies |
| **MCP protocol** | ✅ Native | ❌ | ❌ |
| **Auto re-index** | ✅ File watching | ❌ | ✅ IDE-specific |
| **Dependency analysis** | ✅ Recursive CTEs | ⚠️ Limited | ⚠️ Limited |
| **Zero config** | ✅ Just run | ❌ Complex setup | ✅ Built-in |
| **AI integration** | ✅ MCP tools | ⚠️ LSP adapter needed | ❌ |

## Contributing

Contributions welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Guidelines

- Follow standard Go idioms
- Add tests for new features
- Update documentation
- Run `gofmt` before committing
- Ensure `go vet` passes

## License

GNU General Public License v3.0 - see [LICENSE](LICENSE) file for details.

## Credits

Built with:
- [Tree-sitter](https://tree-sitter.github.io/) - AST parsing
- [Model Context Protocol](https://modelcontextprotocol.io) - AI agent integration
- [fsnotify](https://github.com/fsnotify/fsnotify) - File watching
- [SQLite](https://www.sqlite.org/) - Graph storage

