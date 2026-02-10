package scanner

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	tslua "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tspy "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tsts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	ignore "github.com/sabhiram/go-gitignore"

	"codemap/internal/graph"
	"codemap/util"
)

type Scanner struct {
	languages map[string]*sitter.Language
	queries   map[string]*sitter.Query
}

func New() (*Scanner, error) {
	s := &Scanner{
		languages: make(map[string]*sitter.Language),
		queries:   make(map[string]*sitter.Query),
	}

	// Register languages
	s.languages["go"] = sitter.NewLanguage(tsgo.Language())
	s.languages["py"] = sitter.NewLanguage(tspy.Language())
	s.languages["js"] = sitter.NewLanguage(tsjs.Language())
	s.languages["ts"] = sitter.NewLanguage(tsts.LanguageTypescript())
	s.languages["lua"] = sitter.NewLanguage(tslua.Language())
	// Zig disabled for now

	// Compile queries
	for ext, lang := range s.languages {
		qStr, ok := Queries[getLangKey(ext)]
		if !ok {
			continue
		}
		q, err := sitter.NewQuery(lang, qStr)
		if err != nil {
			return nil, fmt.Errorf("failed to compile query for %s: %w", ext, err)
		}
		s.queries[ext] = q
	}

	return s, nil
}

func getLangKey(ext string) string {
	switch ext {
	case "go":
		return "go"
	case "py":
		return "python"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "lua":
		return "lua"
	default:
		return ""
	}
}

// ScanFile scans a single file and returns its nodes.
func (s *Scanner) ScanFile(ctx context.Context, path string) ([]*graph.Node, error) {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	lang, ok := s.languages[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}

	query, ok := s.queries[ext]
	if !ok {
		return nil, fmt.Errorf("no query for extension: %s", ext)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse file")
	}
	defer tree.Close()

	qc := sitter.NewQueryCursor()
	defer qc.Close()

	var nodes []*graph.Node
	iter := qc.Captures(query, tree.RootNode(), content)
	for {
		m, idx := iter.Next()
		if idx == math.MaxUint {
			break
		}

		for _, c := range m.Captures {
			name := string(content[c.Node.StartByte():c.Node.EndByte()])
			kind := query.CaptureNames()[c.Index]

			nodes = append(nodes, &graph.Node{
				ID:        util.GenerateNodeID(path, name),
				Name:      name,
				Kind:      kind,
				FilePath:  path,
				LineStart: int(c.Node.StartPosition().Row) + 1,
				LineEnd:   int(c.Node.EndPosition().Row) + 1,
				ColStart:  int(c.Node.StartPosition().Column) + 1,
				ColEnd:    int(c.Node.EndPosition().Column) + 1,
				SymbolURI: util.PathToURI(path),
			})
		}
	}

	return nodes, nil
}

func (s *Scanner) Scan(ctx context.Context, root string) ([]*graph.Node, error) {
	var nodes []*graph.Node

	// Load gitignore
	ign, _ := ignore.CompileIgnoreFile(filepath.Join(root, ".gitignore"))

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden files and common ignore dirs
		if strings.HasPrefix(d.Name(), ".") && d.Name() != "." && d.Name() != ".gitignore" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == "zig-out") {
			return filepath.SkipDir
		}

		// Check gitignore
		relPath, _ := filepath.Rel(root, path)
		if ign != nil && ign.MatchesPath(relPath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		// Check extension
		ext := strings.TrimPrefix(filepath.Ext(path), ".")
		lang, ok := s.languages[ext]
		if !ok {
			return nil
		}
		query, ok := s.queries[ext]
		if !ok {
			return nil
		}

		// Parse
		content, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip unreadable files
		}

		parser := sitter.NewParser()
		parser.SetLanguage(lang)
		tree := parser.Parse(content, nil)
		if tree == nil {
			return nil
		}
		defer tree.Close()

		rootNode := tree.RootNode()
		qc := sitter.NewQueryCursor()
		defer qc.Close()

		matches := qc.Matches(query, rootNode, content)
		captureNames := query.CaptureNames()

		for {
			match := matches.Next()
			if match == nil {
				break
			}

			var nameNode sitter.Node
			var foundName bool
			var kind string = "symbol"

			for _, capture := range match.Captures {
				cName := captureNames[capture.Index]

				if cName == "name" {
					nameNode = capture.Node
					foundName = true
				}
			}

			if foundName {
				// Extract content
				name := nameNode.Utf8Text(content)

				// simple kind inference
				parentNode := nameNode.Parent()
				if parentNode != nil {
					kind = parentNode.Kind()
				}

				node := &graph.Node{
					ID:        util.GenerateNodeID(relPath, name),
					Name:      name,
					Kind:      kind,
					FilePath:  path, // Store absolute path for LSP compatibility
					LineStart: int(nameNode.StartPosition().Row) + 1,
					LineEnd:   int(nameNode.EndPosition().Row) + 1,
					ColStart:  int(nameNode.StartPosition().Column) + 1,
					ColEnd:    int(nameNode.EndPosition().Column) + 1,
				}
				nodes = append(nodes, node)
			}
		}

		return nil
	})

	return nodes, err
}
