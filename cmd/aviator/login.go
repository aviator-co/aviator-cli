package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aviator-co/aviator-cli/internal/auth"
	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Sign in to Aviator in your browser",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runLogin(cmd.Context())
	},
}

func runLogin(ctx context.Context) error {
	host := config.Av.Aviator.APIHost
	// Warn before the browser flow, not after it: a static token wins over
	// whatever the sign-in produces.
	if shadow := staticTokenDescription(); shadow != "" {
		fmt.Fprintf(os.Stderr, "%s %s is set and takes precedence; "+
			"other commands will keep using it.\n\n", colors.Warning("warning:"), shadow)
	}

	if err := auth.Login(ctx, auth.LoginOptions{
		APIHost: host,
		Store:   auth.DefaultStore(),
		Out:     os.Stderr,
	}); err != nil {
		return err
	}

	fmt.Printf("%s Signed in to %s\n", colors.Success("✓"), host)
	fmt.Printf("  Session stored in the system keychain\n")
	return nil
}

// staticTokenDescription names the configured API token that would shadow an
// OAuth session, or "" when there is none.
func staticTokenDescription() string {
	switch {
	case config.Av.Aviator.APITokenFromEnv:
		return "the AVIATOR_API_TOKEN environment variable"
	case config.Av.Aviator.APIToken != "":
		return "the aviator.apiToken setting in your config file"
	default:
		return ""
	}
}
