package main

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/spf13/cobra"
)

var resultsFlags struct {
	Run  int
	JSON bool
}

// resultsCmd is a preset over `show`: it fetches the acceptance_criteria field
// group (which the server attaches latest_verification to) and renders only
// the verification outcome.
var resultsCmd = &cobra.Command{
	Use:   "results <id>",
	Short: "Show the latest verification results (e.g. aviator results r/123)",
	Long: "Show the latest verification results (e.g. aviator results r/123), or\n" +
		"those of the run --run names. `aviator show` lists a review's runs.\n" +
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
		var run *api.VerificationRun
		if resultsFlags.Run != 0 {
			if run, err = client.GetVerifyRun(cmd.Context(), runbookNumber, resultsFlags.Run); err != nil {
				return err
			}
		}
		if resultsFlags.JSON {
			if run != nil {
				return printJSON(newResultsRunJSON(detail, run))
			}
			return printJSON(newResultsJSON(detail))
		}

		fmt.Print(formatDetailHeader(detail))
		switch {
		case run != nil:
			fmt.Print(formatVerification("Verification", run))
		case detail.LatestVerification != nil:
			fmt.Print(formatVerification("Latest verification", detail.LatestVerification))
		default:
			fmt.Println("  Latest verification: none yet")
		}
		if run == nil {
			fmt.Print(formatWaitingSubmission(detail.WaitingSubmission))
		}
		return nil
	},
}

func init() {
	resultsCmd.Flags().IntVar(&resultsFlags.Run, "run", 0,
		"show run `id` instead of the latest (ids in aviator show); --json returns only that run")
	resultsCmd.Flags().BoolVar(&resultsFlags.JSON, "json", false,
		"print the results as a single JSON object instead of the human summary")
}

type resultsJSON struct {
	sessionRef
	Version *int `json:"version"`
	// TODO: drop once the verify-submit skill reads version instead.
	RunbookVersion     *int                   `json:"runbook_version"`
	LatestVerification *verificationJSON      `json:"latest_verification"`
	WaitingSubmission  *waitingSubmissionJSON `json:"waiting_submission"`
}

type waitingSubmissionJSON struct {
	RunID       int     `json:"run_id"`
	CommitSHA   *string `json:"commit_sha"`
	SubmittedAt string  `json:"submitted_at"`
	Complete    bool    `json:"complete"`
}

func newWaitingSubmissionJSON(w *api.WaitingSubmission) *waitingSubmissionJSON {
	if w == nil {
		return nil
	}
	return &waitingSubmissionJSON{
		RunID:       w.RunID,
		CommitSHA:   w.CommitSHA,
		SubmittedAt: w.SubmittedAt,
		Complete:    w.Complete,
	}
}

func newResultsJSON(d *api.RunbookDetail) resultsJSON {
	return resultsJSON{
		sessionRef:         newSessionRef(d.RunbookNumber, d.URL),
		Version:            d.RunbookVersion,
		RunbookVersion:     d.RunbookVersion,
		LatestVerification: newVerificationJSON(d.LatestVerification),
		WaitingSubmission:  newWaitingSubmissionJSON(d.WaitingSubmission),
	}
}

// formatWaitingSubmission is the line for evidence no run has started yet,
// or nothing when there is none.
func formatWaitingSubmission(w *api.WaitingSubmission) string {
	if w == nil {
		return ""
	}
	commit := "no commit"
	if sha := deref(w.CommitSHA); sha != "" {
		commit = shortSHA(sha)
	}
	state := "all files uploaded, no run started yet"
	if !w.Complete {
		state = "uploads not finished"
	}
	return fmt.Sprintf("  Evidence submitted for %s, %s: aviator scenarios lists it\n", commit, state)
}

type resultsRunJSON struct {
	sessionRef
	Version        *int              `json:"version"`
	RunbookVersion *int              `json:"runbook_version"`
	Run            *verificationJSON `json:"run"`
}

func newResultsRunJSON(d *api.RunbookDetail, run *api.VerificationRun) resultsRunJSON {
	return resultsRunJSON{
		sessionRef:     newSessionRef(d.RunbookNumber, d.URL),
		Version:        d.RunbookVersion,
		RunbookVersion: d.RunbookVersion,
		Run:            newVerificationJSON(run),
	}
}
