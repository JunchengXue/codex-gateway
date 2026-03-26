# Codex Gateway

Self-hosted gateway that bridges ChatGPT Codex backend to an OpenAI-compatible API. Handles OAuth login, token refresh, and protocol conversion automatically.

## Quick Start

```bash
# Download from GitHub Releases or build from source
make build

# Start (opens browser for OAuth login on first run)
./codex-gateway serve
```

That's it. The gateway listens on `:8721` with an auto-generated API key.

Check the terminal output for the API key file location, or read it directly:

```bash
cat ~/.codex-gateway/api-key
```

## CLI Flags

```
./codex-gateway serve [flags]

  --listen   Listen address (default :8721)
  --api-key  Downstream API key (auto-generated if omitted)
  --proxy    Outbound proxy URL (http/https/socks5)

./codex-gateway auth login [flags]

  --proxy    Outbound proxy URL
```

## Config File (Optional)

Place `~/.codex-gateway/config.yaml` to override defaults. All fields are optional:

```yaml
listen: ":8721"
proxy_url: "http://127.0.0.1:7890"
downstream_api_key: "your-key"
```

CLI flags take precedence over the config file.

## API

All `/v1/*` endpoints require `Authorization: Bearer <api-key>`.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/healthz` | Health check (no auth) |
| GET | `/v1/models` | List models |
| POST | `/v1/chat/completions` | Chat completions |
| POST | `/v1/responses` | Codex responses passthrough |

Example:

```bash
curl http://localhost:8721/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $(cat ~/.codex-gateway/api-key)" \
  -d '{"model":"gpt-5.1-codex","messages":[{"role":"user","content":"Hello!"}]}'
```

## Data Directory

All runtime data lives in `~/.codex-gateway/`:

| File | Purpose |
|------|---------|
| `config.yaml` | Optional config overrides |
| `oauth-token.json` | OAuth tokens (auto-refreshed) |
| `api-key` | Auto-generated downstream API key |

## Build

```bash
make build          # current platform
make build-all      # linux/macOS x arm64/amd64
```

## License

MIT
