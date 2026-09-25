package main

import (
	"strings"
	"testing"
)

func TestResolveListenAddrs(t *testing.T) {
	tests := []struct {
		name      string
		flagSet   bool
		flagValue string
		envValue  string
		config    []string
		legacy    string
		want      []string
	}{
		{
			name:   "config list",
			config: []string{"127.0.0.1:8787", "192.168.3.1:8787"},
			legacy: "127.0.0.1:8787",
			want:   []string{"127.0.0.1:8787", "192.168.3.1:8787"},
		},
		{
			name:   "legacy config",
			legacy: "127.0.0.1:8787",
			want:   []string{"127.0.0.1:8787"},
		},
		{
			name:     "environment",
			envValue: "127.0.0.1:8787, 192.168.3.1:8787",
			config:   []string{"ignored:1"},
			want:     []string{"127.0.0.1:8787", "192.168.3.1:8787"},
		},
		{
			name:      "flag",
			flagSet:   true,
			flagValue: "127.0.0.1:8787,192.168.3.1:8787",
			envValue:  "ignored:1",
			config:    []string{"ignored:2"},
			want:      []string{"127.0.0.1:8787", "192.168.3.1:8787"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveListenAddrs(tt.flagSet, tt.flagValue, tt.envValue, tt.config, tt.legacy)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("addresses = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveListenAddrsRejectsEmptyAddress(t *testing.T) {
	if _, err := resolveListenAddrs(false, "", "", []string{"127.0.0.1:8787", ""}, ""); err == nil {
		t.Fatal("expected empty listen address to be rejected")
	}
}

func TestOpenListenersSupportsMultipleAddresses(t *testing.T) {
	listeners, err := openListeners([]string{"127.0.0.1:0", "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	if len(listeners) != 2 {
		t.Fatalf("opened %d listeners, want 2", len(listeners))
	}
}

func TestResolveRoutesReadsAPIKeysFromEnvironment(t *testing.T) {
	t.Setenv("TEST_CDS_API_KEY", "cds-key")
	t.Setenv("TEST_CHANJIKE_API_KEY", "chanjike-key")

	routes := resolveRoutes(map[string]fileRouteConfig{
		"gpt-5.6-sol": {
			UpstreamBaseURL: "https://cds.example/v1/",
			UpstreamModel:   "sol-upstream",
			APIKeyEnv:       "TEST_CDS_API_KEY",
			APIKey:          "ignored-file-key",
		},
		"gpt-5.6-luna": {
			UpstreamBaseURL: "https://chanjike.example/v1/",
			APIKey:          "chanjike-file-key",
		},
	})

	if routes["gpt-5.6-sol"].APIKey != "cds-key" {
		t.Fatalf("sol API key was not resolved")
	}
	if routes["gpt-5.6-sol"].Model != "sol-upstream" {
		t.Fatalf("sol upstream model = %q, want sol-upstream", routes["gpt-5.6-sol"].Model)
	}
	if routes["gpt-5.6-luna"].APIKey != "chanjike-file-key" {
		t.Fatalf("luna API key was not resolved")
	}
}
