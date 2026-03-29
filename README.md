# CodeMap

> Semantic code-graph engine — MCP server and CLI

CodeMap builds and maintains a real-time semantic graph of your codebase using Tree-sitter AST parsing and Language Server Protocol enrichment. It exposes the graph both as an **MCP server** (for AI agents) and as a **human-friendly CLI**.

---

## Features

- **Multi-language AST parsing** — Go, Python, JavaScript, TypeScript, Lua, Zig, Templ
- **LSP enrichment** — cross-file references and interface implementations via gopls, pylsp, typescript-language-server, lua-language-server, zls, templ lsp
- **Real-time updates** — file watcher with 500 ms debounce; background daemon with 5-minute idle timeout
- **LSP diagnostics** — errors, warnings, hints captured during indexing and queryable
- **Persistent graph** — SQLite with WAL mode, recursive CTEs for dependency traversal
- **Auto-install LSPs** — missing language servers are downloaded on first use to `~/.cache/codemap/`; silent background upgrades on subsequent runs
- **MCP server** — 6 tools, 4 prompts, 2 resource endpoints over stdio
- **CLI** — 8 subcommands with `--json` output and tabwriter-formatted tables

---

## Installation

```bash
git clone https://github.com/yourusername/codemap.git
cd codemap
go build -o codemap .
```

> **Go 1.25.6+** required. CGo must be available (needed by the Tree-sitter grammars and the templ parser).

Place the resulting `codemap` binary somewhere on your `$PATH`.

---

## Quick Start

### As an MCP Server

```bash
# Run in your project directory (indexes on startup, watches for changes)
cd /path/to/project
codemap serve

# Or specify the project root explicitly
codemap --project-dir /path/to/project serve
```

Add to your MCP client configuration (e.g. Claude Desktop):

```json
{
  "mcpServers": {
    "codemap": {
      "command": "/absolute/path/to/codemap",
      "args": ["--project-dir", "/absolute/path/to/project"]
    }
  }
}
```

`serve` is also the **default command** — running `codemap` with no subcommand starts the MCP server.

### As a CLI

```bash
# Build or rebuild the index
codemap index

# Show index statistics
codemap status

# List all symbols in a file
codemap symbols internal/graph/store.go

# Find all locations of a symbol
codemap symbol Open --source

# Show transitive dependents of a symbol
codemap impact NodeID --json

# Show LSP diagnostics
codemap diagnostics --severity 1
```

---

## CLI Reference

All subcommands accept two persistent global flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--project-dir DIR` | auto-detected git root | Project root used for git-ignore filtering and LSP workspace |
| `--db-dir DIR` | *(see below)* | Override the database directory |

**Database path resolution:**

| `--db-dir` | DB file location |
|---|---|
| Not set | `<cwd>/.codemap` |
| Set to `<d>` | `<d>/codemap.sqlite` |

### Subcommands

```
codemap serve
```
Start the MCP server over stdio. This is the default when no subcommand is given.

---

```
codemap watch [--daemon]
```
Spawn a background file-watcher daemon. The daemon re-indexes changed files incrementally and stops itself after **5 minutes of inactivity**. `--daemon` is internal (used by the spawned process).

---

```
codemap index
```
Perform a full (re)build of the symbol index. Blocks until complete and prints `nodes=N  edges=M`.

---

```
codemap status
```
Print node and edge counts from the existing index. Returns an error if no index has been built yet.

---

```
codemap symbols <file> [--json]
```
List every symbol defined in `<file>`. Accepts relative or absolute paths.

```
KIND      NAME          LINES   (internal/graph/store.go)
function  Open          88-115
function  OpenReadOnly  118-133
function  BulkUpsert…   136-160
```

---

```
codemap symbol <name> [--source] [--json]
```
Find all locations of `<name>` across the project. `--source` includes the source code snippet.

```
KIND      FILE                        LINES   NAME
function  internal/graph/store.go     88-115  Open
```

---

```
codemap impact <name> [--json]
```
Show every symbol that **transitively depends on** `<name>` (reverse-edge traversal via recursive CTE).

---

```
codemap diagnostics [--file PATH] [--severity N] [--json]
```
List LSP diagnostics captured during the last index run.

| `--severity` | Level |
|---|---|
| `1` | error |
| `2` | warning |
| `3` | info |
| `4` | hint |
| `0` (default) | all |

```
SEVERITY  FILE                  LINE  COL  SOURCE  MESSAGE
error     internal/graph/st…    42    5    gopls   undefined: foo
```

---

## MCP Tools

| Tool | Description |
|------|-------------|
| `index` | Trigger a full re-index; waits for completion and reports node/edge/diagnostic counts |
| `index_status` | Return current index status (`idle`, `in_progress`, `ready`, `failed`) with counts |
| `get_symbols_in_file` | List all symbols in a file (accepts relative or absolute paths) |
| `get_symbol` | Find all locations of a named symbol; optional `with_source` flag |
| `find_impact` | Transitive reverse-dependency analysis — all symbols that depend on the named symbol |
| `get_diagnostics` | Return LSP diagnostics; optional `file_path` and `severity` filters |

### MCP Resources

| URI | Description |
|-----|-------------|
| `codemap://usage-guidelines` | System prompt and operating guidelines (Markdown) |
| `codemap://schemas/{tool_name}` | JSON schema for a tool's arguments |

### MCP Prompts

| Prompt | Arguments | Description |
|--------|-----------|-------------|
| `analyze-impact` | `symbol_name` | Guide the agent to assess the blast radius of a change |
| `explore-file` | `file_path` | Guide the agent to understand a file's structure |
| `locate-and-explain` | `symbol_name` | Find a symbol and explain its context |
| `re-index-workspace` | — | Instruct the agent to refresh the graph |

---

## Language Support

| Language | Extensions | Tree-sitter | LSP server | Symbols extracted |
|----------|-----------|-------------|-----------|-------------------|
| Go | `.go` | ✅ | gopls | functions, methods, types |
| Python | `.py` | ✅ | pylsp | functions, classes |
| JavaScript | `.js` `.jsx` | ✅ | typescript-language-server | functions, methods, classes, arrow-function variables |
| TypeScript | `.ts` `.tsx` | ✅ | typescript-language-server | functions, methods, classes, interfaces, type aliases |
| Lua | `.lua` | ✅ | lua-language-server | functions, methods |
| Zig | `.zig` | ✅ | zls | functions |
| Templ | `.templ` | ✅ | templ lsp | components, CSS declarations, script declarations, functions |

Generated files (`_templ.go`, `.sql.go`, `_string.go`) are automatically skipped.

---

## LSP Auto-Install

CodeMap resolves language server binaries in this order:

1. **System `$PATH`** — if `gopls` (or any other LSP binary) is already installed, it is used directly
2. **`~/.cache/codemap/`** — previously auto-downloaded binaries are cached here
3. **Auto-download** — if not found anywhere, the binary is downloaded and saved to `~/.cache/codemap/`

A **silent background upgrade check** runs once per process. If a newer version of a cached binary is available it is reinstalled without blocking the main indexing flow.

To use your own LSP installations, simply ensure they are on `$PATH` — CodeMap will find and use them.

---

## Architecture

```
codemap
├── MCP server (serve command / default)
│   └── JSON-RPC over stdio
│       ├── 6 tools
│       ├── 4 prompts
│       └── 2 resource endpoints
│
├── CLI (index / status / symbols / symbol / impact / diagnostics)
│   └── reads the same SQLite database (read-only)
│
└── Engine (shared by all modes)
    ├── Scanner — Tree-sitter AST parsing → graph.Node values
    ├── LSP Service — references + implementations → graph.Edge values
    │   └── per-language Client with adaptive warmup + notification handling
    ├── Graph Store — SQLite (WAL, foreign keys, recursive CTEs)
    │   ├── nodes (symbols)
    │   ├── edges (references, implements)
    │   └── diagnostics + diagnostic_edges
    └── Watcher — fsnotify + 500 ms debounce + 5 min idle timeout
```

### Node ID

Node IDs are deterministic: `SHA256(filePath + ":" + name + ":" + kind)[:16]` (32-char hex). This makes incremental updates collision-safe and idempotent.

### Data Model

```json
// Node
{
  "id": "a3f1…c9d2",
  "name": "Open",
  "kind": "function",
  "file_path": "/abs/path/to/store.go",
  "line_start": 88,
  "line_end": 115,
  "col_start": 1,
  "col_end": 1,
  "name_line": 88,
  "name_col": 6,
  "symbol_uri": "file:///abs/path/to/store.go"
}

// Edge
{
  "source_id": "a3f1…c9d2",
  "target_id": "b7e4…1a88",
  "relation": "references"  // or "implements"
}

// Diagnostic
{
  "id": "d9c3…7f41",
  "file_path": "/abs/path/to/store.go",
  "line": 42,
  "col": 5,
  "severity": 1,
  "code": "undeclared",
  "source": "gopls",
  "message": "undefined: foo"
}
```

---

## Project Structure

```
codemap/
├── main.go              # Cobra root command + serve / watch / index
├── commands.go          # CLI subcommands: status, symbols, symbol, impact, diagnostics
├── SYSTEM_PROMPT.md     # Embedded MCP usage guidelines
├── go.mod / go.sum
├── mise.toml            # Task runner (build, test, vet)
├── internal/
│   ├── daemon/          # PID file management, detached process spawning
│   ├── db/              # EnsureDir helper
│   ├── graph/
│   │   ├── types.go     # Node, Edge, Diagnostic, DiagnosticEdge types + constants
│   │   └── store.go     # Open/OpenReadOnly, all CRUD, recursive CTE queries, diagnostics
│   ├── lsp/
│   │   ├── lsp.go       # Client (send loop, notification handling, DrainDiagnostics), Service
│   │   ├── transport.go # LSP stdio framing (Content-Length headers)
│   │   └── types.go     # JSON-RPC 2.0 + LSP protocol types
│   ├── pkgmgr/
│   │   ├── manager.go   # Manager.ResolveBinary, Install
│   │   ├── metadata.go  # Per-binary install/version/latest recipes, archive extraction
│   │   └── upgrade.go   # Background silent upgrade checks
│   ├── scanner/
│   │   ├── scanner.go   # New(root), Scan, ScanFile — Tree-sitter walk
│   │   └── queries.go   # Tree-sitter S-expression queries per language
│   ├── server/
│   │   ├── server.go    # New/NewWatch, ForceIndex, WaitForIndex, runIndex, saveDiagnostics
│   │   ├── tools.go     # MCP tool registration (6 tools)
│   │   ├── resources.go # MCP resource registration
│   │   ├── prompts.go   # MCP prompt registration
│   │   └── query.go     # NodeWithSource, NodeToWithSource, AbsFilePath (shared by MCP + CLI)
│   ├── treesittertempl/ # CGo wrapper for the Templ tree-sitter grammar (parser.c + scanner.c)
│   └── watcher/
│       └── watcher.go   # New, Run(ctx, cancel), idle timeout, debounce, reindexFile
├── util/
│   ├── git.go           # FindGitRoot(dir)
│   ├── hash.go          # NodeID, DiagnosticID
│   └── uri.go           # PathToURI, URIToPath
└── tests/
    ├── integration_test.go
    └── lsp_integration_test.go
```

---

## Building and Testing

```bash
# Build
go build -o codemap .
# or
mise run build

# Test
go test ./...
# or
mise run test

# Vet
go vet ./...
# or
mise run vet
```

---

## Troubleshooting

**`project has not been indexed yet — run: codemap index`**
Run `codemap index` once before using `status`, `symbols`, `symbol`, `impact`, or `diagnostics`.

**LSP enrichment produces no edges**
The language server needs a few seconds to index the workspace. The first run includes a 5-second warmup wait per language. Subsequent runs use the cached binary with no delay.

**`inotify: too many open files` (Linux)**
```bash
echo fs.inotify.max_user_watches=524288 | sudo tee -a /etc/sysctl.conf
sudo sysctl -p
```

**High memory / slow index on large codebases**
Tree-sitter scanning is fast; the bottleneck is LSP enrichment. Run `codemap index` once, then use the watcher daemon (`codemap watch`) for incremental updates.

---

## License

GNU General Public License v3.0 — see [LICENSE](LICENSE).
