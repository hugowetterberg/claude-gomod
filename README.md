# claude-gomod

An MCP server that gives Claude access to Go module source code from [proxy.golang.org](https://proxy.golang.org). When a module isn't available on the proxy, it suggests local directories where the source might exist.

## Tools

| Tool | Description |
|------|-------------|
| `gomod_list_versions` | List available versions of a module |
| `gomod_read_mod` | Read a module's go.mod file |
| `gomod_list_files` | List files in a module's source archive |
| `gomod_read_file` | Read a source file from a module's archive |

All tools accept `"latest"` as the version, which is resolved via the proxy's `/@latest` endpoint.

When the proxy returns 404, `gomod_list_versions` checks `~/Projects` for a local directory matching the module's last path segment and suggests it as a fallback.

## Install

```bash
go install github.com/hugowetterberg/claude-gomod@latest
```

Or build from source:

```bash
git clone https://github.com/hugowetterberg/claude-gomod.git
cd claude-gomod
go build .
```

## Register with Claude Code

Add it globally (available across all projects):

```bash
claude mcp add --scope user gomod -- /path/to/claude-gomod
```

Or for the current project only (the default):

```bash
claude mcp add gomod -- /path/to/claude-gomod
```

During development you can point at the source directory:

```bash
claude mcp add --scope user gomod -- go run /path/to/claude-gomod
```

With a custom local fallback directory:

```bash
claude mcp add --scope user gomod -- /path/to/claude-gomod -local-dir /other/path
```

## Usage examples

Once registered, Claude can use the tools directly:

- "List versions of golang.org/x/tools"
- "Show me the go.mod of golang.org/x/net@latest"
- "List files in github.com/modelcontextprotocol/go-sdk v1.1.0"
- "Read server.go from github.com/modelcontextprotocol/go-sdk v1.1.0"

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-local-dir` | `~/Projects` | Base directory for local module fallback |

## Running tests

```bash
go test ./...
```
