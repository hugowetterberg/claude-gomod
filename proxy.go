package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	defaultProxyURL = "https://proxy.golang.org"
	maxZipSize      = 100 << 20 // 100 MB
)

// ErrModuleNotFound is returned when the proxy responds with 404 or 410.
var ErrModuleNotFound = errors.New("module not found")

// ProxyClient fetches module data from proxy.golang.org.
type ProxyClient struct {
	baseURL string
	client  *http.Client
}

func NewProxyClient() *ProxyClient {
	return &ProxyClient{
		baseURL: defaultProxyURL,
		client:  http.DefaultClient,
	}
}

// ListVersions returns the list of known versions for a module.
func (p *ProxyClient) ListVersions(ctx context.Context, module string) ([]string, error) {
	url := fmt.Sprintf("%s/%s/@v/list", p.baseURL, encodePath(module))

	body, err := p.get(ctx, url)
	if err != nil {
		return nil, err
	}

	var versions []string

	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			versions = append(versions, line)
		}
	}

	return versions, nil
}

// Latest returns the JSON info for the latest version of a module.
func (p *ProxyClient) Latest(ctx context.Context, module string) (string, error) {
	url := fmt.Sprintf("%s/%s/@latest", p.baseURL, encodePath(module))

	body, err := p.get(ctx, url)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// ResolveLatest resolves "latest" to a concrete version string.
func (p *ProxyClient) ResolveLatest(ctx context.Context, module string) (string, error) {
	url := fmt.Sprintf("%s/%s/@latest", p.baseURL, encodePath(module))

	body, err := p.get(ctx, url)
	if err != nil {
		return "", err
	}

	// The response is JSON like {"Version":"v0.1.0","Time":"..."}
	// Do a simple extraction to avoid importing encoding/json just for this.
	s := string(body)

	const key = `"Version":"`

	i := strings.Index(s, key)
	if i < 0 {
		return "", fmt.Errorf("cannot parse latest response: %s", s)
	}

	s = s[i+len(key):]

	j := strings.Index(s, `"`)
	if j < 0 {
		return "", fmt.Errorf("cannot parse latest response version")
	}

	return s[:j], nil
}

// ReadMod returns the go.mod content for a module version.
func (p *ProxyClient) ReadMod(ctx context.Context, module, version string) (string, error) {
	url := fmt.Sprintf("%s/%s/@v/%s.mod", p.baseURL, encodePath(module), version)

	body, err := p.get(ctx, url)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// DownloadZip downloads the zip archive for a module version.
func (p *ProxyClient) DownloadZip(ctx context.Context, module, version string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/@v/%s.zip", p.baseURL, encodePath(module), version)

	body, err := p.get(ctx, url)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (p *ProxyClient) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, ErrModuleNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxZipSize+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if len(body) > maxZipSize {
		return nil, fmt.Errorf("response too large (>%d bytes)", maxZipSize)
	}

	return body, nil
}

// encodePath encodes a module path for use in proxy URLs.
// Uppercase letters are replaced with !lowercase per the
// Go module proxy protocol.
func encodePath(path string) string {
	var b strings.Builder

	for _, r := range path {
		if 'A' <= r && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}

	return b.String()
}
