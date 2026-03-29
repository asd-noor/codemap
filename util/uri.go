package util

import (
	"net/url"
	"path/filepath"
	"strings"
)

// PathToURI converts an absolute file path to a file:// URI.
func PathToURI(path string) string {
	abs, _ := filepath.Abs(path)
	u := &url.URL{Scheme: "file", Path: abs}
	return u.String()
}

// URIToPath converts a file:// URI to an absolute file path.
func URIToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return strings.TrimPrefix(uri, "file://")
	}
	return u.Path
}
