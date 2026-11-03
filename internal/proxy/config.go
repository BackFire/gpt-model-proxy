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
	Routes          map[string]RouteConfig
	DefaultRoute    *RouteConfig
	UserAgent       string
	ModelField      string
	PreserveHost    bool
	MaxRewriteBytes int64
}

type RouteConfig struct {
	UpstreamBaseURL string
	Model           string
	APIKey          string
	OverrideAPIKey  *bool
}

func (r RouteConfig) overridesAPIKey() bool {
	return r.OverrideAPIKey == nil || *r.OverrideAPIKey
}

func (r RouteConfig) validate() error {
	if err := validateUpstreamURL(r.UpstreamBaseURL); err != nil {
		return err
	}
	if r.overridesAPIKey() && strings.TrimSpace(r.APIKey) == "" {
		return errors.New("API key is required when override_api_key is enabled")
	}
	return nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return errors.New("listen address is required")
	}
	if c.DefaultRoute != nil {
		if err := c.DefaultRoute.validate(); err != nil {
			return errors.New("default route: " + err.Error())
		}
	} else if len(c.Routes) == 0 {
		if err := validateUpstreamURL(c.UpstreamBaseURL); err != nil {
			return err
		}
	}
	for model, route := range c.Routes {
		if strings.TrimSpace(model) == "" {
			return errors.New("route model is required")
		}
		if err := route.validate(); err != nil {
			return errors.New("route " + model + ": " + err.Error())
		}
	}
	if strings.TrimSpace(c.ModelField) == "" {
		return errors.New("model field is required")
	}
	if c.MaxRewriteBytes <= 0 {
		return errors.New("max rewrite bytes must be positive")
	}
	return nil
}

func validateUpstreamURL(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("upstream base URL is required")
	}
	upstream, err := url.Parse(value)
	if err != nil {
		return err
	}
	if upstream.Scheme != "http" && upstream.Scheme != "https" {
		return errors.New("upstream scheme must be http or https")
	}
	if upstream.Host == "" {
		return errors.New("upstream host is required")
	}
	return nil
}
