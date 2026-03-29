package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"codemap/internal/graph"
	"codemap/internal/server"
)

// ── status ────────────────────────────────────────────────────────────────────

func newStatusCmd(projectDir, dbDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current codemap index status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := resolveStorePaths(*projectDir, *dbDir)
			return runStatus(dbPath)
		},
	}
}

func runStatus(dbPath string) error {
	ctx := context.Background()
	store, err := server.OpenStoreReadOnly(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	nodes, err := store.NodeCount(ctx)
	if err != nil {
		return err
	}
	edges, err := store.EdgeCount(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("nodes=%d  edges=%d\n", nodes, edges)
	return nil
}

// ── symbols ───────────────────────────────────────────────────────────────────

func newSymbolsCmd(projectDir, dbDir *string) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "symbols <file>",
		Short: "List all symbols in <file>",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := resolveStorePaths(*projectDir, *dbDir)
			return runSymbols(*projectDir, dbPath, args[0], jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}

func runSymbols(projectDir, dbPath, filePath string, jsonOut bool) error {
	absFile, err := server.AbsFilePath(filePath)
	if err != nil {
		return err
	}

	root := rootDir(projectDir)
	ctx := context.Background()
	store, err := server.OpenStoreReadOnly(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	nodes, err := store.GetSymbolsInFile(ctx, absFile)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(nodes)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "KIND\tNAME\tLINES\t(%s)\n", relPath(root, absFile))
	for _, n := range nodes {
		fmt.Fprintf(w, "%s\t%s\t%d-%d\n", n.Kind, n.Name, n.LineStart, n.LineEnd)
	}
	return w.Flush()
}

// ── symbol ────────────────────────────────────────────────────────────────────

func newSymbolCmd(projectDir, dbDir *string) *cobra.Command {
	var withSource, jsonOut bool
	cmd := &cobra.Command{
		Use:   "symbol <name>",
		Short: "Find all locations of <name> in the project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := resolveStorePaths(*projectDir, *dbDir)
			return runSymbol(*projectDir, dbPath, args[0], withSource, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&withSource, "source", false, "Include source code snippet")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}

func runSymbol(projectDir, dbPath, name string, withSource, jsonOut bool) error {
	root := rootDir(projectDir)
	ctx := context.Background()
	store, err := server.OpenStoreReadOnly(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	nodes, err := store.GetSymbolLocation(ctx, name)
	if err != nil {
		return err
	}

	results := make([]server.NodeWithSource, len(nodes))
	for i, n := range nodes {
		results[i] = server.NodeToWithSource(n, withSource)
	}

	if jsonOut {
		return printJSON(results)
	}

	if !withSource {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KIND\tFILE\tLINES\tNAME")
		for _, n := range results {
			fmt.Fprintf(w, "%s\t%s\t%d-%d\t%s\n", n.Kind, relPath(root, n.FilePath), n.LineStart, n.LineEnd, n.Name)
		}
		return w.Flush()
	}

	// With --source: print each result with a fenced code block.
	for i, n := range results {
		if i > 0 {
			fmt.Println()
		}
		rel := relPath(root, n.FilePath)
		fmt.Printf("%s  %s  %s  lines %d-%d\n", n.Kind, n.Name, rel, n.LineStart, n.LineEnd)
		if n.Source != "" {
			lang := langFromExt(n.FilePath)
			fmt.Printf("```%s\n%s```\n", lang, n.Source)
		}
	}
	return nil
}

// ── impact ────────────────────────────────────────────────────────────────────

func newImpactCmd(projectDir, dbDir *string) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "impact <name>",
		Short: "Show all symbols that transitively depend on <name>",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := resolveStorePaths(*projectDir, *dbDir)
			return runImpact(*projectDir, dbPath, args[0], jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}

func runImpact(projectDir, dbPath, name string, jsonOut bool) error {
	root := rootDir(projectDir)
	ctx := context.Background()
	store, err := server.OpenStoreReadOnly(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	nodes, err := store.FindImpact(ctx, name)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(nodes)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tNAME\tFILE\tLINES")
	for _, n := range nodes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d-%d\n", n.Kind, n.Name, relPath(root, n.FilePath), n.LineStart, n.LineEnd)
	}
	return w.Flush()
}

// ── diagnostics ───────────────────────────────────────────────────────────────

func newDiagnosticsCmd(projectDir, dbDir *string) *cobra.Command {
	var (
		filterFile string
		jsonOut    bool
		severity   int
	)
	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "List LSP diagnostics stored in the codemap index",
		Long: `List categorised LSP diagnostics collected during the last index run.

Diagnostics are grouped by file and ordered by severity (error → warning →
info → hint) then by line number. Use --severity to restrict output to a
specific level (1=error, 2=warning, 3=info, 4=hint).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := resolveStorePaths(*projectDir, *dbDir)
			return runDiagnostics(*projectDir, dbPath, filterFile, severity, jsonOut)
		},
	}
	cmd.Flags().StringVar(&filterFile, "file", "", "Restrict output to a single file (absolute or relative path)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().IntVar(&severity, "severity", 0, "Filter by severity level (1=error 2=warning 3=info 4=hint); 0 means all")
	return cmd
}

func runDiagnostics(projectDir, dbPath, filterFile string, severity int, jsonOut bool) error {
	root := rootDir(projectDir)
	ctx := context.Background()

	store, err := server.OpenStoreReadOnly(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	var diags []graph.Diagnostic

	if filterFile != "" {
		absFile, err := server.AbsFilePath(filterFile)
		if err != nil {
			return err
		}
		diags, err = store.GetDiagnosticsForFile(ctx, absFile)
		if err != nil {
			return err
		}
	} else {
		diags, err = store.GetAllDiagnostics(ctx)
		if err != nil {
			return err
		}
	}

	// Apply optional severity filter.
	if severity > 0 {
		filtered := diags[:0]
		for _, d := range diags {
			if d.Severity == severity {
				filtered = append(filtered, d)
			}
		}
		diags = filtered
	}

	if jsonOut {
		return printJSON(diags)
	}

	if len(diags) == 0 {
		fmt.Println("no diagnostics found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEVERITY\tFILE\tLINE\tCOL\tSOURCE\tMESSAGE")
	for _, d := range diags {
		rel := relPath(root, d.FilePath)
		src := d.Source
		if src == "" {
			src = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\t%s\n",
			severityLabel(d.Severity), rel, d.Line, d.Col, src, d.Message)
	}
	return w.Flush()
}

// ── shared helpers ────────────────────────────────────────────────────────────

// relPath returns a path relative to root, falling back to the original.
func relPath(root, absPath string) string {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return absPath
	}
	return rel
}

// langFromExt maps a file extension to a Markdown fenced-code-block language tag.
func langFromExt(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".lua":
		return "lua"
	case ".zig":
		return "zig"
	case ".templ":
		return "templ"
	default:
		return ""
	}
}

// severityLabel converts an LSP severity integer to a short human-readable tag.
func severityLabel(s int) string {
	switch s {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "unknown"
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
