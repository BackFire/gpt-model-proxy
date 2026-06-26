package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyRewritesModelAndUserAgent(t *testing.T) {
	var gotModel string
	var gotUA string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotUA = req.UserAgent()
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		gotModel, _ = payload["model"].(string)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	p := newTestProxy(t, Config{
		ListenAddr:      "127.0.0.1:0",
		UpstreamBaseURL: upstream.URL + "/v1/",
		Model:           "gpt-5.5",
		UserAgent:       "gpt-model-proxy-test",
		ModelField:      "model",
		MaxRewriteBytes: DefaultMaxRewriteBytes,
	})

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/responses", bytes.NewBufferString(`{"model":"codex-auto-review","input":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotModel != "gpt-5.5" {
		t.Fatalf("model = %q, want gpt-5.5", gotModel)
	}
	if gotUA != "gpt-model-proxy-test" {
		t.Fatalf("user-agent = %q, want rewrite", gotUA)
	}
}

func TestProxyPreservesBodyWhenModelFieldMissing(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		gotBody = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := newTestProxy(t, Config{
		ListenAddr:      "127.0.0.1:0",
		UpstreamBaseURL: upstream.URL,
		Model:           "gpt-5.5",
		ModelField:      "model",
		MaxRewriteBytes: DefaultMaxRewriteBytes,
	})

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/responses", bytes.NewBufferString(`{"input":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotBody != `{"input":"x"}` {
		t.Fatalf("body = %q, want original", gotBody)
	}
}

func TestRewriteGzipJSONModel(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte(`{"model":"codex-auto-review","input":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	rewritten, changed, err := rewriteJSONModel(compressed.Bytes(), "gzip", "model", "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}

	reader, err := gzip.NewReader(bytes.NewReader(rewritten))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var payload map[string]any
	if err := json.NewDecoder(reader).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "gpt-5.5" {
		t.Fatalf("model = %v, want gpt-5.5", payload["model"])
	}
}

func TestProxyPreservesUnknownLengthBodyWhenTooLarge(t *testing.T) {
	const originalBody = `{"model":"codex-auto-review","input":"0123456789"}`
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		gotBody = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := newTestProxy(t, Config{
		ListenAddr:      "127.0.0.1:0",
		UpstreamBaseURL: upstream.URL,
		Model:           "gpt-5.5",
		ModelField:      "model",
		MaxRewriteBytes: 5,
	})

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/responses", nil)
	req.Body = io.NopCloser(strings.NewReader(originalBody))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotBody != originalBody {
		t.Fatalf("body = %q, want original", gotBody)
	}
}

func TestMapPathDoesNotDuplicateBasePath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		reqPath  string
		want     string
	}{
		{
			name:     "request already includes base path",
			basePath: "/v1/",
			reqPath:  "/v1/responses",
			want:     "/v1/responses",
		},
		{
			name:     "request omits base path",
			basePath: "/v1/",
			reqPath:  "/responses",
			want:     "/v1/responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapPath(tt.basePath, tt.reqPath); got != tt.want {
				t.Fatalf("mapPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func newTestProxy(t *testing.T, cfg Config) *Proxy {
	t.Helper()
	p, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
