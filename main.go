package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	homeDir, _ := os.UserHomeDir()
	defaultLocalDir := filepath.Join(homeDir, "Projects")

	localDir := flag.String("local-dir", defaultLocalDir, "Base directory for local module fallback")

	flag.Parse()

	proxy := NewProxyClient()
	cache := NewZipCache()
	local := NewLocalReader(*localDir)

	var modCacheDir string

	if out, err := exec.Command("go", "env", "GOMODCACHE").Output(); err != nil {
		log.Printf("warning: could not determine GOMODCACHE: %v (mod cache disabled)", err)
	} else {
		modCacheDir = strings.TrimSpace(string(out))
	}

	modCache := NewModCache(modCacheDir)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "claude-gomod",
		Version: "0.1.0",
	}, nil)

	registerTools(server, proxy, cache, local, modCache)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
