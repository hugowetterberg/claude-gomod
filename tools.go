package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
