package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEncodePath(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"golang.org/x/tools", "golang.org/x/tools"},
		{"github.com/Azure/go-sdk", "github.com/!azure/go-sdk"},
		{"github.com/BurntSushi/toml", "github.com/!burnt!sushi/toml"},
		{"nochange", "nochange"},
		{"ALL", "!a!l!l"},
	}

	for _, tt := range tests {
		got := encodePath(tt.input)
		if got != tt.want {
			t.Errorf("encodePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func newTestProxy(handler http.Handler) (*ProxyClient, *httptest.Server) {
	ts := httptest.NewServer(handler)

	return &ProxyClient{baseURL: ts.URL, client: ts.Client()}, ts
}

func TestProxyClient_ListVersions(t *testing.T) {
	proxy, ts := newTestProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/example.com/mod/@v/list" {
			http.NotFound(w, r)

			return
		}

		if _, err := w.Write([]byte("v0.1.0\nv0.2.0\nv1.0.0\n")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer ts.Close()

	versions, err := proxy.ListVersions(context.Background(), "example.com/mod")
	if err != nil {
		t.Fatal(err)
	}

	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}

	if versions[2] != "v1.0.0" {
		t.Errorf("versions[2] = %q, want %q", versions[2], "v1.0.0")
	}
}

func TestProxyClient_ListVersions_NotFound(t *testing.T) {
	proxy, ts := newTestProxy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer ts.Close()

	_, err := proxy.ListVersions(context.Background(), "example.com/nonexistent")
	if !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("got err=%v, want ErrModuleNotFound", err)
	}
}

func TestProxyClient_ListVersions_Gone(t *testing.T) {
	proxy, ts := newTestProxy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer ts.Close()

	_, err := proxy.ListVersions(context.Background(), "example.com/gone")
	if !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("got err=%v, want ErrModuleNotFound", err)
	}
}

func TestProxyClient_ResolveLatest(t *testing.T) {
	proxy, ts := newTestProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/example.com/mod/@latest" {
			http.NotFound(w, r)

			return
		}

		if _, err := w.Write([]byte(`{"Version":"v1.2.3","Time":"2025-01-01T00:00:00Z"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer ts.Close()

	version, err := proxy.ResolveLatest(context.Background(), "example.com/mod")
	if err != nil {
		t.Fatal(err)
	}

	if version != "v1.2.3" {
		t.Errorf("got %q, want %q", version, "v1.2.3")
	}
}

func TestProxyClient_ReadMod(t *testing.T) {
	proxy, ts := newTestProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/example.com/mod/@v/v1.0.0.mod" {
			http.NotFound(w, r)

			return
		}

		if _, err := w.Write([]byte("module example.com/mod\n\ngo 1.21\n")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer ts.Close()

	content, err := proxy.ReadMod(context.Background(), "example.com/mod", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	if content != "module example.com/mod\n\ngo 1.21\n" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestProxyClient_DownloadZip(t *testing.T) {
	zipData := createTestZip(t, "example.com/mod@v1.0.0/", map[string]string{
		"go.mod": "module example.com/mod\n",
	})

	proxy, ts := newTestProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/example.com/mod/@v/v1.0.0.zip" {
			http.NotFound(w, r)

			return
		}

		if _, err := w.Write(zipData); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer ts.Close()

	data, err := proxy.DownloadZip(context.Background(), "example.com/mod", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Fatal("empty zip data")
	}
}

func TestProxyClient_CaseEncoding(t *testing.T) {
	var gotPath string

	proxy, ts := newTestProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path

		if _, err := w.Write([]byte("v1.0.0\n")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer ts.Close()

	if _, err := proxy.ListVersions(context.Background(), "github.com/Azure/go-sdk"); err != nil {
		t.Fatal(err)
	}

	want := "/github.com/!azure/go-sdk/@v/list"
	if gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
}

func TestProxyClient_ServerError(t *testing.T) {
	proxy, ts := newTestProxy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	_, err := proxy.ListVersions(context.Background(), "example.com/mod")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}

	if errors.Is(err, ErrModuleNotFound) {
		t.Fatal("500 should not be ErrModuleNotFound")
	}
}
