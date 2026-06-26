package proxy

import (
	"errors"
	"net/url"
	"strings"
)

const DefaultMaxRewriteBytes int64 = 64 << 20

type Config struct {
	ListenAddr      string
	UpstreamBaseURL string
	Model           string
	UserAgent       string
	ModelField      string
	PreserveHost    bool
	MaxRewriteBytes int64
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return errors.New("listen address is required")
	}
	if strings.TrimSpace(c.UpstreamBaseURL) == "" {
		return errors.New("upstream base URL is required")
	}
	upstream, err := url.Parse(c.UpstreamBaseURL)
	if err != nil {
		return err
	}
	if upstream.Scheme != "http" && upstream.Scheme != "https" {
		return errors.New("upstream scheme must be http or https")
	}
	if upstream.Host == "" {
		return errors.New("upstream host is required")
	}
	if strings.TrimSpace(c.ModelField) == "" {
		return errors.New("model field is required")
	}
	if c.MaxRewriteBytes <= 0 {
		return errors.New("max rewrite bytes must be positive")
	}
	return nil
}
