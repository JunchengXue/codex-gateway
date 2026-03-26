package upstream

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

type TokenProvider interface {
	AccessToken(context.Context) (string, error)
	ForceRefresh(context.Context) (string, error)
}

type Client struct {
	baseURL       string
	httpClient    *http.Client
	tokenProvider TokenProvider
	logger        *slog.Logger
}

func NewClient(baseURL string, httpClient *http.Client, tokenProvider TokenProvider, logger *slog.Logger) *Client {
	return &Client{
		baseURL:       strings.TrimRight(baseURL, "/"),
		httpClient:    httpClient,
		tokenProvider: tokenProvider,
		logger:        logger,
	}
}

// Do sends an authenticated request to the upstream. On 401, it force-refreshes
// the token and retries once.
func (c *Client) Do(ctx context.Context, method, path string, body []byte, contentType string, headers map[string]string) (*http.Response, error) {
	token, err := c.tokenProvider.AccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("oauth token unavailable: %w", err)
	}

	resp, err := c.do(ctx, method, path, body, contentType, token, headers)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	// 401 — force refresh and retry once
	resp.Body.Close()
	refreshed, refreshErr := c.tokenProvider.ForceRefresh(ctx)
	if refreshErr != nil {
		return nil, fmt.Errorf("oauth token refresh failed: %w", refreshErr)
	}

	return c.do(ctx, method, path, body, contentType, refreshed, headers)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, contentType, accessToken string, headers map[string]string) (*http.Response, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")

	if accountID := extractAccountID(accessToken); accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}

	for k, v := range headers {
		if k != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}

	c.logger.DebugContext(ctx, "upstream response", "method", method, "path", path, "status", resp.StatusCode)
	return resp, nil
}

func extractAccountID(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	authClaims, ok := claims["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := authClaims["chatgpt_account_id"].(string)
	return strings.TrimSpace(id)
}
