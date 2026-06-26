package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

type Proxy struct {
	cfg      Config
	upstream *url.URL
	logger   *slog.Logger
	proxy    *httputil.ReverseProxy
}

func New(cfg Config, logger *slog.Logger) (*Proxy, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	upstream, err := url.Parse(cfg.UpstreamBaseURL)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	p := &Proxy{
		cfg:      cfg,
		upstream: upstream,
		logger:   logger,
	}

	rp := httputil.NewSingleHostReverseProxy(upstream)
	originalDirector := rp.Director
	rp.Director = func(req *http.Request) {
		originalPath := req.URL.Path
		originalDirector(req)
		req.URL.Scheme = upstream.Scheme
		req.URL.Host = upstream.Host
		req.URL.Path = mapPath(upstream.Path, originalPath)
		req.URL.RawPath = ""
		if !cfg.PreserveHost {
			req.Host = upstream.Host
		}
		req.Header.Set("X-Forwarded-Host", req.Host)
		appendForwardedFor(req)
		if cfg.UserAgent != "" {
			req.Header.Set("User-Agent", cfg.UserAgent)
		}
	}
	rp.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del("Alt-Svc")
		return nil
	}
	rp.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		logger.Error("proxy request failed", "method", req.Method, "path", req.URL.Path, "error", err)
		http.Error(w, "proxy request failed", http.StatusBadGateway)
	}

	p.proxy = rp
	return p, nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if err := p.rewriteRequest(req); err != nil {
		p.logger.Warn("request rewrite skipped", "method", req.Method, "path", req.URL.Path, "error", err)
	}
	p.proxy.ServeHTTP(w, req)
}

func (p *Proxy) rewriteRequest(req *http.Request) error {
	if p.cfg.Model == "" || req.Body == nil {
		return nil
	}
	if !isJSONRequest(req.Header.Get("Content-Type")) {
		return nil
	}
	if req.ContentLength > p.cfg.MaxRewriteBytes {
		return errBodyTooLarge
	}

	body, err := readLimited(req.Body, p.cfg.MaxRewriteBytes)
	if err != nil {
		if len(body) > 0 {
			req.Body = prependBody(body, req.Body)
		}
		return err
	}
	defer req.Body.Close()

	rewritten, changed, err := rewriteJSONModel(body, req.Header.Get("Content-Encoding"), p.cfg.ModelField, p.cfg.Model)
	if err != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		return err
	}
	if !changed {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		return nil
	}

	req.Body = io.NopCloser(bytes.NewReader(rewritten))
	req.ContentLength = int64(len(rewritten))
	req.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(rewritten)), nil
	}
	return nil
}

var errBodyTooLarge = errors.New("request body is larger than rewrite limit")

func isJSONRequest(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.Contains(contentType, "application/json") || strings.Contains(contentType, "+json")
}

func readLimited(body io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return data, err
	}
	if int64(len(data)) > maxBytes {
		return data, errBodyTooLarge
	}
	return data, nil
}

func prependBody(prefix []byte, body io.ReadCloser) io.ReadCloser {
	return &prefixReadCloser{
		reader: io.MultiReader(bytes.NewReader(prefix), body),
		closer: body,
	}
}

type prefixReadCloser struct {
	reader io.Reader
	closer io.Closer
}

func (p *prefixReadCloser) Read(data []byte) (int, error) {
	return p.reader.Read(data)
}

func (p *prefixReadCloser) Close() error {
	return p.closer.Close()
}

func rewriteJSONModel(body []byte, encoding string, field string, model string) ([]byte, bool, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		rewritten, changed, err := rewritePlainJSONModel(body, field, model)
		if err != nil {
			return nil, false, err
		}
		return rewritten, changed, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, false, err
		}
		plain, err := io.ReadAll(reader)
		closeErr := reader.Close()
		if err != nil {
			return nil, false, err
		}
		if closeErr != nil {
			return nil, false, closeErr
		}
		rewritten, changed, err := rewritePlainJSONModel(plain, field, model)
		if err != nil || !changed {
			return nil, changed, err
		}
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(rewritten); err != nil {
			return nil, false, err
		}
		if err := writer.Close(); err != nil {
			return nil, false, err
		}
		return compressed.Bytes(), true, nil
	default:
		return nil, false, errors.New("unsupported request content encoding")
	}
}

func rewritePlainJSONModel(body []byte, field string, model string) ([]byte, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, err
	}
	current, exists := payload[field]
	if !exists {
		return body, false, nil
	}
	if current == model {
		return body, false, nil
	}
	payload[field] = model

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	return rewritten, true, nil
}

func mapPath(basePath, reqPath string) string {
	if basePath == "" || basePath == "/" {
		return reqPath
	}
	if reqPath == "" || reqPath == "/" {
		return basePath
	}
	normalizedBase := strings.TrimRight(basePath, "/")
	if reqPath == normalizedBase || strings.HasPrefix(reqPath, normalizedBase+"/") {
		return reqPath
	}
	return normalizedBase + "/" + strings.TrimLeft(reqPath, "/")
}

func appendForwardedFor(req *http.Request) {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	if host == "" {
		return
	}
	prior := req.Header.Get("X-Forwarded-For")
	if prior == "" {
		req.Header.Set("X-Forwarded-For", host)
		return
	}
	req.Header.Set("X-Forwarded-For", prior+", "+host)
}
