package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Collections/Agents/codex-gateway/internal/auth"
	"github.com/Collections/Agents/codex-gateway/internal/server"
	"github.com/Collections/Agents/codex-gateway/internal/upstream"
	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	var listen string
	var apiKey string
	var proxyURL string
	var logLevel string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run gateway HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd.Context(), listen, apiKey, proxyURL, logLevel)
		},
	}

	cmd.Flags().StringVar(&listen, "listen", "", "Listen address (default :8721)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Downstream API key")
	cmd.Flags().StringVar(&proxyURL, "proxy", "", "Outbound proxy URL (http/https/socks5)")
	cmd.Flags().StringVar(&logLevel, "log-level", "warn", "Log level: trace, debug, info, warn, error")

	return cmd
}

func runServe(ctx context.Context, listen, apiKey, proxyURL, logLevel string) error {
	rt, err := bootstrap(proxyURL, logLevel)
	if err != nil {
		return err
	}

	if listen != "" {
		rt.Cfg.Listen = listen
	}

	if apiKey != "" {
		rt.Cfg.DownstreamAPIKey = apiKey
	}
	if rt.Cfg.DownstreamAPIKey == "" {
		keyPath := filepath.Join(rt.DataDir, "api-key")
		key, err := ensureAPIKey(keyPath)
		if err != nil {
			return fmt.Errorf("ensure api key: %w", err)
		}
		rt.Cfg.DownstreamAPIKey = key
	}

	if err := ensureValidToken(ctx, rt); err != nil {
		return err
	}

	if err := writeConnectionInfo(rt.DataDir, rt.Cfg.Listen, rt.Cfg.DownstreamAPIKey); err != nil {
		rt.Logger.WarnContext(ctx, "failed to write connection info", "error", err)
	}
	infoPath := filepath.Join(rt.DataDir, "connection-info")
	fmt.Fprintf(os.Stderr, "\033[36mConnection info: %s\033[0m\n", infoPath)

	manager := auth.NewManager(rt.Store, func(ctx context.Context, in auth.Token) (auth.Token, error) {
		refreshed, err := rt.OAuthClient.RefreshToken(ctx, in.RefreshToken)
		if err != nil {
			return auth.Token{}, err
		}
		if refreshed.RefreshToken == "" {
			refreshed.RefreshToken = in.RefreshToken
		}
		return refreshed, nil
	}, auth.WithLogger(rt.Logger))

	upstreamHTTPClient, err := newHTTPClient(time.Duration(rt.Cfg.TimeoutSeconds)*time.Second, rt.Cfg.ProxyURL)
	if err != nil {
		return fmt.Errorf("build upstream http client: %w", err)
	}

	upstreamClient := upstream.NewClient(rt.Cfg.CodexBaseURL, upstreamHTTPClient, manager, rt.Logger)

	handler := server.New(server.Dependencies{
		FixedAPIKey:   rt.Cfg.DownstreamAPIKey,
		ResponsesPath: rt.Cfg.CodexResponses,
		Originator:    rt.Cfg.OAuthOriginator,
		Logger:        rt.Logger,
		UpstreamClient: upstreamClient,
	})

	httpServer := &http.Server{
		Addr:    rt.Cfg.Listen,
		Handler: handler,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	rt.Logger.InfoContext(ctx, "gateway server starting", "listen", rt.Cfg.Listen)
	go func() { errCh <- httpServer.ListenAndServe() }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

func ensureValidToken(ctx context.Context, rt *runtime) error {
	token, err := rt.Store.Load()
	if err == nil && token.AccessToken != "" && token.RefreshToken != "" {
		fmt.Fprintf(os.Stderr, "\033[32mReusing existing OAuth token (expires %s)\033[0m\n", token.ExpiresAt.Format("2006-01-02 15:04:05"))
		return nil
	}

	fmt.Fprintf(os.Stderr, "\033[33mNo valid OAuth token found, starting interactive login...\033[0m\n")
	token, err = rt.OAuthClient.AuthenticateWithCallback(ctx, os.Stdout)
	if err != nil {
		return fmt.Errorf("oauth login failed: %w", err)
	}

	if token.RefreshToken == "" {
		return fmt.Errorf("oauth login succeeded but refresh_token is empty")
	}

	if err := rt.Store.Save(token); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\033[32mLogin successful.\033[0m\n")
	return nil
}

func writeConnectionInfo(dataDir, listen, apiKey string) error {
	host := "http://localhost" + listen

	content := fmt.Sprintf(`Endpoint : %s
API Key  : %s

Example:

  curl %s/v1/chat/completions \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer %s" \
    -d '{"model":"gpt-5.1-codex","messages":[{"role":"user","content":"Hello"}]}'
`, host, apiKey, host, apiKey)

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, "connection-info"), []byte(content), 0o600)
}
