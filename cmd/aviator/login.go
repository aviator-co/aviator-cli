package main

import (
	"fmt"
	"math"
	"os"
	"time"

	"github.com/aviator-co/aviator-cli/internal/auth"
	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Sign in to Aviator in your browser",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		host := config.Av.Aviator.APIHost
		session, err := auth.Login(cmd.Context(), auth.LoginOptions{
			APIHost: host,
			Store:   auth.DefaultStore(),
			Out:     os.Stderr,
		})
		if err != nil {
			return err
		}

		fmt.Printf("%s Signed in to %s\n", colors.Success("✓"), host)
		fmt.Printf("  Session stored in the system keychain\n")
		if !session.Expiry.IsZero() {
			fmt.Printf("  Access token valid for %s (refreshed automatically)\n",
				humanDuration(time.Until(session.Expiry)))
		}
		if origin := config.APITokenOrigin; origin != config.TokenOriginNone {
			fmt.Printf("\n%s %s is set and takes precedence; other commands will keep using it.\n",
				colors.Warning("warning:"), origin)
		}
		return nil
	},
}

// humanDuration renders a token lifetime as a coarse "10 days" / "1 hour".
func humanDuration(d time.Duration) string {
	switch {
	case d >= 36*time.Hour:
		return plural(int(math.Round(d.Hours()/24)), "day")
	case d >= 45*time.Minute:
		return plural(int(math.Round(d.Hours())), "hour")
	default:
		return plural(max(int(math.Round(d.Minutes())), 1), "minute")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
