package main

import (
	"fmt"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/spf13/cobra"
)

var resultsFlags struct {
	JSON bool
}

// resultsCmd is sugar for `show` scoped to the latest verification run (which
// the API attaches to the acceptance_criteria field).
var resultsCmd = &cobra.Command{
	Use:   "results <id>",
	Short: "Show the latest verification results (e.g. aviator results r/123)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		fields := []string{"acceptance_criteria"}
		if resultsFlags.JSON {
			raw, err := client.GetRunbookDetailRaw(cmd.Context(), runbookNumber, fields)
			if err != nil {
				return err
			}
			return printJSON(raw)
		}
		detail, err := client.GetRunbookDetail(cmd.Context(), runbookNumber, fields)
		if err != nil {
			return err
		}
		fmt.Print(formatDetailHeader(detail))
		if detail.LatestVerification != nil {
			fmt.Print(formatVerification(detail.LatestVerification))
		} else {
			fmt.Println("  Latest verification: none yet")
		}
		return nil
	},
}

func init() {
	resultsCmd.Flags().BoolVar(&resultsFlags.JSON, "json", false, "print the raw response as pretty JSON")
}
