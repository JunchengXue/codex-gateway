package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Collections/Agents/codex-gateway/internal/auth"
	"github.com/Collections/Agents/codex-gateway/internal/server"
	"github.com/Collections/Agents/codex-gateway/internal/upstream"
	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	var workdir string
	var configFile string
	var listen string
	var apiKey string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run gateway HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd.Context(), workdir, configFile, listen, apiKey)
		},
	}

	cmd.Flags().StringVar(&workdir, "workdir", ".", "Runtime working directory")
	cmd.Flags().StringVar(&configFile, "config", "config.yaml", "Config file path (must be inside workdir)")
	cmd.Flags().StringVar(&listen, "listen", "", "Listen address (overrides config, default :8721)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Downstream API key (overrides config)")

	return cmd
}

func runServe(ctx context.Context, workdir, configFile, listen, apiKey string) error {
	rt, err := bootstrap(workdir, configFile)
	if err != nil {
		return err
	}

	if listen != "" {
		rt.Cfg.Server.Listen = listen
	}
	if apiKey != "" {
		rt.Cfg.Auth.DownstreamAPIKey = apiKey
	}
	if rt.Cfg.Auth.DownstreamAPIKey == "" {
		key, err := ensureAPIKey(rt.APIKeyPath)
		if err != nil {
			return fmt.Errorf("ensure api key: %w", err)
		}
		rt.Cfg.Auth.DownstreamAPIKey = key
		rt.Logger.InfoContext(ctx, "using auto-generated api key", "path", rt.APIKeyPath)
	}

	if err := ensureValidToken(ctx, rt); err != nil {
		return err
	}

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

	upstreamHTTPClient, err := newHTTPClient(time.Duration(rt.Cfg.Codex.TimeoutSeconds)*time.Second, rt.Cfg.Network.ProxyURL)
	if err != nil {
		return fmt.Errorf("build upstream http client: %w", err)
	}

	upstreamClient := upstream.NewClient(rt.Cfg.Codex.BaseURL, upstreamHTTPClient, manager, rt.Logger)

	handler := server.New(server.Dependencies{
		FixedAPIKey:    rt.Cfg.Auth.DownstreamAPIKey,
		ResponsesPath:  rt.Cfg.Codex.ResponsesPath,
		Originator:     rt.Cfg.OAuth.Originator,
		Logger:         rt.Logger,
		UpstreamClient: upstreamClient,
	})

	httpServer := &http.Server{
		Addr:    rt.Cfg.Server.Listen,
		Handler: handler,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	rt.Logger.InfoContext(ctx, "gateway server starting", "listen", rt.Cfg.Server.Listen)
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
		rt.Logger.InfoContext(ctx, "existing oauth token found")
		return nil
	}

	rt.Logger.InfoContext(ctx, "no valid oauth token, starting interactive login")
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

	fmt.Fprintf(os.Stdout, "Login successful. Token saved to %s\n", rt.TokenPath)
	return nil
}
