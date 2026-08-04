package main

import (
	"fmt"

	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the aviator CLI version",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println(config.Version)
		return nil
	},
}
