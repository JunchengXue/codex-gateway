package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Auth    AuthConfig    `yaml:"auth"`
	Logging LoggingConfig `yaml:"logging"`
	OAuth   OAuthConfig   `yaml:"oauth"`
	Network NetworkConfig `yaml:"network"`
	Codex   CodexConfig   `yaml:"codex"`
}

type ServerConfig struct {
	Listen string `yaml:"listen"`
}

type AuthConfig struct {
	DownstreamAPIKey string `yaml:"downstream_api_key"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type OAuthConfig struct {
	ClientID          string   `yaml:"client_id"`
	ClientSecret      string   `yaml:"client_secret"`
	AuthorizeEndpoint string   `yaml:"authorize_endpoint"`
	TokenEndpoint     string   `yaml:"token_endpoint"`
	RedirectHost      string   `yaml:"redirect_host"`
	RedirectPort      int      `yaml:"redirect_port"`
	RedirectPath      string   `yaml:"redirect_path"`
	Originator        string   `yaml:"originator"`
	Scopes            []string `yaml:"scopes"`
}

type NetworkConfig struct {
	ProxyURL string `yaml:"proxy_url"`
}

type CodexConfig struct {
	BaseURL        string `yaml:"base_url"`
	ResponsesPath  string `yaml:"responses_path"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse yaml: %w", err)
	}

	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	defaults := map[*string]string{
		&cfg.Server.Listen:           ":8080",
		&cfg.Logging.Level:           "info",
		&cfg.Codex.BaseURL:           "https://chatgpt.com",
		&cfg.Codex.ResponsesPath:     "/backend-api/codex/responses",
		&cfg.OAuth.ClientID:          "app_EMoamEEZ73f0CkXaXp7hrann",
		&cfg.OAuth.AuthorizeEndpoint: "https://auth.openai.com/oauth/authorize",
		&cfg.OAuth.TokenEndpoint:     "https://auth.openai.com/oauth/token",
		&cfg.OAuth.RedirectHost:      "localhost",
		&cfg.OAuth.RedirectPath:      "/auth/callback",
		&cfg.OAuth.Originator:        "opencode",
	}
	for ptr, val := range defaults {
		if *ptr == "" {
			*ptr = val
		}
	}

	if cfg.Codex.TimeoutSeconds == 0 {
		cfg.Codex.TimeoutSeconds = 60
	}
	if cfg.OAuth.RedirectPort == 0 {
		cfg.OAuth.RedirectPort = 1455
	}
	if cfg.OAuth.RedirectHost == "127.0.0.1" {
		cfg.OAuth.RedirectHost = "localhost"
	}
	if len(cfg.OAuth.Scopes) == 0 {
		cfg.OAuth.Scopes = []string{"openid", "profile", "email", "offline_access"}
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Auth.DownstreamAPIKey) == "" {
		return fmt.Errorf("missing required field: auth.downstream_api_key")
	}

	if proxyURL := strings.TrimSpace(c.Network.ProxyURL); proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || !u.IsAbs() || strings.TrimSpace(u.Hostname()) == "" {
			return fmt.Errorf("invalid network.proxy_url %q", c.Network.ProxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5", "socks5h":
		default:
			return fmt.Errorf("invalid network.proxy_url scheme %q (expected http, https, socks5, or socks5h)", u.Scheme)
		}
	}

	return nil
}
