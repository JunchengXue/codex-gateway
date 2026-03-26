# Codex Gateway

ChatGPT Codex backend → OpenAI-compatible API gateway.

## Install

Download the binary for your platform from [GitHub Releases](https://github.com/JunchengXue/codex-gateway/releases/latest), then:

```bash
# macOS only: the binary is not Apple-signed, remove quarantine to allow execution
xattr -d com.apple.quarantine codex-gateway-*

chmod +x codex-gateway-* && sudo mv codex-gateway-* /usr/local/bin/codex-gateway
```

Or build from source (Go 1.24+): `make build`

## Quick Start

```bash
codex-gateway serve
```

First run opens browser for OAuth login. After that, check connection info:

```bash
cat ~/.codex-gateway/connection-info
```

## Background

```bash
# Start in background
nohup ./codex-gateway serve > /dev/null 2>&1 & echo $! > ~/.codex-gateway/pid

# Stop
kill $(cat ~/.codex-gateway/pid)
```

## Flags

```
./codex-gateway serve
  --listen      Listen address (default :8721)
  --api-key     Downstream API key (auto-generated if omitted)
  --proxy       Outbound proxy (http/https/socks5)
  --log-level   trace, debug, info, warn (default), error
```

| Log level | Output |
|-----------|--------|
| `warn` | Warnings and errors only |
| `debug` | + upstream request/response metadata |
| `trace` | + full upstream request/response body |

## Config (Optional)

`~/.codex-gateway/config.yaml` — all fields optional, CLI flags take precedence:

```yaml
listen: ":8721"
proxy_url: "http://127.0.0.1:7890"
downstream_api_key: "your-key"
```

## Data Directory

`~/.codex-gateway/`

| File | Purpose |
|------|---------|
| `connection-info` | Endpoint, API key, and curl example |
| `oauth-token.json` | OAuth tokens (auto-refreshed) |
| `api-key` | Auto-generated downstream API key |
| `config.yaml` | Optional config overrides |

## Build

```bash
make build          # current platform
make build-all      # linux/macOS × arm64/amd64
```

## License

MIT
