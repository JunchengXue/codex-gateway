package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Collections/Agents/codex-gateway/internal/auth"
	"github.com/Collections/Agents/codex-gateway/internal/config"
	"github.com/Collections/Agents/codex-gateway/internal/oauth"
)

type runtime struct {
	Cfg         config.Config
	Logger      *slog.Logger
	Store       *auth.FileStore
	OAuthClient *oauth.Client
	DataDir     string
}

func bootstrap(proxyURL, logLevel string) (*runtime, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if proxyURL != "" {
		cfg.ProxyURL = proxyURL
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	logger := newRootLogger(logLevel)

	httpClient, err := newHTTPClient(time.Duration(cfg.OAuthTimeoutSeconds)*time.Second, cfg.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("build http client: %w", err)
	}

	oauthClient := oauth.NewClient(oauth.Config{
		ClientID:          cfg.OAuthClientID,
		AuthorizeEndpoint: cfg.OAuthAuthorize,
		TokenEndpoint:     cfg.OAuthToken,
		RedirectHost:      cfg.OAuthRedirectHost,
		RedirectPort:      cfg.OAuthRedirectPort,
		RedirectPath:      cfg.OAuthRedirectPath,
		Originator:        cfg.OAuthOriginator,
		Scopes:            cfg.OAuthScopes,
	}, oauth.WithHTTPClient(httpClient))

	dataDir, err := resolveDataDir()
	if err != nil {
		return nil, err
	}

	return &runtime{
		Cfg:         cfg,
		Logger:      logger,
		Store:       auth.NewFileStore(filepath.Join(dataDir, "oauth-token.json")),
		OAuthClient: oauthClient,
		DataDir:     dataDir,
	}, nil
}

func resolveDataDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(homeDir, ".codex-gateway"), nil
}

func ensureAPIKey(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		if key := strings.TrimSpace(string(b)); key != "" {
			return key, nil
		}
	}

	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	key := "cgw-" + hex.EncodeToString(buf)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create api key dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write api key: %w", err)
	}

	return key, nil
}
