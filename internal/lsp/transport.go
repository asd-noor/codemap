// Package lsp implements the LSP stdio transport: framing messages with the
// Content-Length header as required by the Language Server Protocol.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// WriteMessage writes a JSON-RPC message to w using LSP header framing.
func WriteMessage(w io.Writer, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("lsp: marshal message: %w", err)
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(w, header); err != nil {
		return fmt.Errorf("lsp: write header: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("lsp: write body: %w", err)
	}
	return nil
}

// ReadMessage reads one LSP-framed message from r and unmarshals it into v.
func ReadMessage(r *bufio.Reader, v any) error {
	// Read headers until the blank line.
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return fmt.Errorf("lsp: read header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if strings.HasPrefix(line, "Content-Length:") {
			s := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			n, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("lsp: parse Content-Length: %w", err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return fmt.Errorf("lsp: missing Content-Length header")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return fmt.Errorf("lsp: read body: %w", err)
	}

	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("lsp: unmarshal body: %w", err)
	}
	return nil
}
