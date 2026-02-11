# claude-gomod

MCP server that exposes Go module proxy data as tools for Claude.

## Build & Test

```bash
go build .              # Build the binary
go test ./...           # Run all tests
golangci-lint run ./... # Lint (strict config in .golangci.yml)
```

## Architecture

Single-package (`main`) MCP server using `github.com/modelcontextprotocol/go-sdk`.

- `main.go` — Entry point, discovers GOMODCACHE, wires dependencies
- `tools.go` — MCP tool registration and handlers (`gomod_list_versions`, `gomod_read_mod`, `gomod_list_files`, `gomod_read_file`)
- `proxy.go` — HTTP client for proxy.golang.org (`ProxyClient`, `encodePath`)
- `cache.go` — In-memory zip archive cache (`ZipCache`, `ZipEntry`)
- `modcache.go` — Local Go module cache reader (`ModCache`, reads from `$GOMODCACHE`)
- `local.go` — Local directory fallback suggestions (`LocalReader`)

Data flow: handlers check `ModCache` first (instant, no network), fall back to `ProxyClient` + `ZipCache`.

## Code Style

- Strict linting via `.golangci.yml` (wsl_v5, nlreturn, lll max 120, gci, gofumpt, gosec, wrapcheck, etc.)
- Add blank lines before returns, range loops, if statements, and variable declarations per wsl_v5
- Use `errors.Is()` not `==` for error comparison (errorlint)
- Use `_` for unused parameters (revive)
- Check all error returns including `w.Write` in tests (errcheck)
- Use `0o600` not `0o644` for `os.WriteFile` in tests (gosec)
- Wrap errors from external packages (wrapcheck)
- Comments must end with a period (godot)
- Wrap comments at column 80.
- Use `fmt.Fprintf` instead of `WriteString(fmt.Sprintf(...))` (QF1012)
- Keep lines under 120 characters; break long function signatures across lines
- Printf-like functions must end with `f` (goprintffuncname)
- Test helper `testing.TB` params must be named `tb` (thelper)
- Use `mustf(t, err, "message", args...)` in tests for concise error checking (defined in `helpers_test.go`)
