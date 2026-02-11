package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// ZipEntry holds a cached zip archive with pre-built file lookup.
type ZipEntry struct {
	reader *zip.Reader
	files  map[string]*zip.File // stripped path -> zip.File
}

// ListFiles returns file paths matching an optional prefix filter.
func (e *ZipEntry) ListFiles(prefix string) []string {
	var result []string

	for name := range e.files {
		if prefix == "" || strings.HasPrefix(name, prefix) {
			result = append(result, name)
		}
	}

	return result
}

// ReadFile reads the content of a file from the zip archive.
// Returns an error for binary files.
func (e *ZipEntry) ReadFile(path string) (string, error) {
	f, ok := e.files[path]
	if !ok {
		return "", fmt.Errorf("file not found in archive: %s", path)
	}

	rc, err := f.Open()
	if err != nil {
		return "", fmt.Errorf("open file in zip: %w", err)
	}
	defer rc.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		return "", fmt.Errorf("read file from zip: %w", err)
	}

	data := buf.Bytes()
	if !utf8.Valid(data) {
		return "", fmt.Errorf("file appears to be binary: %s", path)
	}

	return string(data), nil
}

// ZipCache is an in-memory cache of downloaded module zip archives.
type ZipCache struct {
	mu      sync.Mutex
	entries map[string]*ZipEntry
}

func NewZipCache() *ZipCache {
	return &ZipCache{
		entries: make(map[string]*ZipEntry),
	}
}

// Get returns a cached ZipEntry, or nil if not cached.
func (c *ZipCache) Get(module, version string) *ZipEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.entries[module+"@"+version]
}

// Put parses and caches a zip archive. The prefix "module@version/" is stripped
// from file paths in the lookup map.
func (c *ZipCache) Put(module, version string, data []byte) (*ZipEntry, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("parse zip: %w", err)
	}

	prefix := module + "@" + version + "/"
	files := make(map[string]*zip.File, len(r.File))

	for _, f := range r.File {
		name := f.Name

		name = strings.TrimPrefix(name, prefix)
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}

		files[name] = f
	}

	entry := &ZipEntry{
		reader: r,
		files:  files,
	}

	c.mu.Lock()
	c.entries[module+"@"+version] = entry
	c.mu.Unlock()

	return entry, nil
}
