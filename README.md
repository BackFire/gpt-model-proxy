# gpt-model-proxy

A small Go reverse proxy for OpenAI-compatible APIs. It rewrites request JSON fields and headers before forwarding the request to a configurable upstream.

Primary use case: point Codex at this local proxy and replace internal request models such as `codex-auto-review` with a backend-supported model such as `gpt-5.5`.

## Features

- Rewrites the top-level JSON `model` field.
- Rewrites `User-Agent`.
- Supports configurable upstream `base_url`.
- Streams upstream responses through `httputil.ReverseProxy`.
- No third-party runtime dependencies.

## Build

```bash
go build -o bin/gpt-model-proxy ./cmd/gpt-model-proxy
```

## Install

```bash
go build -o "$HOME/.local/bin/gpt-model-proxy" ./cmd/gpt-model-proxy
mkdir -p "$HOME/.config/gpt-model-proxy"
```

The installed binary path is `$HOME/.local/bin/gpt-model-proxy`.

## Configuration

Default config path:

```text
$HOME/.config/gpt-model-proxy/config.json
```

Start from the checked-in example:

```bash
mkdir -p "$HOME/.config/gpt-model-proxy"
cp config/config.example.json "$HOME/.config/gpt-model-proxy/config.json"
```

Example fields:

```json
{
  "listen_addr": "127.0.0.1:8787",
  "upstream_base_url": "https://api.openai.com/v1/",
  "model": "gpt-5.5",
  "user_agent": "auto",
  "codex_version": "",
  "model_field": "model",
  "preserve_host": false,
  "max_rewrite_bytes": 67108864,
  "shutdown_timeout": "10s",
  "log_level": "info"
}
```

Use `GMP_CONFIG=/path/to/config.json` to use another config file.

For private gateways, fill the real `upstream_base_url` only in your local config file. Do not commit private gateway hosts, tokens, or account-specific upstream URLs.

Set `user_agent` to `auto` to generate a value like:

```text
codex-tui/0.142.1 (Mac OS 26.5.1; arm64) xterm-256color (codex-tui; 1.0.0)
```

`auto` reads `codex --version`, OS version, CPU architecture, and `$TERM`. Use `codex_version` or `GMP_CODEX_VERSION` when the service host does not have `codex` on PATH. Use `GMP_TERM` to override terminal detection. If `$TERM` is empty, `dumb`, or `unknown`, the proxy uses `xterm-256color`.

CLI flags override environment variables, and environment variables override the config file.

## Run

```bash
gpt-model-proxy
```

The same settings can also be provided as environment variables:

```bash
GMP_LISTEN=127.0.0.1:8787 \
GMP_UPSTREAM=https://api.openai.com/v1/ \
GMP_MODEL=gpt-5.5 \
GMP_USER_AGENT=auto \
bin/gpt-model-proxy
```

The proxy logs startup, shutdown, skipped rewrites, and upstream forwarding errors to stderr using Go `slog` text output. It does not print the configured upstream URL. It has no built-in history file or notification output.

## Autostart

Install the binary and config first, then install autostart:

```bash
go build -o "$HOME/.local/bin/gpt-model-proxy" ./cmd/gpt-model-proxy
mkdir -p "$HOME/.config/gpt-model-proxy"
cp config/config.example.json "$HOME/.config/gpt-model-proxy/config.json"
scripts/install-autostart.sh
```

On macOS, the script installs:

```text
$HOME/Library/LaunchAgents/com.backfire.gpt-model-proxy.plist
```

macOS logs go to:

```text
$HOME/Library/Logs/gpt-model-proxy/stdout.log
$HOME/Library/Logs/gpt-model-proxy/stderr.log
```

Useful macOS commands:

```bash
launchctl print "gui/$(id -u)/com.backfire.gpt-model-proxy"
launchctl bootout "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.backfire.gpt-model-proxy.plist"
```

On Debian, the script installs a system-level systemd service so the proxy starts at boot even when the user has not logged in:

```text
/etc/systemd/system/gpt-model-proxy.service
```

The service runs as the user who executed the installer and reads that user's `$HOME/.config/gpt-model-proxy/config.json`.

Run `scripts/install-autostart.sh` as the target user, not with `sudo`; the script calls `sudo` only for writing and enabling the systemd unit.

Useful Debian commands:

```bash
systemctl status gpt-model-proxy.service
journalctl -u gpt-model-proxy.service -f
sudo systemctl disable --now gpt-model-proxy.service
```

## Verify

```bash
go test ./...
go test -race ./...
go build -o "$HOME/.local/bin/gpt-model-proxy" ./cmd/gpt-model-proxy
sh -n scripts/install-autostart.sh
```

## Codex Config Example

Point your model provider at the local proxy:

```toml
[model_providers.local_proxy]
name = "local_proxy"
base_url = "http://127.0.0.1:8787/v1/"
wire_api = "responses"
requires_openai_auth = true
```

The proxy forwards requests to `upstream_base_url` or `GMP_UPSTREAM` and rewrites:

```json
{"model":"codex-auto-review"}
```

to:

```json
{"model":"gpt-5.5"}
```

## Options

```text
-listen             listen address, default 127.0.0.1:8787
-upstream           upstream base URL, required
-model              replacement model
-user-agent         replacement User-Agent
-model-field        JSON field to rewrite, default model
-preserve-host      forward the original Host header
-max-rewrite-bytes  max request body bytes eligible for JSON rewriting, default 67108864
-shutdown-timeout   graceful shutdown timeout, default 10s
-log-level          debug, info, warn, error
```

## Notes

Only JSON request bodies are inspected. Non-JSON requests and responses are passed through unchanged, apart from configured header rewrites.
