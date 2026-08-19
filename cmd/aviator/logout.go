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
	RunE: func(_ *cobra.Command, _ []string) error {
		host := config.Av.Aviator.APIHost
		err := auth.DefaultStore().Delete(host)
		if errors.Is(err, auth.ErrNoSession) {
			fmt.Printf("No stored session for %s.\n", host)
			return nil
		}
		if err != nil {
			return err
		}

		fmt.Printf("%s Removed the stored session for %s\n", colors.Success("✓"), host)
		fmt.Printf("  Aviator cannot revoke tokens, so any token already issued stays\n")
		fmt.Printf("  valid until it expires\n")
		if shadow := staticTokenDescription(); shadow != "" {
			fmt.Printf("  %s is still set and will be used for API requests\n", shadow)
		}
		return nil
	},
}
