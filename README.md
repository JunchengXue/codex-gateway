# Codex Gateway

OpenAI OAuth to OpenAI-compatible API gateway. Bridges the ChatGPT Codex API with a standard OpenAI chat completions interface, handling OAuth 2.0 (PKCE) authentication, token management, and request/response conversion automatically.

## Features

- OpenAI-compatible `/v1/chat/completions` and `/v1/models` endpoints
- OAuth 2.0 PKCE authentication with automatic token refresh
- Streaming (SSE) support
- Tool/function calling support
- HTTP/HTTPS/SOCKS5 proxy support
- Cross-platform builds (macOS, Linux)

## Quick Start

### 1. Get the binary

**Option A: Download pre-built binary**

Download from [GitHub Releases](https://github.com/Collections/Agents/codex-gateway/releases) for your platform:

| Platform | Binary |
|----------|--------|
| macOS (Apple Silicon) | `codex-gateway-darwin-arm64` |
| macOS (Intel) | `codex-gateway-darwin-amd64` |
| Linux (ARM64) | `codex-gateway-linux-arm64` |
| Linux (x86_64) | `codex-gateway-linux-amd64` |

```bash
chmod +x codex-gateway-*
```

**Option B: Build from source**

Requirements: Go 1.24+

```bash
git clone https://github.com/Collections/Agents/codex-gateway.git
cd codex-gateway
make build
```

Cross-compile all platforms:

```bash
make build-all
```

### 2. Configure

Copy the example config and edit it:

```bash
cp config.example.yaml config.yaml
```

The only required change is setting `auth.downstream_api_key` to a secret key of your choice. This key is what your clients will use to authenticate with the gateway.

```yaml
server:
  listen: ":8080"

auth:
  downstream_api_key: "your-secret-key-here"  # REQUIRED: clients use this as Bearer token

logging:
  level: "info"  # debug, info, warn, error

network:
  proxy_url: ""  # e.g. http://127.0.0.1:7890 or socks5://127.0.0.1:1080

oauth:
  client_id: "app_EMoamEEZ73f0CkXaXp7hrann"
  authorize_endpoint: "https://auth.openai.com/oauth/authorize"
  token_endpoint: "https://auth.openai.com/oauth/token"
  redirect_host: "localhost"
  redirect_port: 1455
  redirect_path: "/auth/callback"
  originator: "opencode"
  scopes:
    - "openid"
    - "profile"
    - "email"
    - "offline_access"

codex:
  base_url: "https://chatgpt.com"
  responses_path: "/backend-api/codex/responses"
  timeout_seconds: 60
```

### 3. Login

Run the OAuth login flow to obtain credentials. This opens a browser for you to log in with your OpenAI account:

```bash
./codex-gateway auth login
```

The token is saved to `oauth-token.json` in the working directory and will be refreshed automatically.

### 4. Start the gateway

```bash
./codex-gateway serve
```

The gateway is now listening on the configured address (default `:8080`).

## Usage

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Health check |
| `GET` | `/v1/models` | List available models |
| `POST` | `/v1/chat/completions` | Chat completions (OpenAI format) |
| `POST` | `/v1/responses` | Direct Codex responses passthrough |

All `/v1/*` endpoints require the `Authorization` header:

```
Authorization: Bearer <your-downstream-api-key>
```

### Example: Chat Completion

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-secret-key-here" \
  -d '{
    "model": "gpt-5.1-codex",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

### Example: Streaming

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-secret-key-here" \
  -d '{
    "model": "gpt-5.1-codex",
    "stream": true,
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

### Available Models

- `gpt-5.3-codex`
- `gpt-5.2-codex`
- `gpt-5.1-codex`
- `gpt-5.1-codex-mini`
- `gpt-5.1-codex-max`

## CLI Reference

```
codex-gateway [command]

Commands:
  serve        Start the HTTP gateway server
  auth login   Run OAuth login flow

Flags:
  --workdir string   Runtime working directory (default ".")
  --config string    Config file path, relative to workdir (default "config.yaml")
```

## Proxy Configuration

Set `network.proxy_url` in `config.yaml` for environments that require a proxy for upstream requests:

```yaml
network:
  proxy_url: "http://127.0.0.1:7890"       # HTTP proxy
  # proxy_url: "socks5://127.0.0.1:1080"   # SOCKS5 proxy
```

Supported schemes: `http`, `https`, `socks5`, `socks5h`.

## License

MIT
