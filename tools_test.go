package main

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// testEnv sets up a full MCP server with a fake proxy and returns a connected
// client session for calling tools.
type testEnv struct {
	session     *mcp.ClientSession
	proxyHTTP   *httptest.Server
	localDir    string
	modCacheDir string
}

func (e *testEnv) close() {
	e.session.Close()
	e.proxyHTTP.Close()
}

func setupTestEnv(t *testing.T, handler http.Handler) *testEnv {
	t.Helper()

	ts := httptest.NewServer(handler)
	proxy := &ProxyClient{baseURL: ts.URL, client: ts.Client()}
	cache := NewZipCache()
	localDir := t.TempDir()
	local := NewLocalReader(localDir)
	modCacheDir := t.TempDir()
	modCache := NewModCache(modCacheDir)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "claude-gomod-test",
		Version: "0.0.1",
	}, nil)

	registerTools(server, proxy, cache, local, modCache)

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "0.0.1",
	}, nil)

	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, t1, nil)
	if err != nil {
		ts.Close()
		t.Fatal(err)
	}

	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		ts.Close()
		t.Fatal(err)
	}

	return &testEnv{
		session:     session,
		proxyHTTP:   ts,
		localDir:    localDir,
		modCacheDir: modCacheDir,
	}
}

func callTool(
	t *testing.T, env *testEnv, name string, args map[string]any,
) *mcp.CallToolResult {
	t.Helper()

	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}

	return result
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	return tc.Text
}

// fakeProxy routes requests for a test module.
func fakeProxy(zipData []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/example.com/testmod/@v/list":
			_, _ = w.Write([]byte("v0.1.0\nv0.2.0\nv1.0.0\n"))
		case "/example.com/testmod/@latest":
			_, _ = w.Write([]byte(`{"Version":"v1.0.0","Time":"2025-06-01T00:00:00Z"}`))
		case "/example.com/testmod/@v/v1.0.0.mod":
			_, _ = w.Write([]byte("module example.com/testmod\n\ngo 1.21\n"))
		case "/example.com/testmod/@v/v1.0.0.zip":
			_, _ = w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	})
}

func TestToolsListVersions(t *testing.T) {
	env := setupTestEnv(t, fakeProxy(nil))
	defer env.close()

	result := callTool(t, env, "gomod_list_versions", map[string]any{
		"module": "example.com/testmod",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "v0.1.0") {
		t.Error("expected v0.1.0 in output")
	}

	if !strings.Contains(text, "v1.0.0") {
		t.Error("expected v1.0.0 in output")
	}

	if !strings.Contains(text, "Latest") {
		t.Error("expected latest info in output")
	}
}

func TestToolsListVersions_NotFound_NoLocal(t *testing.T) {
	env := setupTestEnv(t, fakeProxy(nil))
	defer env.close()

	result := callTool(t, env, "gomod_list_versions", map[string]any{
		"module": "example.com/nonexistent",
	})

	if !result.IsError {
		t.Error("expected IsError=true for not found module")
	}

	text := resultText(t, result)
	if !strings.Contains(text, "not found") {
		t.Errorf("expected 'not found' in error: %s", text)
	}
}

func TestToolsListVersions_NotFound_WithLocalFallback(t *testing.T) {
	env := setupTestEnv(t, fakeProxy(nil))
	defer env.close()

	// Create a local directory matching the module's last path segment.
	mustf(t, os.Mkdir(filepath.Join(env.localDir, "nonexistent"), 0o755), "create local dir")

	result := callTool(t, env, "gomod_list_versions", map[string]any{
		"module": "example.com/nonexistent",
	})

	if result.IsError {
		t.Error("should not be error when local fallback exists")
	}

	text := resultText(t, result)

	if !strings.Contains(text, "local directory") {
		t.Errorf("expected local fallback suggestion: %s", text)
	}

	if !strings.Contains(text, env.localDir) {
		t.Errorf("expected local dir path in suggestion: %s", text)
	}
}

func TestToolsReadMod(t *testing.T) {
	env := setupTestEnv(t, fakeProxy(nil))
	defer env.close()

	result := callTool(t, env, "gomod_read_mod", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
	})

	text := resultText(t, result)
	if !strings.Contains(text, "module example.com/testmod") {
		t.Errorf("unexpected go.mod content: %s", text)
	}
}

func TestToolsReadMod_Latest(t *testing.T) {
	env := setupTestEnv(t, fakeProxy(nil))
	defer env.close()

	result := callTool(t, env, "gomod_read_mod", map[string]any{
		"module":  "example.com/testmod",
		"version": "latest",
	})

	text := resultText(t, result)
	if !strings.Contains(text, "module example.com/testmod") {
		t.Errorf("expected go.mod content after resolving latest: %s", text)
	}
}

func TestToolsListFiles(t *testing.T) {
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"go.mod":      "module example.com/testmod\n",
		"main.go":     "package main\n",
		"cmd/run.go":  "package cmd\n",
		"lib/util.go": "package lib\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_list_files", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "4 files") {
		t.Errorf("expected '4 files' in output: %s", text)
	}

	if !strings.Contains(text, "main.go") {
		t.Error("expected main.go in file list")
	}

	if !strings.Contains(text, "cmd/run.go") {
		t.Error("expected cmd/run.go in file list")
	}
}

func TestToolsListFiles_WithPrefix(t *testing.T) {
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"go.mod":      "module example.com/testmod\n",
		"main.go":     "package main\n",
		"cmd/run.go":  "package cmd\n",
		"cmd/help.go": "package cmd\n",
		"lib/util.go": "package lib\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_list_files", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"path":    "cmd/",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "2 files") {
		t.Errorf("expected '2 files' in output: %s", text)
	}

	if !strings.Contains(text, "prefix: cmd/") {
		t.Error("expected prefix info in output")
	}

	if strings.Contains(text, "main.go") {
		t.Error("main.go should be filtered out by prefix")
	}
}

func TestToolsListFiles_LatestResolution(t *testing.T) {
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"go.mod": "module example.com/testmod\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_list_files", map[string]any{
		"module":  "example.com/testmod",
		"version": "latest",
	})

	text := resultText(t, result)
	// Should have resolved latest -> v1.0.0 and listed files.
	if !strings.Contains(text, "v1.0.0") {
		t.Errorf("expected resolved version in output: %s", text)
	}
}

func TestToolsReadFile(t *testing.T) {
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_read_file", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"path":    "main.go",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "package main") {
		t.Errorf("expected Go source in output: %s", text)
	}

	if !strings.Contains(text, "fmt.Println") {
		t.Errorf("expected function body in output: %s", text)
	}
}

func TestToolsReadFile_NotInArchive(t *testing.T) {
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"main.go": "package main\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_read_file", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"path":    "nonexistent.go",
	})

	if !result.IsError {
		t.Fatal("expected IsError for missing file")
	}

	text := resultText(t, result)
	if !strings.Contains(text, "not found") {
		t.Errorf("expected 'not found' in error text: %s", text)
	}
}

func TestToolsReadFile_Binary(t *testing.T) {
	zipData := createTestZipWithBinary(t, "example.com/testmod@v1.0.0/", "image.png",
		[]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0xff, 0xfe})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	result := callTool(t, env, "gomod_read_file", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"path":    "image.png",
	})

	if !result.IsError {
		t.Fatal("expected IsError for binary file")
	}

	text := resultText(t, result)
	if !strings.Contains(text, "binary") {
		t.Errorf("expected 'binary' in error text: %s", text)
	}
}

func TestToolsZipCaching(t *testing.T) {
	// Verify that the zip is only downloaded once for multiple tool calls.
	downloadCount := 0
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n",
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/example.com/testmod/@v/v1.0.0.zip":
			downloadCount++
			_, _ = w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	})

	env := setupTestEnv(t, handler)
	defer env.close()

	// First call downloads the zip.
	callTool(t, env, "gomod_list_files", map[string]any{
		"module": "example.com/testmod", "version": "v1.0.0",
	})

	if downloadCount != 1 {
		t.Fatalf("expected 1 download, got %d", downloadCount)
	}

	// Second call should use cache.
	callTool(t, env, "gomod_read_file", map[string]any{
		"module": "example.com/testmod", "version": "v1.0.0", "path": "b.go",
	})

	if downloadCount != 1 {
		t.Fatalf("expected still 1 download after cache hit, got %d", downloadCount)
	}
}

func TestToolsListReturnsAllFourTools(t *testing.T) {
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
	} {
		if !names[want] {
			t.Errorf("missing tool %q in tools/list response", want)
		}
	}
}

// createTestZipWithBinary creates a zip containing a single binary file.
func createTestZipWithBinary(t *testing.T, prefix, name string, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := zip.NewWriter(&buf)

	f, err := w.Create(prefix + name)

	mustf(t, err, "create %s in zip", name)

	_, err = f.Write(data)

	mustf(t, err, "write binary data to zip")

	w.Close()

	return buf.Bytes()
}

// populateModCache creates a fake module directory in the test env's mod cache.
func populateModCache(t *testing.T, dir, module, version string, files map[string]string) {
	t.Helper()

	modDir := filepath.Join(dir, encodePath(module)+"@"+version)

	for name, content := range files {
		full := filepath.Join(modDir, filepath.FromSlash(name))

		mustf(t, os.MkdirAll(filepath.Dir(full), 0o755), "create parent dir for %s", name)
		mustf(t, os.WriteFile(full, []byte(content), 0o600), "write %s", name)
	}
}

func TestToolsReadFile_FromModCache(t *testing.T) {
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
		"main.go": "package main\n\nfunc main() {}\n",
		"go.mod":  "module example.com/testmod\n\ngo 1.21\n",
	})

	result := callTool(t, env, "gomod_read_file", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"path":    "main.go",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "package main") {
		t.Errorf("expected source from mod cache: %s", text)
	}

	if zipHits.Load() != 0 {
		t.Error("proxy zip endpoint should not have been hit")
	}
}

func TestToolsListFiles_FromModCache(t *testing.T) {
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
		"go.mod":   "module example.com/testmod\n",
		"main.go":  "package main\n",
		"lib/a.go": "package lib\n",
	})

	result := callTool(t, env, "gomod_list_files", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "3 files") {
		t.Errorf("expected '3 files' in output: %s", text)
	}

	if !strings.Contains(text, "main.go") {
		t.Error("expected main.go in file list")
	}

	if zipHits.Load() != 0 {
		t.Error("proxy zip endpoint should not have been hit")
	}
}

func TestToolsReadMod_FromModCache(t *testing.T) {
	var modHits atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".mod") {
			modHits.Add(1)
		}

		http.NotFound(w, r)
	})

	env := setupTestEnv(t, handler)
	defer env.close()

	populateModCache(t, env.modCacheDir, "example.com/testmod", "v1.0.0", map[string]string{
		"go.mod": "module example.com/testmod\n\ngo 1.21\n",
	})

	result := callTool(t, env, "gomod_read_mod", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
	})

	text := resultText(t, result)

	if !strings.Contains(text, "module example.com/testmod") {
		t.Errorf("expected go.mod content from mod cache: %s", text)
	}

	if modHits.Load() != 0 {
		t.Error("proxy .mod endpoint should not have been hit")
	}
}

func TestToolsReadFile_FallsBackToProxy(t *testing.T) {
	// Module is NOT in mod cache, so it should fall back to proxy zip.
	zipData := createTestZip(t, "example.com/testmod@v1.0.0/", map[string]string{
		"main.go": "package main\n",
	})

	env := setupTestEnv(t, fakeProxy(zipData))
	defer env.close()

	// Don't populate mod cache — it should fall through to proxy.
	result := callTool(t, env, "gomod_read_file", map[string]any{
		"module":  "example.com/testmod",
		"version": "v1.0.0",
		"path":    "main.go",
	})

	text := resultText(t, result)
	if !strings.Contains(text, "package main") {
		t.Errorf("expected source from proxy fallback: %s", text)
	}
}
