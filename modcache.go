package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ModCache reads module files directly from the local Go module cache
// ($GOMODCACHE), avoiding network requests when modules are already downloaded.
type ModCache struct {
	dir string
}

// NewModCache creates a ModCache rooted at the given directory.
// If dir is empty, all lookups will report the module as absent.
func NewModCache(dir string) *ModCache {
	return &ModCache{dir: dir}
}

// ModDir returns the on-disk path for a module version in the cache.
func (m *ModCache) ModDir(module, version string) string {
	return filepath.Join(m.dir, encodePath(module)+"@"+version)
}

// HasModule reports whether the module version directory exists in the cache.
func (m *ModCache) HasModule(module, version string) bool {
	if m.dir == "" {
		return false
	}

	info, err := os.Stat(m.ModDir(module, version))

	return err == nil && info.IsDir()
}

// ListFiles walks the extracted module directory and returns file paths
// relative to the module root. Only regular files are included.
// If prefix is non-empty, only paths starting with prefix are returned.
func (m *ModCache) ListFiles(module, version, prefix string) ([]string, error) {
	root := m.ModDir(module, version)

	var files []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("compute relative path: %w", err)
		}

		// Normalize to forward slashes for consistency with zip-based paths.
		rel = filepath.ToSlash(rel)
		if prefix == "" || strings.HasPrefix(rel, prefix) {
			files = append(files, rel)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk module cache dir: %w", err)
	}

	return files, nil
}

// ReadFile reads a file from the extracted module directory.
// Returns an error if the file contains non-UTF-8 (binary) content.
func (m *ModCache) ReadFile(module, version, path string) (string, error) {
	full := filepath.Join(m.ModDir(module, version), filepath.FromSlash(path))

	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("read file from mod cache: %w", err)
	}

	if !utf8.Valid(data) {
		return "", fmt.Errorf("file appears to be binary: %s", path)
	}

	return string(data), nil
}
