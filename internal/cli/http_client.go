package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func newHTTPClient(timeout time.Duration, proxyURL string) (*http.Client, error) {
	client := &http.Client{Timeout: timeout}

	if trimmed := strings.TrimSpace(proxyURL); trimmed != "" {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy_url: %w", err)
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(parsed)
		client.Transport = transport
	}

	return client, nil
}
