package main

import (
	"archive/zip"
	"bytes"
	"sort"
	"strings"
	"testing"
)

// createTestZip builds a zip archive in memory. The prefix is prepended to
// each file name (e.g. "mod@v1.0.0/").
func createTestZip(t *testing.T, prefix string, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := zip.NewWriter(&buf)

	for name, content := range files {
		f, err := w.Create(prefix + name)

		mustf(t, err, "create %s in zip", name)

		_, err = f.Write([]byte(content))

		mustf(t, err, "write %s to zip", name)
	}

	mustf(t, w.Close(), "close zip writer")

	return buf.Bytes()
}

func TestZipCache_PutAndGet(t *testing.T) {
	cache := NewZipCache()

	if entry := cache.Get("mod", "v1.0.0"); entry != nil {
		t.Fatal("expected nil for uncached module")
	}

	data := createTestZip(t, "mod@v1.0.0/", map[string]string{
		"go.mod":   "module mod\n",
		"main.go":  "package main\n",
		"lib/a.go": "package lib\n",
	})

	entry, err := cache.Put("mod", "v1.0.0", data)

	mustf(t, err, "put zip in cache")

	if entry == nil {
		t.Fatal("expected non-nil entry")
	}

	// Should be retrievable.
	got := cache.Get("mod", "v1.0.0")
	if got != entry {
		t.Fatal("Get returned different entry than Put")
	}

	// Different version should not exist.
	if cache.Get("mod", "v2.0.0") != nil {
		t.Fatal("expected nil for different version")
	}
}

func TestZipEntry_ListFiles(t *testing.T) {
	data := createTestZip(t, "mod@v1.0.0/", map[string]string{
		"go.mod":       "module mod\n",
		"main.go":      "package main\n",
		"cmd/run.go":   "package cmd\n",
		"cmd/build.go": "package cmd\n",
		"lib/util.go":  "package lib\n",
	})

	cache := NewZipCache()

	entry, err := cache.Put("mod", "v1.0.0", data)

	mustf(t, err, "put zip in cache")

	t.Run("no prefix", func(t *testing.T) {
		files := entry.ListFiles("")
		if len(files) != 5 {
			t.Fatalf("got %d files, want 5", len(files))
		}
	})

	t.Run("cmd/ prefix", func(t *testing.T) {
		files := entry.ListFiles("cmd/")
		sort.Strings(files)

		if len(files) != 2 {
			t.Fatalf("got %d files, want 2: %v", len(files), files)
		}

		if files[0] != "cmd/build.go" || files[1] != "cmd/run.go" {
			t.Errorf("unexpected files: %v", files)
		}
	})

	t.Run("nonexistent prefix", func(t *testing.T) {
		files := entry.ListFiles("nope/")
		if len(files) != 0 {
			t.Fatalf("got %d files, want 0", len(files))
		}
	})
}

func TestZipEntry_ReadFile(t *testing.T) {
	data := createTestZip(t, "mod@v1.0.0/", map[string]string{
		"hello.go": "package main\n\nfunc main() {}\n",
	})

	cache := NewZipCache()
	entry, _ := cache.Put("mod", "v1.0.0", data)

	content, err := entry.ReadFile("hello.go")

	mustf(t, err, "read hello.go")

	if content != "package main\n\nfunc main() {}\n" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestZipEntry_ReadFile_NotFound(t *testing.T) {
	data := createTestZip(t, "mod@v1.0.0/", map[string]string{
		"hello.go": "package main\n",
	})

	cache := NewZipCache()
	entry, _ := cache.Put("mod", "v1.0.0", data)

	_, err := entry.ReadFile("missing.go")
	if err == nil {
		t.Fatal("expected error for missing file")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found': %v", err)
	}
}

func TestZipEntry_ReadFile_Binary(t *testing.T) {
	// Create a zip with a binary file (invalid UTF-8).
	var buf bytes.Buffer

	w := zip.NewWriter(&buf)

	f, _ := w.Create("mod@v1.0.0/image.png")

	_, err := f.Write([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0xff, 0xfe})

	mustf(t, err, "write binary data to zip")

	w.Close()

	cache := NewZipCache()
	entry, _ := cache.Put("mod", "v1.0.0", buf.Bytes())

	_, err = entry.ReadFile("image.png")
	if err == nil {
		t.Fatal("expected error for binary file")
	}

	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("error should mention 'binary': %v", err)
	}
}

func TestZipCache_InvalidZip(t *testing.T) {
	cache := NewZipCache()

	_, err := cache.Put("mod", "v1.0.0", []byte("not a zip file"))
	if err == nil {
		t.Fatal("expected error for invalid zip data")
	}
}

func TestZipEntry_PathStripping(t *testing.T) {
	// Verify that the module@version/ prefix is properly stripped.
	data := createTestZip(t, "github.com/foo/bar@v2.0.0/", map[string]string{
		"README.md": "hello",
		"pkg/x.go":  "package pkg\n",
	})

	cache := NewZipCache()
	entry, _ := cache.Put("github.com/foo/bar", "v2.0.0", data)

	// Files should be accessible without the prefix.
	content, err := entry.ReadFile("README.md")

	mustf(t, err, "read README.md")

	if content != "hello" {
		t.Errorf("unexpected content: %q", content)
	}

	files := entry.ListFiles("")
	sort.Strings(files)

	if len(files) != 2 || files[0] != "README.md" || files[1] != "pkg/x.go" {
		t.Errorf("unexpected files: %v", files)
	}
}
