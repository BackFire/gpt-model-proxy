package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BackFire/gpt-model-proxy/internal/proxy"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gpt-model-proxy: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fileCfg, err := loadFileConfig(defaultConfigPath())
	if err != nil {
		return err
	}

	var flags cliFlags
	flag.StringVar(&flags.ListenAddr, "listen", "127.0.0.1:8787", "listen address")
	flag.StringVar(&flags.UpstreamBaseURL, "upstream", "", "upstream base URL, for example https://api.openai.com/v1/")
	flag.StringVar(&flags.Model, "model", "", "replacement model for JSON request bodies")
	flag.StringVar(&flags.UserAgent, "user-agent", "", "replacement User-Agent header")
	flag.StringVar(&flags.ModelField, "model-field", "model", "JSON field to rewrite")
	flag.BoolVar(&flags.PreserveHost, "preserve-host", false, "forward the original Host header")
	flag.Int64Var(&flags.MaxRewriteBytes, "max-rewrite-bytes", proxy.DefaultMaxRewriteBytes, "max request body bytes eligible for JSON rewriting")
	flag.DurationVar(&flags.ShutdownTimeout, "shutdown-timeout", 10*time.Second, "graceful shutdown timeout")
	flag.StringVar(&flags.LogLevel, "log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	setFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	cfg := proxy.Config{
		ListenAddr:      pickString(setFlags["listen"], flags.ListenAddr, os.Getenv("GMP_LISTEN"), fileCfg.ListenAddr, "127.0.0.1:8787"),
		UpstreamBaseURL: pickString(setFlags["upstream"], flags.UpstreamBaseURL, os.Getenv("GMP_UPSTREAM"), fileCfg.UpstreamBaseURL),
		Model:           pickString(setFlags["model"], flags.Model, os.Getenv("GMP_MODEL"), fileCfg.Model),
		UserAgent:       resolveUserAgent(pickString(setFlags["user-agent"], flags.UserAgent, os.Getenv("GMP_USER_AGENT"), fileCfg.UserAgent), firstString(os.Getenv("GMP_CODEX_VERSION"), fileCfg.CodexVersion)),
		ModelField:      pickString(setFlags["model-field"], flags.ModelField, os.Getenv("GMP_MODEL_FIELD"), fileCfg.ModelField, "model"),
		PreserveHost:    pickBool(setFlags["preserve-host"], flags.PreserveHost, os.Getenv("GMP_PRESERVE_HOST"), fileCfg.PreserveHost, false),
		MaxRewriteBytes: pickInt64(setFlags["max-rewrite-bytes"], flags.MaxRewriteBytes, os.Getenv("GMP_MAX_REWRITE_BYTES"), fileCfg.MaxRewriteBytes, proxy.DefaultMaxRewriteBytes),
	}
	logLevel := pickString(setFlags["log-level"], flags.LogLevel, os.Getenv("GMP_LOG_LEVEL"), fileCfg.LogLevel, "info")
	shutdownTimeout := pickDuration(setFlags["shutdown-timeout"], flags.ShutdownTimeout, os.Getenv("GMP_SHUTDOWN_TIMEOUT"), fileCfg.ShutdownTimeout, 10*time.Second)

	if err := cfg.Validate(); err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLogLevel(logLevel)}))
	handler, err := proxy.New(cfg, logger)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("proxy listening", "listen", cfg.ListenAddr, "upstream_configured", cfg.UpstreamBaseURL != "")
		errCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutting down", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type fileConfig struct {
	ListenAddr      string `json:"listen_addr"`
	UpstreamBaseURL string `json:"upstream_base_url"`
	Model           string `json:"model"`
	UserAgent       string `json:"user_agent"`
	ModelField      string `json:"model_field"`
	PreserveHost    *bool  `json:"preserve_host"`
	MaxRewriteBytes int64  `json:"max_rewrite_bytes"`
	ShutdownTimeout string `json:"shutdown_timeout"`
	LogLevel        string `json:"log_level"`
	CodexVersion    string `json:"codex_version"`
}

type cliFlags struct {
	ListenAddr      string
	UpstreamBaseURL string
	Model           string
	UserAgent       string
	ModelField      string
	PreserveHost    bool
	MaxRewriteBytes int64
	ShutdownTimeout time.Duration
	LogLevel        string
}

func loadFileConfig(path string) (fileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fileConfig{}, nil
		}
		return fileConfig{}, err
	}
	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fileConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}
	return cfg, nil
}

func defaultConfigPath() string {
	if value := strings.TrimSpace(os.Getenv("GMP_CONFIG")); value != "" {
		return value
	}
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.json"
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "gpt-model-proxy", "config.json")
}

func firstString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func pickString(isSet bool, flagValue string, values ...string) string {
	if isSet {
		return strings.TrimSpace(flagValue)
	}
	return firstString(values...)
}

func firstBool(envValue string, fileValue *bool, fallback bool) bool {
	if value := strings.TrimSpace(envValue); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	if fileValue != nil {
		return *fileValue
	}
	return fallback
}

func pickBool(isSet bool, flagValue bool, envValue string, fileValue *bool, fallback bool) bool {
	if isSet {
		return flagValue
	}
	return firstBool(envValue, fileValue, fallback)
}

func firstInt64(envValue string, fileValue int64, fallback int64) int64 {
	value := strings.TrimSpace(envValue)
	if value == "" && fileValue > 0 {
		return fileValue
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func pickInt64(isSet bool, flagValue int64, envValue string, fileValue int64, fallback int64) int64 {
	if isSet {
		return flagValue
	}
	return firstInt64(envValue, fileValue, fallback)
}

func firstDuration(envValue string, fileValue string, fallback time.Duration) time.Duration {
	value := firstString(envValue, fileValue)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func pickDuration(isSet bool, flagValue time.Duration, envValue string, fileValue string, fallback time.Duration) time.Duration {
	if isSet {
		return flagValue
	}
	return firstDuration(envValue, fileValue, fallback)
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
