package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen           string   `yaml:"listen"`
	ProxyURL         string   `yaml:"proxy_url"`
	DownstreamAPIKey string   `yaml:"downstream_api_key"`
	CodexBaseURL     string   `yaml:"codex_base_url"`
	CodexResponses   string   `yaml:"codex_responses_path"`
	TimeoutSeconds   int      `yaml:"timeout_seconds"`
	OAuthClientID    string   `yaml:"oauth_client_id"`
	OAuthAuthorize   string   `yaml:"oauth_authorize_endpoint"`
	OAuthToken       string   `yaml:"oauth_token_endpoint"`
	OAuthRedirectHost string  `yaml:"oauth_redirect_host"`
	OAuthRedirectPort int     `yaml:"oauth_redirect_port"`
	OAuthRedirectPath string  `yaml:"oauth_redirect_path"`
	OAuthOriginator  string   `yaml:"oauth_originator"`
	OAuthScopes      []string `yaml:"oauth_scopes"`
}

func Load() (Config, error) {
	cfg := defaults()

	path, err := defaultPath()
	if err != nil {
		return cfg, nil // no home dir, use defaults
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, nil // file missing, use defaults
	}

	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	applyDefaults(&cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func defaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex-gateway", "config.yaml"), nil
}

func defaults() Config {
	return Config{
		Listen:            ":8721",
		CodexBaseURL:      "https://chatgpt.com",
		CodexResponses:    "/backend-api/codex/responses",
		TimeoutSeconds:    60,
		OAuthClientID:     "app_EMoamEEZ73f0CkXaXp7hrann",
		OAuthAuthorize:    "https://auth.openai.com/oauth/authorize",
		OAuthToken:        "https://auth.openai.com/oauth/token",
		OAuthRedirectHost: "localhost",
		OAuthRedirectPort: 1455,
		OAuthRedirectPath: "/auth/callback",
		OAuthOriginator:   "opencode",
		OAuthScopes:       []string{"openid", "profile", "email", "offline_access"},
	}
}

func applyDefaults(cfg *Config) {
	d := defaults()
	if cfg.Listen == "" {
		cfg.Listen = d.Listen
	}
	if cfg.CodexBaseURL == "" {
		cfg.CodexBaseURL = d.CodexBaseURL
	}
	if cfg.CodexResponses == "" {
		cfg.CodexResponses = d.CodexResponses
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = d.TimeoutSeconds
	}
	if cfg.OAuthClientID == "" {
		cfg.OAuthClientID = d.OAuthClientID
	}
	if cfg.OAuthAuthorize == "" {
		cfg.OAuthAuthorize = d.OAuthAuthorize
	}
	if cfg.OAuthToken == "" {
		cfg.OAuthToken = d.OAuthToken
	}
	if cfg.OAuthRedirectHost == "" {
		cfg.OAuthRedirectHost = d.OAuthRedirectHost
	}
	if cfg.OAuthRedirectHost == "127.0.0.1" {
		cfg.OAuthRedirectHost = "localhost"
	}
	if cfg.OAuthRedirectPort == 0 {
		cfg.OAuthRedirectPort = d.OAuthRedirectPort
	}
	if cfg.OAuthRedirectPath == "" {
		cfg.OAuthRedirectPath = d.OAuthRedirectPath
	}
	if cfg.OAuthOriginator == "" {
		cfg.OAuthOriginator = d.OAuthOriginator
	}
	if len(cfg.OAuthScopes) == 0 {
		cfg.OAuthScopes = d.OAuthScopes
	}
}

func (c Config) Validate() error {
	if proxyURL := strings.TrimSpace(c.ProxyURL); proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || !u.IsAbs() || strings.TrimSpace(u.Hostname()) == "" {
			return fmt.Errorf("invalid proxy_url %q", c.ProxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5", "socks5h":
		default:
			return fmt.Errorf("invalid proxy_url scheme %q (expected http, https, socks5, or socks5h)", u.Scheme)
		}
	}
	return nil
}
