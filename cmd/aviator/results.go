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
	Long: "Show the latest verification results (e.g. aviator results r/123)\n" +
		"\n" +
		"Reading results: " + docsResults + "#reading-results\n" +
		"Fixing a failure: " + docsResults + "#fixing-a-failure",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		detail, err := client.GetRunbookDetail(cmd.Context(), runbookNumber, []string{"acceptance_criteria"})
		if err != nil {
			return err
		}

		// The server only defines latest_verification as part of the
		// acceptance_criteria field group. If that contract moves, fail
		// loudly instead of misreading an absent key as "no runs yet".
		if !detail.LatestVerificationPresent {
			return errors.New(
				"response did not include latest_verification; the server contract may have changed — try 'aviator show'")
		}
		if resultsFlags.JSON {
			return printJSON(newResultsJSON(detail))
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
		"print the results as a single JSON object instead of the human summary")
}

type resultsJSON struct {
	sessionRef
	Version *int `json:"version"`
	// TODO: drop once the verify-submit skill reads version instead.
	RunbookVersion     *int              `json:"runbook_version"`
	LatestVerification *verificationJSON `json:"latest_verification"`
}

func newResultsJSON(d *api.RunbookDetail) resultsJSON {
	return resultsJSON{
		sessionRef:         newSessionRef(d.RunbookNumber, d.URL),
		Version:            d.RunbookVersion,
		RunbookVersion:     d.RunbookVersion,
		LatestVerification: newVerificationJSON(d.LatestVerification),
	}
}
