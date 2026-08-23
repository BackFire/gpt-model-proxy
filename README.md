# gpt-model-proxy

A small Go reverse proxy for OpenAI-compatible APIs. It routes requests by model or rewrites request JSON fields and headers before forwarding them to a configurable upstream.

Primary use case: point Codex at this local proxy and replace internal request models such as `codex-auto-review` with a backend-supported model such as `gpt-5.5`.

## Features

- Rewrites the top-level JSON `model` field.
- Routes different request models to independent upstreams.
- Replaces `Authorization` with the selected route's API key.
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
  "routes": {
    "gpt-5.6-sol": {
      "upstream_base_url": "https://cds.example/v1/",
      "upstream_model": "gpt-5.6-sol",
      "api_key_env": "GMP_CDS_API_KEY"
    },
    "gpt-5.6-luna": {
      "upstream_base_url": "https://chanjike.example/v1/",
      "upstream_model": "gpt-5.6-luna",
      "api_key_env": "GMP_CHANJIKE_API_KEY"
    },
    "gpt-5.6-terra": {
      "upstream_base_url": "https://chanjike.example/v1/",
      "upstream_model": "gpt-5.6-terra",
      "api_key_env": "GMP_CHANJIKE_API_KEY"
    }
  },
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

In route mode, the incoming top-level `model` selects a route. `upstream_model` defaults to the route name when omitted. Every route requires an API key. Prefer `api_key_env`; the proxy reads that variable at startup and replaces the incoming `Authorization` header before forwarding. A private local config may use `api_key` directly when a service manager cannot provide environment variables. If both are set, `api_key_env` wins. Keep configs containing `api_key` at mode `0600`. Requests with missing or unknown models are rejected with `400` instead of being sent to a fallback upstream.

Export the upstream credentials in the proxy service environment:

```bash
export GMP_CDS_API_KEY=...
export GMP_CHANJIKE_API_KEY=...
```

The legacy single-upstream fields `upstream_base_url` and `model`, plus their CLI and environment variable equivalents, remain supported when `routes` is absent.

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
base_url = "http://127.0.0.1:8787/"
wire_api = "responses"
```

The local provider does not need client-side authentication. The selected route adds its own upstream `Authorization` header.

With `routes` configured, the proxy forwards `gpt-5.6-sol` to its CDS route and `gpt-5.6-luna` or `gpt-5.6-terra` to their chanjike routes. In legacy mode, it forwards requests to `upstream_base_url` or `GMP_UPSTREAM` and rewrites:

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
