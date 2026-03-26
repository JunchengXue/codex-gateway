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
	var workdir string
	var configFile string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Run interactive OAuth login",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogin(cmd.Context(), workdir, configFile)
		},
	}

	cmd.Flags().StringVar(&workdir, "workdir", ".", "Runtime working directory")
	cmd.Flags().StringVar(&configFile, "config", "config.yaml", "Config file path (must be inside workdir)")

	return cmd
}

func runAuthLogin(ctx context.Context, workdir, configFile string) error {
	rt, err := bootstrap(workdir, configFile)
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

	fmt.Fprintf(os.Stdout, "Login successful. Token saved to %s\n", rt.TokenPath)
	return nil
}
