package main

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFileMatches_NoFilters(t *testing.T) {
	if !fileMatches("cmd/main.go", nil, "") {
		t.Error("expected match with no filters")
	}
}

func TestFileMatches_Extensions(t *testing.T) {
	exts := []string{".go", ".mod"}

	if !fileMatches("main.go", exts, "") {
		t.Error("expected .go to match")
	}

	if !fileMatches("go.mod", exts, "") {
		t.Error("expected .mod to match")
	}

	if fileMatches("README.md", exts, "") {
		t.Error("expected .md to be filtered out")
	}
}

func TestFileMatches_ExtensionsCaseInsensitive(t *testing.T) {
	exts := []string{".GO"}

	if !fileMatches("main.go", exts, "") {
		t.Error("expected case-insensitive extension match")
	}
}

func TestFileMatches_Glob(t *testing.T) {
	if !fileMatches("cmd/run.go", nil, "cmd/*.go") {
		t.Error("expected cmd/*.go to match cmd/run.go")
	}

	if fileMatches("lib/util.go", nil, "cmd/*.go") {
		t.Error("expected cmd/*.go not to match lib/util.go")
	}
}

func TestFileMatches_GlobAndExtensions(t *testing.T) {
	exts := []string{".go"}

	if !fileMatches("cmd/run.go", exts, "cmd/*") {
		t.Error("expected match with both filters passing")
	}

	if fileMatches("cmd/README.md", exts, "cmd/*") {
		t.Error("expected extension filter to reject .md")
	}

	if fileMatches("lib/util.go", exts, "cmd/*") {
		t.Error("expected glob filter to reject lib/ path")
	}
}

func TestFileMatches_InvalidGlob(t *testing.T) {
	// Invalid glob pattern should not match anything.
	if fileMatches("main.go", nil, "[invalid") {
		t.Error("expected invalid glob to not match")
	}
}

func TestSearchContent_BasicMatch(t *testing.T) {
	content := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	re := regexp.MustCompile(`Println`)

	matches := searchContent(re, content, 0, 0)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	if matches[0].LineNum != 4 {
		t.Errorf("expected line 4, got %d", matches[0].LineNum)
	}

	if !strings.Contains(matches[0].Line, "Println") {
		t.Errorf("expected match line to contain Println: %q", matches[0].Line)
	}
}

func TestSearchContent_NoMatches(t *testing.T) {
	content := "package main\n\nfunc main() {}\n"
	re := regexp.MustCompile(`doesnotexist`)

	matches := searchContent(re, content, 0, 0)

	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestSearchContent_ContextLines(t *testing.T) {
	content := "line1\nline2\nline3\nMATCH\nline5\nline6\nline7\n"
	re := regexp.MustCompile(`MATCH`)

	matches := searchContent(re, content, 2, 2)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	m := matches[0]

	if len(m.Before) != 2 {
		t.Fatalf("expected 2 before lines, got %d", len(m.Before))
	}

	if m.Before[0] != "line2" || m.Before[1] != "line3" {
		t.Errorf("unexpected before: %v", m.Before)
	}

	if len(m.After) != 2 {
		t.Fatalf("expected 2 after lines, got %d", len(m.After))
	}

	if m.After[0] != "line5" || m.After[1] != "line6" {
		t.Errorf("unexpected after: %v", m.After)
	}
}

func TestSearchContent_ContextAtBoundaries(t *testing.T) {
	content := "MATCH\nline2\nline3\n"
	re := regexp.MustCompile(`MATCH`)

	matches := searchContent(re, content, 3, 3)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	m := matches[0]

	if len(m.Before) != 0 {
		t.Errorf("expected 0 before lines at start of file, got %d", len(m.Before))
	}

	if len(m.After) != 2 {
		t.Errorf("expected 2 after lines (capped by file end), got %d", len(m.After))
	}
}

func TestSearchContent_MultipleMatches(t *testing.T) {
	content := "aaa\nbbb\naaa\nbbb\naaa\n"
	re := regexp.MustCompile(`aaa`)

	matches := searchContent(re, content, 0, 0)

	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}

	if matches[0].LineNum != 1 {
		t.Errorf("expected first match at line 1, got %d", matches[0].LineNum)
	}

	if matches[1].LineNum != 3 {
		t.Errorf("expected second match at line 3, got %d", matches[1].LineNum)
	}

	if matches[2].LineNum != 5 {
		t.Errorf("expected third match at line 5, got %d", matches[2].LineNum)
	}
}

func TestToolsSearchFiles(t *testing.T) {
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"go.mod":  "module example.com/testmod\n\ngo 1.21\n",
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		"lib.go":  "package main\n\nfunc helper() string {\n\treturn \"world\"\n}\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_search_files", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"pattern": "func",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "main.go") {
		t.Error("expected main.go in results")
	}

	if !strings.Contains(text, "lib.go") {
		t.Error("expected lib.go in results")
	}

	if !strings.Contains(text, "func main()") {
		t.Error("expected func main() match")
	}

	if !strings.Contains(text, "func helper()") {
		t.Error("expected func helper() match")
	}
}

func TestToolsSearchFiles_WithContext(t *testing.T) {
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"main.go": "line1\nline2\nMATCH\nline4\nline5\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_search_files", map[string]any{
		"module":       "example.com/testmod",
		"version":      "v1.0.0",
		"pattern":      "MATCH",
		"lines_before": 1,
		"lines_after":  1,
	})

	text := resultText(t, result)

	if !strings.Contains(text, "2- line2") {
		t.Errorf("expected before-context line: %s", text)
	}

	if !strings.Contains(text, "3: MATCH") {
		t.Errorf("expected match line: %s", text)
	}

	if !strings.Contains(text, "4- line4") {
		t.Errorf("expected after-context line: %s", text)
	}
}

func TestToolsSearchFiles_Extensions(t *testing.T) {
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"main.go":   "package main\nfunc main() {}\n",
		"go.mod":    "module example.com/testmod\n",
		"README.md": "# main module\nfunc is mentioned here too\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_search_files", map[string]any{
		"module":     "example.com/testmod",
		"version":    "v1.0.0",
		"pattern":    "func",
		"extensions": []string{".go"},
	})

	text := resultText(t, result)

	if !strings.Contains(text, "main.go") {
		t.Error("expected main.go in results")
	}

	if strings.Contains(text, "README.md") {
		t.Error("expected README.md to be filtered out")
	}
}

func TestToolsSearchFiles_Glob(t *testing.T) {
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"cmd/run.go":  "package cmd\nfunc Run() {}\n",
		"cmd/help.go": "package cmd\nfunc Help() {}\n",
		"lib/util.go": "package lib\nfunc Util() {}\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_search_files", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"pattern": "func",
		"glob":    "cmd/*.go",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "cmd/run.go") {
		t.Error("expected cmd/run.go in results")
	}

	if !strings.Contains(text, "cmd/help.go") {
		t.Error("expected cmd/help.go in results")
	}

	if strings.Contains(text, "lib/util.go") {
		t.Error("expected lib/util.go to be filtered out by glob")
	}
}

func TestToolsSearchFiles_InvalidRegexp(t *testing.T) {
	env := setupTestEnv(t, fakeProxy(nil))
	defer env.close()

	result := callTool(t, env, "gomod_search_files", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"pattern": "[invalid",
	})

	if !result.IsError {
		t.Fatal("expected IsError for invalid regexp")
	}

	text := resultText(t, result)

	if !strings.Contains(text, "invalid regexp") {
		t.Errorf("expected 'invalid regexp' in error: %s", text)
	}
}

func TestToolsSearchFiles_NoMatches(t *testing.T) {
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_search_files", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"pattern": "doesnotexist",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "0 matches") {
		t.Errorf("expected '0 matches' in output: %s", text)
	}
}

func TestToolsSearchFiles_FromModCache(t *testing.T) {
	var zipHits atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".zip") {
			zipHits.Add(1)
		}

		http.NotFound(w, r)
	})

	env := setupTestEnv(t, handler)
	defer env.close()

	populateModCache(t, env.modCacheDir, "example.com/testmod", "v1.0.0", map[string]string{
		"main.go": "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		"go.mod":  "module example.com/testmod\n\ngo 1.21\n",
	})

	result := callTool(t, env, "gomod_search_files", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"pattern": "Println",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "main.go") {
		t.Errorf("expected main.go in results: %s", text)
	}

	if !strings.Contains(text, "Println") {
		t.Errorf("expected Println in results: %s", text)
	}

	if zipHits.Load() != 0 {
		t.Error("proxy zip endpoint should not have been hit")
	}
}

func TestToolsListReturnsFiveTools(t *testing.T) {
	env := setupTestEnv(t, fakeProxy(nil))
	defer env.close()

	result, err := env.session.ListTools(context.Background(), nil)

	mustf(t, err, "list tools")

	names := make(map[string]bool)

	for _, tool := range result.Tools {
		names[tool.Name] = true
	}

	for _, want := range []string{
		"gomod_list_versions",
		"gomod_read_mod",
		"gomod_list_files",
		"gomod_read_file",
		"gomod_search_files",
	} {
		if !names[want] {
			t.Errorf("missing tool %q in tools/list response", want)
		}
	}
}
