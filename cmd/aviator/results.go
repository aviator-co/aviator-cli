package main

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/spf13/cobra"
)

var resultsFlags struct {
	JSON bool
}

// resultsCmd is a preset over `show`: it fetches the acceptance_criteria field
// group (which the server attaches latest_verification to) and renders only
// the verification outcome.
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
		raw, detail, err := client.GetRunbookDetail(cmd.Context(), runbookNumber, []string{"acceptance_criteria"})
		if err != nil {
			return err
		}
		if resultsFlags.JSON {
			return printJSON(raw)
		}

		// The server only defines latest_verification as part of the
		// acceptance_criteria field group. If that contract moves, fail
		// loudly instead of misreading an absent key as "no runs yet".
		if !detail.LatestVerificationPresent {
			return errors.New(
				"response did not include latest_verification; the server contract may have changed — try 'aviator show' or --json")
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
	resultsCmd.Flags().BoolVar(&resultsFlags.JSON, "json", false,
		"print the raw response as pretty JSON (the acceptance_criteria fetch the results are attached to)")
}
