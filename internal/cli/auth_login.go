package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newAuthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "OAuth authentication commands",
	}
	cmd.AddCommand(newAuthLoginCommand())
	return cmd
}

func newAuthLoginCommand() *cobra.Command {
	var proxyURL string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Run interactive OAuth login",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogin(cmd.Context(), proxyURL)
		},
	}

	cmd.Flags().StringVar(&proxyURL, "proxy", "", "Outbound proxy URL (http/https/socks5)")

	return cmd
}

func runAuthLogin(ctx context.Context, proxyURL string) error {
	rt, err := bootstrap(proxyURL, "info")
	if err != nil {
		return err
	}

	token, err := rt.OAuthClient.AuthenticateWithCallback(ctx, os.Stdout)
	if err != nil {
		return fmt.Errorf("oauth login failed: %w", err)
	}

	if token.RefreshToken == "" {
		return fmt.Errorf("oauth login succeeded but refresh_token is empty")
	}

	if err := rt.Store.Save(token); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Login successful.")
	return nil
}
