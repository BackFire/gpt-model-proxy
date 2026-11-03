# gpt-model-proxy

A small Go reverse proxy for OpenAI-compatible APIs. It routes requests by model or rewrites request JSON fields and headers before forwarding them to a configurable upstream.

Primary use case: point Codex at this local proxy and route GPT-6 Astra, Sol, 6.1 Sol, Luna, and internal requests such as `codex-auto-review` to backend-supported models.

## Features

- Rewrites the top-level JSON `model` field.
- Routes different request models to independent upstreams, with an optional default route for unmatched models.
- Listens on multiple configured addresses in one process.
- Optionally replaces `Authorization` with the selected route's API key.
- Rewrites non-Codex `User-Agent` headers.
- Supports configurable upstream `base_url`.
- Streams upstream responses through `httputil.ReverseProxy`.
- No third-party runtime dependencies.

## Build

Requires Go 1.26 or newer.

```bash
go build -o bin/gpt-model-proxy ./cmd/gpt-model-proxy
```

## Install

```bash
mkdir -p "$HOME/.local/bin"
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

For an existing installation, merge the example into your config instead of overwriting it. Before starting the proxy, replace the example upstream URL and provide the default route's credentials. The minimal example forwards all model names unchanged to one default upstream; add explicit routes only for exceptions.

Example fields:

```json
{
  "listen_addrs": [
    "127.0.0.1:8787"
  ],
  "default_route": {
    "upstream_base_url": "https://cds.example/v1/",
    "override_api_key": true,
    "api_key_env": "GMP_CDS_API_KEY"
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

`listen_addrs` creates one listener per address in the same process. To also serve a LAN interface, add its actual address to the list, for example `"192.168.3.1:8787"`. Every configured address must exist on the host when the proxy starts. The legacy `listen_addr` field remains supported; `-listen` and `GMP_LISTEN` accept comma-separated addresses and override the config file. Restrict LAN access to trusted clients: the proxy has no inbound authentication and can add its upstream API key to routed requests.

Use `GMP_CONFIG=/path/to/config.json` to use another config file.

In route mode, the incoming top-level `model` selects an exact route first. If no route matches, `default_route` is used when configured; otherwise the proxy returns `400`. An explicit route's `upstream_model` defaults to its route name. The default route preserves the requested model unless its own `upstream_model` is set. `default_route` also works without any explicit routes and handles requests without JSON bodies, such as `GET /v1/models`. JSON requests still require a valid nonempty model; malformed JSON, unsupported content encodings, and bodies exceeding the routing limit remain errors.

Both `routes` entries and `default_route` accept `override_api_key`:

- `true`: replace the incoming `Authorization` header with `Bearer <configured key>`, including when the client sends a different key or no key. A nonempty configured key is required.
- `false`: preserve the client's `Authorization` header, including its absence. No configured key is required; any configured `api_key` or `api_key_env` value is unused for forwarding.
- Omitted: defaults to `true` for compatibility with existing route configs.

Prefer `api_key_env`; the proxy reads that variable at startup. A private local config may use `api_key` directly when a service manager cannot provide environment variables. If both are set, `api_key_env` wins. Keep configs containing `api_key` at mode `0600`. For example, add this `routes` entry to forward client credentials:

```json
{
  "routes": {
    "gpt-client-auth": {
      "upstream_base_url": "https://other.example/v1/",
      "override_api_key": false
    }
  }
}
```

Export the upstream credentials in the proxy service environment:

```bash
export GMP_CDS_API_KEY=...
```

The legacy single-upstream fields `upstream_base_url` and `model`, plus their CLI and environment variable equivalents, remain supported when both `routes` and `default_route` are absent.

For private gateways, fill the real `upstream_base_url` only in your local config file. Do not commit private gateway hosts, tokens, or account-specific upstream URLs.

Set `user_agent` to `auto` to generate a value like:

```text
codex-tui/0.142.1 (Mac OS 26.5.1; arm64) xterm-256color (codex-tui; 1.0.0)
```

`auto` reads `codex --version`, OS version, CPU architecture, and `$TERM`. Use `codex_version` or `GMP_CODEX_VERSION` when the service host does not have `codex` on PATH. Use `GMP_TERM` to override terminal detection. If `$TERM` is empty, `dumb`, or `unknown`, the proxy uses `xterm-256color`.

Incoming `User-Agent` values beginning with `codex/`, `codex-`, or `codex_` are preserved, including `codex-tui` and `codex_cli_rs`. Detection ignores case and surrounding whitespace. Other or missing values use the configured `user_agent`. An empty or omitted `user_agent` disables rewriting.

Some gateways route requests based on User-Agent. Verify model access through the proxy with `user_agent = "auto"`; a direct request with another User-Agent can return `model_not_found` even for an available model. A `/models` listing alone does not prove that a Responses request will succeed.

CLI flags override environment variables, and environment variables override the config file.

## Run

```bash
gpt-model-proxy
```

When both `routes` and `default_route` are absent, the legacy settings can also be provided as environment variables:

```bash
GMP_LISTEN=127.0.0.1:8787 \
GMP_UPSTREAM=https://api.openai.com/v1/ \
GMP_MODEL=gpt-5.5 \
GMP_USER_AGENT=auto \
bin/gpt-model-proxy
```

The proxy logs startup, shutdown, skipped rewrites, and upstream forwarding errors to stderr using Go `slog` text output. It does not print the configured upstream URL. It has no built-in history file or notification output.

## Autostart

Prepare the config and credentials as described above, then install the binary and autostart:

```bash
mkdir -p "$HOME/.local/bin"
go build -o "$HOME/.local/bin/gpt-model-proxy" ./cmd/gpt-model-proxy
scripts/install-autostart.sh
```

`scripts/install-autostart.sh` writes service configuration and enables autostart. It has no dry-run mode; `sh -n scripts/install-autostart.sh` checks syntax without changing services.

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
go build -o bin/gpt-model-proxy ./cmd/gpt-model-proxy
sh -n scripts/install-autostart.sh
```

## Codex Config Example

Merge [config/codex.example.toml](config/codex.example.toml) into your user-level `~/.codex/config.toml`, preserving existing settings. Provider settings must be in user-level config. The essential provider selection is:

```toml
model_provider = "local_proxy"
model = "gpt-6.1-sol"
review_model = "gpt-6-luna"
model_reasoning_effort = "medium"

[model_providers.local_proxy]
name = "local_proxy"
base_url = "http://127.0.0.1:8787/"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false

[agents]
default_subagent_model = "gpt-6.1-sol"
default_subagent_reasoning_effort = "medium"
```

With the example's `override_api_key: true`, the local provider does not need client-side authentication. The selected route adds its own upstream `Authorization` header. Routes using `override_api_key: false` require the client to supply any credentials needed by that upstream.

The examples select `gpt-6.1-sol` with `medium` reasoning for both the main session and subagents. The default route forwards model names and reasoning effort unchanged; no model aliases are configured.

The default route handles GPT-6 Astra, Sol, 6.1 Sol, Luna, `codex-auto-review`, and other model names without individual entries. `review_model` selects the code-review model; automatic approval review uses `codex-auto-review`. Verify that the default upstream supports each requested model. Add an explicit route with `upstream_model` only when a model needs a different upstream or name, for example:

```json
{
  "routes": {
    "special-model": {
      "upstream_base_url": "https://other.example/v1/",
      "upstream_model": "backend-model",
      "override_api_key": true,
      "api_key_env": "GMP_OTHER_API_KEY"
    }
  }
}
```

The proxy uses HTTP Responses with streaming; disable WebSocket transport for this provider. Model reasoning and other request fields pass through unchanged. After editing the proxy config, restart the service to load the new routes:

```bash
# macOS
launchctl kickstart -k "gui/$(id -u)/com.backfire.gpt-model-proxy"
# Debian
sudo systemctl restart gpt-model-proxy.service
```

After filling the real upstream settings and restarting the service, verify the configured model with a real Codex request:

```bash
codex exec --model gpt-6.1-sol "Reply with OK only."
```

Run this from a trusted project directory. This sends a model request and may incur upstream usage charges. The proxy's Go tests use local test servers and do not verify account access or gateway-specific request requirements.

If Codex shows `Folder access` on every launch, check `projects."/absolute/path".trust_level` in `~/.codex/config.toml`. Opening restricted does not save trust. Start from a trusted project directory, or explicitly mark the intended directory `trusted` after deciding to allow its config, hooks, and rules. This is independent of model routing and command sandbox permissions.

References: [Codex models](https://learn.chatgpt.com/docs/models), [configuration reference](https://developers.openai.com/codex/config-reference).

In legacy mode, the proxy forwards requests to `upstream_base_url` or `GMP_UPSTREAM` and rewrites:

```json
{"model":"codex-auto-review"}
```

to:

```json
{"model":"gpt-5.5"}
```

## Options

```text
-listen             listen address(es), comma-separated, default 127.0.0.1:8787
-upstream           legacy upstream base URL, required when routes and default_route are absent
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
