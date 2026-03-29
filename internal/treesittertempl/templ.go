// Package treesittertempl provides the tree-sitter Language for Templ files.
//
// The upstream Go binding (github.com/vrischmann/tree-sitter-templ/bindings/go)
// does not include the external scanner, so we provide our own wrapper that
// links both parser.c and scanner.c.
package treesittertempl

// #cgo CFLAGS: -std=c11 -fPIC
// #include "src/parser.c"
// #include "src/scanner.c"
import "C"

import "unsafe"

// Language returns the tree-sitter Language pointer for Templ.
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_templ())
}
