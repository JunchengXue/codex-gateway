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

type runtimePaths struct {
	Workdir    string
	ConfigPath string
	TokenPath  string
	APIKeyPath string
}

type runtime struct {
	Cfg         config.Config
	Logger      *slog.Logger
	Store       *auth.FileStore
	OAuthClient *oauth.Client
	TokenPath   string
	APIKeyPath  string
}

func bootstrap(workdir, configFile string) (*runtime, error) {
	paths, err := resolveRuntimePaths(workdir, configFile)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(paths.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	logger := newRootLogger(cfg.Logging.Level)

	httpClient, err := newHTTPClient(30*time.Second, cfg.Network.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("build http client: %w", err)
	}

	oauthClient := oauth.NewClient(oauth.Config{
		ClientID:          cfg.OAuth.ClientID,
		ClientSecret:      cfg.OAuth.ClientSecret,
		AuthorizeEndpoint: cfg.OAuth.AuthorizeEndpoint,
		TokenEndpoint:     cfg.OAuth.TokenEndpoint,
		RedirectHost:      cfg.OAuth.RedirectHost,
		RedirectPort:      cfg.OAuth.RedirectPort,
		RedirectPath:      cfg.OAuth.RedirectPath,
		Originator:        cfg.OAuth.Originator,
		Scopes:            cfg.OAuth.Scopes,
	}, oauth.WithHTTPClient(httpClient))

	return &runtime{
		Cfg:         cfg,
		Logger:      logger,
		Store:       auth.NewFileStore(paths.TokenPath),
		OAuthClient: oauthClient,
		TokenPath:   paths.TokenPath,
		APIKeyPath:  paths.APIKeyPath,
	}, nil
}

func resolveRuntimePaths(workdir, configFile string) (runtimePaths, error) {
	absWorkdir, err := filepath.Abs(strings.TrimSpace(workdir))
	if err != nil {
		return runtimePaths{}, fmt.Errorf("resolve workdir: %w", err)
	}

	info, err := os.Stat(absWorkdir)
	if err != nil {
		return runtimePaths{}, fmt.Errorf("stat workdir: %w", err)
	}
	if !info.IsDir() {
		return runtimePaths{}, fmt.Errorf("workdir is not a directory: %s", absWorkdir)
	}

	resolvedConfig := configFile
	if !filepath.IsAbs(configFile) {
		resolvedConfig = filepath.Join(absWorkdir, configFile)
	}
	absConfig, err := filepath.Abs(resolvedConfig)
	if err != nil {
		return runtimePaths{}, fmt.Errorf("resolve config path: %w", err)
	}

	rel, err := filepath.Rel(absWorkdir, absConfig)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return runtimePaths{}, fmt.Errorf("config path is outside of workdir: %s", absConfig)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return runtimePaths{}, fmt.Errorf("resolve home dir: %w", err)
	}
	dataDir := filepath.Join(homeDir, ".codex-gateway")

	return runtimePaths{
		Workdir:    absWorkdir,
		ConfigPath: absConfig,
		TokenPath:  filepath.Join(dataDir, "oauth-token.json"),
		APIKeyPath: filepath.Join(dataDir, "api-key"),
	}, nil
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
