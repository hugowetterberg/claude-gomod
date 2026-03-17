package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxSearchFiles   = 100
	maxSearchMatches = 1000
)

type listVersionsInput struct {
	Module string `json:"module" jsonschema:"Go module path, e.g. golang.org/x/tools"`
}

type readModInput struct {
	Module  string `json:"module" jsonschema:"Go module path"`
	Version string `json:"version" jsonschema:"Module version or 'latest'"`
}

type listFilesInput struct {
	Module  string `json:"module" jsonschema:"Go module path"`
	Version string `json:"version" jsonschema:"Module version or 'latest'"`
	Path    string `json:"path,omitempty" jsonschema:"Optional path prefix filter"`
}

type readFileInput struct {
	Module  string `json:"module" jsonschema:"Go module path"`
	Version string `json:"version" jsonschema:"Module version or 'latest'"`
	Path    string `json:"path" jsonschema:"File path within the module"`
}

type searchFilesInput struct {
	Module      string   `json:"module" jsonschema:"Go module path"`
	Version     string   `json:"version" jsonschema:"Module version or 'latest'"`
	Pattern     string   `json:"pattern" jsonschema:"Regular expression to search for"`
	LinesBefore int      `json:"lines_before,omitempty" jsonschema:"Context lines before each match (default 0)"`
	LinesAfter  int      `json:"lines_after,omitempty" jsonschema:"Context lines after each match (default 0)"`
	Extensions  []string `json:"extensions,omitempty" jsonschema:"File extensions to include (e.g. .go and .mod)"`
	Glob        string   `json:"glob,omitempty" jsonschema:"Glob pattern to filter file paths (path.Match syntax)"`
}

func registerTools(
	server *mcp.Server, proxy *ProxyClient, cache *ZipCache,
	local *LocalReader, modCache *ModCache,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "gomod_list_versions",
		Description: "List available versions of a Go module from the Go module proxy. " +
			"Returns version list and latest version info.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest,
		input listVersionsInput,
	) (*mcp.CallToolResult, any, error) {
		return handleListVersions(ctx, proxy, local, input)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "gomod_read_mod",
		Description: "Read the go.mod file of a Go module at a specific version. " +
			"Use version 'latest' to auto-resolve.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest,
		input readModInput,
	) (*mcp.CallToolResult, any, error) {
		return handleReadMod(ctx, proxy, modCache, input)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "gomod_list_files",
		Description: "List files in a Go module's source archive. Optionally filter by path prefix.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest,
		input listFilesInput,
	) (*mcp.CallToolResult, any, error) {
		return handleListFiles(ctx, proxy, cache, modCache, input)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "gomod_read_file",
		Description: "Read a source file from a Go module's archive. Rejects binary files.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest,
		input readFileInput,
	) (*mcp.CallToolResult, any, error) {
		return handleReadFile(ctx, proxy, cache, modCache, input)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "gomod_search_files",
		Description: "Search file contents in a Go module using a regular expression. " +
			"Returns matching lines with optional context. " +
			"Binary files are skipped silently.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest,
		input searchFilesInput,
	) (*mcp.CallToolResult, any, error) {
		return handleSearchFiles(ctx, proxy, cache, modCache, input)
	})
}

func handleListVersions(
	ctx context.Context, proxy *ProxyClient,
	local *LocalReader, input listVersionsInput,
) (*mcp.CallToolResult, any, error) {
	versions, err := proxy.ListVersions(ctx, input.Module)
	if err != nil {
		if errors.Is(err, ErrModuleNotFound) {
			return notFoundResult(input.Module, local), nil, nil
		}

		return nil, nil, err
	}

	latest, _ := proxy.Latest(ctx, input.Module)

	var sb strings.Builder

	fmt.Fprintf(&sb, "Versions of %s:\n", input.Module)

	for _, v := range versions {
		sb.WriteString(v)
		sb.WriteByte('\n')
	}

	if latest != "" {
		sb.WriteString("\nLatest info:\n")
		sb.WriteString(latest)
	}

	return textResult(sb.String()), nil, nil
}

func handleReadMod(
	ctx context.Context, proxy *ProxyClient,
	modCache *ModCache, input readModInput,
) (*mcp.CallToolResult, any, error) {
	version, err := resolveVersion(ctx, proxy, input.Module, input.Version)
	if err != nil {
		return nil, nil, err
	}

	if modCache.HasModule(input.Module, version) {
		content, err := modCache.ReadFile(input.Module, version, "go.mod")
		if err == nil {
			return textResult(content), nil, nil
		}
	}

	content, err := proxy.ReadMod(ctx, input.Module, version)
	if err != nil {
		return nil, nil, err
	}

	return textResult(content), nil, nil
}

func handleListFiles(
	ctx context.Context, proxy *ProxyClient, cache *ZipCache,
	modCache *ModCache, input listFilesInput,
) (*mcp.CallToolResult, any, error) {
	version, err := resolveVersion(ctx, proxy, input.Module, input.Version)
	if err != nil {
		return nil, nil, err
	}

	var files []string

	if modCache.HasModule(input.Module, version) {
		files, err = modCache.ListFiles(input.Module, version, input.Path)
		if err != nil {
			return nil, nil, err
		}
	} else {
		entry, err := getOrDownload(ctx, proxy, cache, input.Module, version)
		if err != nil {
			return nil, nil, err
		}

		files = entry.ListFiles(input.Path)
	}

	sort.Strings(files)

	var sb strings.Builder

	fmt.Fprintf(&sb, "Files in %s@%s", input.Module, version)

	if input.Path != "" {
		fmt.Fprintf(&sb, " (prefix: %s)", input.Path)
	}

	fmt.Fprintf(&sb, " (%d files):\n", len(files))

	for _, f := range files {
		sb.WriteString(f)
		sb.WriteByte('\n')
	}

	return textResult(sb.String()), nil, nil
}

func handleReadFile(
	ctx context.Context, proxy *ProxyClient, cache *ZipCache,
	modCache *ModCache, input readFileInput,
) (*mcp.CallToolResult, any, error) {
	version, err := resolveVersion(ctx, proxy, input.Module, input.Version)
	if err != nil {
		return nil, nil, err
	}

	if modCache.HasModule(input.Module, version) {
		content, err := modCache.ReadFile(input.Module, version, input.Path)
		if err != nil {
			return nil, nil, err
		}

		return textResult(content), nil, nil
	}

	entry, err := getOrDownload(ctx, proxy, cache, input.Module, version)
	if err != nil {
		return nil, nil, err
	}

	content, err := entry.ReadFile(input.Path)
	if err != nil {
		return nil, nil, err
	}

	return textResult(content), nil, nil
}

func handleSearchFiles(
	ctx context.Context, proxy *ProxyClient, cache *ZipCache,
	modCache *ModCache, input searchFilesInput,
) (*mcp.CallToolResult, any, error) {
	re, err := regexp.Compile(input.Pattern)
	if err != nil {
		return errorResult(
			fmt.Sprintf("invalid regexp %q: %v", input.Pattern, err),
		), nil, nil
	}

	version, err := resolveVersion(ctx, proxy, input.Module, input.Version)
	if err != nil {
		return nil, nil, err
	}

	files, readFile, err := listAndReader(
		ctx, proxy, cache, modCache, input.Module, version,
	)
	if err != nil {
		return nil, nil, err
	}

	sort.Strings(files)

	var (
		results      []searchFileResult
		totalMatches int
		truncated    int
	)

	for _, f := range files {
		if !fileMatches(f, input.Extensions, input.Glob) {
			continue
		}

		content, err := readFile(f)
		if err != nil {
			// Skip binary or unreadable files.
			continue
		}

		matches := searchContent(re, content, input.LinesBefore, input.LinesAfter)
		if len(matches) == 0 {
			continue
		}

		if len(results) >= maxSearchFiles {
			truncated += len(matches)

			continue
		}

		remaining := maxSearchMatches - totalMatches
		if len(matches) > remaining {
			truncated += len(matches) - remaining
			matches = matches[:remaining]
		}

		results = append(results, searchFileResult{
			Path:    f,
			Matches: matches,
		})

		totalMatches += len(matches)

		if totalMatches >= maxSearchMatches {
			break
		}
	}

	return textResult(
		formatSearchResults(input.Module, version, input.Pattern, results, truncated),
	), nil, nil
}

// listAndReader returns the file list and a read function for a
// module, using the mod cache when available and falling back to the
// proxy zip.
func listAndReader(
	ctx context.Context, proxy *ProxyClient, cache *ZipCache,
	modCache *ModCache, module, version string,
) ([]string, func(string) (string, error), error) {
	if modCache.HasModule(module, version) {
		files, err := modCache.ListFiles(module, version, "")
		if err != nil {
			return nil, nil, err
		}

		readFn := func(p string) (string, error) {
			return modCache.ReadFile(module, version, p)
		}

		return files, readFn, nil
	}

	entry, err := getOrDownload(ctx, proxy, cache, module, version)
	if err != nil {
		return nil, nil, err
	}

	return entry.ListFiles(""), entry.ReadFile, nil
}

func formatSearchResults(
	module, version, pattern string,
	results []searchFileResult, truncated int,
) string {
	var sb strings.Builder

	totalMatches := 0

	for _, r := range results {
		totalMatches += len(r.Matches)
	}

	fmt.Fprintf(
		&sb,
		"Search results for /%s/ in %s@%s (%d matches in %d files):\n",
		pattern, module, version, totalMatches, len(results),
	)

	for _, r := range results {
		fmt.Fprintf(&sb, "\n--- %s ---\n", r.Path)

		for i, m := range r.Matches {
			if i > 0 {
				// Separator between non-contiguous matches in the
				// same file.
				prevEnd := r.Matches[i-1].LineNum + len(r.Matches[i-1].After)
				thisStart := m.LineNum - len(m.Before)

				if thisStart > prevEnd {
					sb.WriteString("--\n")
				}
			}

			beforeStart := m.LineNum - len(m.Before)

			for j, line := range m.Before {
				fmt.Fprintf(&sb, "%d- %s\n", beforeStart+j, line)
			}

			fmt.Fprintf(&sb, "%d: %s\n", m.LineNum, m.Line)

			for j, line := range m.After {
				fmt.Fprintf(&sb, "%d- %s\n", m.LineNum+1+j, line)
			}
		}
	}

	if truncated > 0 {
		fmt.Fprintf(
			&sb,
			"\n(results truncated, %d additional matches not shown)\n",
			truncated,
		)
	}

	return sb.String()
}

func resolveVersion(
	ctx context.Context, proxy *ProxyClient, module, version string,
) (string, error) {
	if strings.EqualFold(version, "latest") {
		resolved, err := proxy.ResolveLatest(ctx, module)
		if err != nil {
			return "", fmt.Errorf("resolve latest version: %w", err)
		}

		return resolved, nil
	}

	return version, nil
}

func getOrDownload(
	ctx context.Context, proxy *ProxyClient,
	cache *ZipCache, module, version string,
) (*ZipEntry, error) {
	if entry := cache.Get(module, version); entry != nil {
		return entry, nil
	}

	data, err := proxy.DownloadZip(ctx, module, version)
	if err != nil {
		return nil, fmt.Errorf("download zip: %w", err)
	}

	entry, err := cache.Put(module, version, data)
	if err != nil {
		return nil, fmt.Errorf("cache zip: %w", err)
	}

	return entry, nil
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
		IsError: true,
	}
}

func notFoundResult(module string, local *LocalReader) *mcp.CallToolResult {
	if suggestion := local.Suggest(module); suggestion != "" {
		return textResult(suggestion)
	}

	return errorResult(fmt.Sprintf("Module %q not found on the Go module proxy.", module))
}
