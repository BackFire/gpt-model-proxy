package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const autoUserAgent = "auto"

func resolveUserAgent(value string) string {
	value = strings.TrimSpace(value)
	if !strings.EqualFold(value, autoUserAgent) {
		return value
	}
	return buildAutoUserAgent()
}

func buildAutoUserAgent() string {
	codexVersion := detectCodexVersion()
	osName, osVersion := detectOS()
	arch := runtime.GOARCH
	term := detectTerminal()

	return "codex-tui/" + codexVersion + " (" + osName + " " + osVersion + "; " + arch + ") " + term + " (codex-tui; 1.0.0)"
}

func detectTerminal() string {
	if term := strings.TrimSpace(os.Getenv("GMP_TERM")); term != "" {
		return term
	}
	term := strings.TrimSpace(os.Getenv("TERM"))
	switch strings.ToLower(term) {
	case "", "dumb", "unknown":
		return "xterm-256color"
	default:
		return term
	}
}

func detectCodexVersion() string {
	output, err := exec.Command("codex", "--version").CombinedOutput()
	if err != nil {
		output, err = exec.Command(os.Args[0], "--version").CombinedOutput()
		if err != nil {
			return "unknown"
		}
	}
	fields := strings.Fields(string(output))
	for i, field := range fields {
		if field == "codex-cli" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	for _, field := range fields {
		if looksLikeVersion(field) {
			return field
		}
	}
	return "unknown"
}

func detectOS() (string, string) {
	switch runtime.GOOS {
	case "darwin":
		output, err := exec.Command("sw_vers", "-productVersion").Output()
		if err != nil {
			return "Mac OS", "unknown"
		}
		return "Mac OS", strings.TrimSpace(string(output))
	case "linux":
		if version := readOSReleaseVersion(); version != "" {
			return "Linux", version
		}
		return "Linux", "unknown"
	default:
		return runtime.GOOS, "unknown"
	}
}

func readOSReleaseVersion() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key != "VERSION_ID" {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return ""
}

func looksLikeVersion(value string) bool {
	hasDigit := false
	hasDot := false
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '.':
			hasDot = true
		default:
			return false
		}
	}
	return hasDigit && hasDot
}
