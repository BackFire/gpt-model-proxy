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
	"sync"
	"testing"
)

func TestProxyRoutesModelsToIndependentUpstreams(t *testing.T) {
	type receivedRequest struct {
		upstream string
		model    string
		auth     string
	}
	var (
		mu       sync.Mutex
		received []receivedRequest
	)
	newUpstream := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			received = append(received, receivedRequest{
				upstream: name,
				model:    payload["model"].(string),
				auth:     req.Header.Get("Authorization"),
			})
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}))
	}
	cds := newUpstream("cds")
	defer cds.Close()
	chanjike := newUpstream("chanjike")
	defer chanjike.Close()

	p := newTestProxy(t, Config{
		ListenAddr: "127.0.0.1:0",
		Routes: map[string]RouteConfig{
			"gpt-5.6-sol": {
				UpstreamBaseURL: cds.URL + "/v1/",
				Model:           "cds-sol",
				APIKey:          "cds-key",
			},
			"gpt-5.6-luna": {
				UpstreamBaseURL: chanjike.URL + "/v1/",
				Model:           "chanjike-luna",
				APIKey:          "chanjike-key",
			},
			"gpt-5.6-terra": {
				UpstreamBaseURL: chanjike.URL + "/v1/",
				APIKey:          "chanjike-key",
			},
		},
		ModelField:      "model",
		MaxRewriteBytes: DefaultMaxRewriteBytes,
	})

	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-luna", "gpt-5.6-terra"} {
		req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/responses", bytes.NewBufferString(`{"model":"`+model+`","input":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer client-key")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("model %s: status = %d, want 200", model, rec.Code)
		}
	}

	want := []receivedRequest{
		{upstream: "cds", model: "cds-sol", auth: "Bearer cds-key"},
		{upstream: "chanjike", model: "chanjike-luna", auth: "Bearer chanjike-key"},
		{upstream: "chanjike", model: "gpt-5.6-terra", auth: "Bearer chanjike-key"},
	}
	if len(received) != len(want) {
		t.Fatalf("received %d requests, want %d", len(received), len(want))
	}
	for i := range want {
		if received[i] != want[i] {
			t.Errorf("request %d = %+v, want %+v", i, received[i], want[i])
		}
	}
}

func TestProxyRejectsUnknownRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Error("unknown model request reached upstream")
	}))
	defer upstream.Close()
	p := newTestProxy(t, Config{
		ListenAddr: "127.0.0.1:0",
		Routes: map[string]RouteConfig{
			"gpt-5.6-sol": {UpstreamBaseURL: upstream.URL, APIKey: "key"},
		},
		ModelField:      "model",
		MaxRewriteBytes: DefaultMaxRewriteBytes,
	})
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/responses", bytes.NewBufferString(`{"model":"unknown"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `no route configured for model "unknown"`) {
		t.Fatalf("body = %q, want unknown model error", rec.Body.String())
	}
}

func TestProxyRoutesGzipRequest(t *testing.T) {
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		reader, err := gzip.NewReader(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		var payload map[string]any
		if err := json.NewDecoder(reader).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		gotModel, _ = payload["model"].(string)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte(`{"model":"gpt-5.6-luna"}`))
	_ = writer.Close()
	p := newTestProxy(t, Config{
		ListenAddr: "127.0.0.1:0",
		Routes: map[string]RouteConfig{
			"gpt-5.6-luna": {UpstreamBaseURL: upstream.URL, Model: "luna-upstream", APIKey: "key"},
		},
		ModelField:      "model",
		MaxRewriteBytes: DefaultMaxRewriteBytes,
	})
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/responses", &compressed)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotModel != "luna-upstream" {
		t.Fatalf("model = %q, want luna-upstream", gotModel)
	}
}

func TestProxyPassesThroughSSEResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "data: second\n\n")
	}))
	defer upstream.Close()
	p := newTestProxy(t, Config{
		ListenAddr: "127.0.0.1:0",
		Routes: map[string]RouteConfig{
			"gpt-5.6-sol": {UpstreamBaseURL: upstream.URL, APIKey: "key"},
		},
		ModelField:      "model",
		MaxRewriteBytes: DefaultMaxRewriteBytes,
	})
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6-sol"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}
	if got, want := rec.Body.String(), "data: first\n\ndata: second\n\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

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
		UserAgent:       "codex-tui/0.159.2",
		ModelField:      "model",
		MaxRewriteBytes: DefaultMaxRewriteBytes,
	})

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/responses", bytes.NewBufferString(`{"model":"codex-auto-review","input":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenAI/Python 2.0")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotModel != "gpt-5.5" {
		t.Fatalf("model = %q, want gpt-5.5", gotModel)
	}
	if gotUA != "codex-tui/0.159.2" {
		t.Fatalf("user-agent = %q, want rewrite", gotUA)
	}
}

func TestProxyPreservesCodexUserAgent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(req.UserAgent()))
	}))
	defer upstream.Close()

	p := newTestProxy(t, Config{
		ListenAddr:      "127.0.0.1:0",
		UpstreamBaseURL: upstream.URL,
		UserAgent:       "codex-tui/0.159.2",
		ModelField:      "model",
		MaxRewriteBytes: DefaultMaxRewriteBytes,
	})
	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/models", nil)
	req.Header.Set("User-Agent", "codex_cli_rs/0.159.2")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "codex_cli_rs/0.159.2" {
		t.Fatalf("user-agent = %q, want original Codex user-agent", got)
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
