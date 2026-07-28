package main

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/auth"
	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove the stored Aviator session",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		host := config.Av.Aviator.APIHost
		err := auth.DefaultStore().Delete(host)
		if errors.Is(err, auth.ErrNoSession) {
			fmt.Printf("No stored session for %s.\n", host)
			return nil
		}
		if err != nil {
			return err
		}

		fmt.Printf("%s Signed out of %s\n", colors.Success("✓"), host)
		fmt.Printf("  Session removed from the system keychain\n")
		if origin := config.APITokenOrigin; origin != config.TokenOriginNone {
			fmt.Printf("  %s is still set and will be used for API requests\n", origin)
		}
		return nil
	},
}
