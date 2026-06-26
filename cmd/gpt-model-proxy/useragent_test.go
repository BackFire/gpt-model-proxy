package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestResolveUserAgentAuto(t *testing.T) {
	t.Setenv("GMP_TERM", "xterm-256color")

	ua := resolveUserAgent("auto", "")
	required := []string{
		"codex-tui/",
		"; " + runtime.GOARCH + ")",
		"xterm-256color",
		"(codex-tui; 1.0.0)",
	}
	for _, part := range required {
		if !strings.Contains(ua, part) {
			t.Fatalf("ua %q does not contain %q", ua, part)
		}
	}
}

func TestResolveUserAgentAutoDefaultsDumbTerm(t *testing.T) {
	t.Setenv("GMP_TERM", "")
	t.Setenv("TERM", "dumb")

	ua := resolveUserAgent("auto", "")
	if !strings.Contains(ua, "xterm-256color") {
		t.Fatalf("ua %q does not contain xterm-256color", ua)
	}
}

func TestResolveUserAgentAutoUsesConfiguredCodexVersion(t *testing.T) {
	t.Setenv("GMP_TERM", "xterm-256color")

	ua := resolveUserAgent("auto", "0.142.2")
	if !strings.HasPrefix(ua, "codex-tui/0.142.2 ") {
		t.Fatalf("ua = %q, want configured codex version", ua)
	}
}

func TestResolveUserAgentExplicit(t *testing.T) {
	if got := resolveUserAgent("custom", "0.142.2"); got != "custom" {
		t.Fatalf("resolveUserAgent() = %q, want custom", got)
	}
}
