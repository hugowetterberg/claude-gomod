package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestModDir(t *testing.T) {
	mc := NewModCache("/tmp/gomod")
	got := mc.ModDir("github.com/Foo/Bar", "v1.0.0")
	want := filepath.Join("/tmp/gomod", "github.com/!foo/!bar@v1.0.0")

	if got != want {
		t.Errorf("ModDir = %q, want %q", got, want)
	}
}

func TestModDir_NoUppercase(t *testing.T) {
	mc := NewModCache("/cache")
	got := mc.ModDir("golang.org/x/tools", "v0.5.0")
	want := filepath.Join("/cache", "golang.org/x/tools@v0.5.0")

	if got != want {
		t.Errorf("ModDir = %q, want %q", got, want)
	}
}

func TestHasModule_Exists(t *testing.T) {
	dir := t.TempDir()
	mc := NewModCache(dir)

	modDir := filepath.Join(dir, "example.com/mod@v1.0.0")

	mustf(t, os.MkdirAll(modDir, 0o755), "create mod dir")

	if !mc.HasModule("example.com/mod", "v1.0.0") {
		t.Error("expected HasModule to return true")
	}
}

func TestHasModule_NotExists(t *testing.T) {
	dir := t.TempDir()
	mc := NewModCache(dir)

	if mc.HasModule("example.com/mod", "v1.0.0") {
		t.Error("expected HasModule to return false")
	}
}

func TestHasModule_EmptyDir(t *testing.T) {
	mc := NewModCache("")

	if mc.HasModule("example.com/mod", "v1.0.0") {
		t.Error("expected HasModule to return false with empty dir")
	}
}

func TestListFiles(t *testing.T) {
	dir := t.TempDir()
	mc := NewModCache(dir)

	modDir := filepath.Join(dir, "example.com/mod@v1.0.0")

	for _, f := range []string{"go.mod", "main.go", "cmd/run.go", "lib/util.go"} {
		full := filepath.Join(modDir, filepath.FromSlash(f))

		mustf(t, os.MkdirAll(filepath.Dir(full), 0o755), "create parent dir for %s", f)
		mustf(t, os.WriteFile(full, []byte("content"), 0o600), "write %s", f)
	}

	files, err := mc.ListFiles("example.com/mod", "v1.0.0", "")

	mustf(t, err, "list files")
	sort.Strings(files)

	want := []string{"cmd/run.go", "go.mod", "lib/util.go", "main.go"}
	if len(files) != len(want) {
		t.Fatalf("got %d files %v, want %d %v", len(files), files, len(want), want)
	}

	for i := range want {
		if files[i] != want[i] {
			t.Errorf("files[%d] = %q, want %q", i, files[i], want[i])
		}
	}
}

func TestListFiles_WithPrefix(t *testing.T) {
	dir := t.TempDir()
	mc := NewModCache(dir)

	modDir := filepath.Join(dir, "example.com/mod@v1.0.0")

	for _, f := range []string{"go.mod", "cmd/run.go", "cmd/help.go", "lib/util.go"} {
		full := filepath.Join(modDir, filepath.FromSlash(f))

		mustf(t, os.MkdirAll(filepath.Dir(full), 0o755), "create parent dir for %s", f)
		mustf(t, os.WriteFile(full, []byte("content"), 0o600), "write %s", f)
	}

	files, err := mc.ListFiles("example.com/mod", "v1.0.0", "cmd/")

	mustf(t, err, "list files with prefix")
	sort.Strings(files)

	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %v", len(files), files)
	}

	if files[0] != "cmd/help.go" || files[1] != "cmd/run.go" {
		t.Errorf("unexpected files: %v", files)
	}
}

func TestReadFile_Text(t *testing.T) {
	dir := t.TempDir()
	mc := NewModCache(dir)

	modDir := filepath.Join(dir, "example.com/mod@v1.0.0")

	mustf(t, os.MkdirAll(modDir, 0o755), "create mod dir")
	mustf(t, os.WriteFile(filepath.Join(modDir, "main.go"), []byte("package main\n"), 0o600), "write main.go")

	content, err := mc.ReadFile("example.com/mod", "v1.0.0", "main.go")

	mustf(t, err, "read main.go")

	if content != "package main\n" {
		t.Errorf("got %q, want %q", content, "package main\n")
	}
}

func TestReadFile_Binary(t *testing.T) {
	dir := t.TempDir()
	mc := NewModCache(dir)

	modDir := filepath.Join(dir, "example.com/mod@v1.0.0")

	mustf(t, os.MkdirAll(modDir, 0o755), "create mod dir")

	binaryData := []byte{0x89, 0x50, 0x4e, 0x47, 0xff, 0xfe}

	mustf(t, os.WriteFile(filepath.Join(modDir, "image.png"), binaryData, 0o600), "write image.png")

	_, err := mc.ReadFile("example.com/mod", "v1.0.0", "image.png")
	if err == nil {
		t.Fatal("expected error for binary file")
	}

	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("error should mention 'binary': %v", err)
	}
}

func TestReadFile_NotFound(t *testing.T) {
	dir := t.TempDir()
	mc := NewModCache(dir)

	modDir := filepath.Join(dir, "example.com/mod@v1.0.0")

	mustf(t, os.MkdirAll(modDir, 0o755), "create mod dir")

	_, err := mc.ReadFile("example.com/mod", "v1.0.0", "missing.go")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
