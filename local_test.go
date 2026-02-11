package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastPathSegment(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"golang.org/x/tools", "tools"},
		{"github.com/foo/bar", "bar"},
		{"singlesegment", "singlesegment"},
		{"a/b/c/d/e", "e"},
	}

	for _, tt := range tests {
		got := lastPathSegment(tt.input)
		if got != tt.want {
			t.Errorf("lastPathSegment(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLocalReader_Suggest_Exists(t *testing.T) {
	dir := t.TempDir()

	mustf(t, os.Mkdir(filepath.Join(dir, "tools"), 0o755), "create tools dir")

	lr := NewLocalReader(dir)
	suggestion := lr.Suggest("golang.org/x/tools")

	if suggestion == "" {
		t.Fatal("expected non-empty suggestion")
	}

	if !strings.Contains(suggestion, filepath.Join(dir, "tools")) {
		t.Errorf("suggestion should contain the local path: %s", suggestion)
	}

	if !strings.Contains(suggestion, "golang.org/x/tools") {
		t.Errorf("suggestion should mention the module: %s", suggestion)
	}
}

func TestLocalReader_Suggest_NotExists(t *testing.T) {
	dir := t.TempDir()

	lr := NewLocalReader(dir)
	suggestion := lr.Suggest("golang.org/x/nonexistent")

	if suggestion != "" {
		t.Errorf("expected empty suggestion, got %q", suggestion)
	}
}

func TestLocalReader_Suggest_FileNotDir(t *testing.T) {
	dir := t.TempDir()

	mustf(t, os.WriteFile(filepath.Join(dir, "tools"), []byte("not a dir"), 0o600), "write tools file")

	lr := NewLocalReader(dir)
	suggestion := lr.Suggest("golang.org/x/tools")

	if suggestion != "" {
		t.Errorf("expected empty suggestion for file (not dir), got %q", suggestion)
	}
}
