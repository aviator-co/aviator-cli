package main

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var editFlags struct {
	Intent          string
	Criteria        []string
	CriteriaFile    string
	ExpectedVersion int
	JSON            bool
}

var editCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Update a review or runbook session's intent or acceptance criteria (e.g. aviator edit r/123)",
	Long: "Update a session's intent, replace its acceptance criteria, or both.\n" +
		"\n" +
		"Replacing criteria needs --expected-version, the version `aviator show`\n" +
		"prints; a stale version is refused.\n" +
		"Edits don't start a verification run: follow a criteria edit with\n" +
		"`aviator verify r/<number>`.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		criteria, err := collectCriteria(editFlags.Criteria, editFlags.CriteriaFile)
		if err != nil {
			return err
		}
		if editFlags.Intent == "" && len(criteria) == 0 {
			return errors.New("pass --intent, --criteria (or --criteria-file), or both")
		}
		if len(criteria) > 0 && !cmd.Flags().Changed("expected-version") {
			return errors.New("--expected-version is required when replacing criteria")
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.EditVerify(cmd.Context(), runbookNumber, api.EditVerifyRequest{
			Intent:             editFlags.Intent,
			AcceptanceCriteria: criteria,
			ExpectedVersion:    editFlags.ExpectedVersion,
		})
		if err != nil {
			return err
		}
		if editFlags.JSON {
			return printJSON(newEditJSON(resp))
		}

		id := formatRunbookID(resp.RunbookNumber)
		fmt.Printf("%s %s updated%s\n", colors.Success("✓"), id, formatVersion(resp.Version))
		if len(criteria) > 0 {
			fmt.Printf("  %s\n", colors.Faint("Verify with: aviator verify "+id))
		}
		return nil
	},
}

func init() {
	registerCriteriaFlags(editCmd, &editFlags.Criteria, &editFlags.CriteriaFile)
	f := editCmd.Flags()
	f.StringVar(&editFlags.Intent, "intent", "", "new intent for the session")
	f.IntVar(&editFlags.ExpectedVersion, "expected-version", 0,
		"session version you expect to be editing (required with --criteria; guards against stale edits)")
	f.BoolVar(&editFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")
}

type editJSON struct {
	sessionRef
	Intent  string `json:"intent"`
	Version *int   `json:"version"`
}

func newEditJSON(resp *api.VerifySession) editJSON {
	return editJSON{
		sessionRef: newSessionRef(resp.RunbookNumber, resp.URL),
		Intent:     resp.Intent,
		Version:    resp.Version,
	}
}
