package main

import "testing"

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
