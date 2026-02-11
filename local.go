package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LocalReader checks for local copies of modules in a base directory.
type LocalReader struct {
	baseDir string
}

func NewLocalReader(baseDir string) *LocalReader {
	return &LocalReader{baseDir: baseDir}
}

// Suggest checks if a local directory exists for the given module path
// and returns a suggestion string pointing to it. Returns empty string
// if no local directory is found.
func (r *LocalReader) Suggest(module string) string {
	name := lastPathSegment(module)
	dir := filepath.Join(r.baseDir, name)

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ""
	}

	return fmt.Sprintf(
		"Module %q is not available on the Go module proxy, "+
			"but a local directory exists at %s that may contain its source. "+
			"You can use your file tools to browse it.",
		module, dir,
	)
}

// lastPathSegment returns the last component of a module path.
// E.g. "golang.org/x/tools" -> "tools".
func lastPathSegment(module string) string {
	if i := strings.LastIndex(module, "/"); i >= 0 {
		return module[i+1:]
	}

	return module
}
