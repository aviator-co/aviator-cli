package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var dismissFlags struct {
	Keys             []string
	CriteriaJSON     string
	CriteriaJSONFile string
	JSON             bool
}

var dismissCmd = &cobra.Command{
	Use:   "dismiss <id>",
	Short: "Clear criteria off a review: delete task criteria, waive invariants (e.g. aviator dismiss r/123)",
	Long: "Delete task criteria by key, or waive baseline invariants by id, so they\n" +
		"no longer gate the review. `aviator show` and `aviator results` print\n" +
		"each criterion's [key ...] or [invariant ...] handle.\n" +
		"\n" +
		"--criteria-json takes a JSON array of entries, each either\n" +
		"  {\"stable_key\": \"...\"}\n" +
		"or\n" +
		"  {\"baseline_invariant_id\": 42, \"category\": \"accepted_risk\", \"justification\": \"...\"}\n" +
		"where category is false_positive, doesnt_apply, accepted_risk, or\n" +
		"fix_in_followup. --key <key> is shorthand for a stable_key entry.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		dismissals, err := buildDismissals(
			dismissFlags.Keys, dismissFlags.CriteriaJSON, dismissFlags.CriteriaJSONFile,
		)
		if err != nil {
			return err
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.DismissCriteria(cmd.Context(), runbookNumber, api.DismissCriteriaRequest{
			Criteria: dismissals,
		})
		if err != nil {
			return err
		}
		if dismissFlags.JSON {
			return printJSON(newDismissJSON(resp))
		}

		fmt.Printf("%s %s: %d deleted, %d waived%s\n", colors.Success("✓"),
			formatRunbookID(resp.RunbookNumber), resp.Deleted, resp.Waived, formatVersion(resp.NewVersion))
		if resp.AlreadyDeleted > 0 {
			fmt.Printf("  %d already deleted\n", resp.AlreadyDeleted)
		}
		return nil
	},
}

func buildDismissals(keys []string, inlineJSON, jsonFile string) ([]api.Dismissal, error) {
	data, err := collectBody(inlineJSON, jsonFile)
	if err != nil {
		return nil, err
	}

	var dismissals []api.Dismissal
	if data != "" {
		dec := json.NewDecoder(strings.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&dismissals); err != nil {
			return nil, errors.Wrap(err, "invalid --criteria-json")
		}
	}
	for _, key := range keys {
		dismissals = append(dismissals, api.Dismissal{StableKey: key})
	}
	if len(dismissals) == 0 {
		return nil, errors.New("pass at least one --key or a --criteria-json entry")
	}
	return dismissals, nil
}

func init() {
	f := dismissCmd.Flags()
	f.StringArrayVar(&dismissFlags.Keys, "key", nil, "key of a task criterion to delete (repeatable)")
	f.StringVar(&dismissFlags.CriteriaJSON, "criteria-json", "", "JSON array of criteria to delete and invariants to waive")
	f.StringVar(&dismissFlags.CriteriaJSONFile, "criteria-json-file", "", "read the --criteria-json array from a file")
	dismissCmd.MarkFlagsMutuallyExclusive("criteria-json", "criteria-json-file")
	f.BoolVar(&dismissFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")
}

type dismissJSON struct {
	ID             string `json:"id"`
	Version        *int   `json:"version"`
	Deleted        int    `json:"deleted"`
	AlreadyDeleted int    `json:"already_deleted"`
	Waived         int    `json:"waived"`
	CriteriaCount  int    `json:"criteria_count"`
}

func newDismissJSON(resp *api.DismissCriteriaResponse) dismissJSON {
	return dismissJSON{
		ID:             formatRunbookID(resp.RunbookNumber),
		Version:        resp.NewVersion,
		Deleted:        resp.Deleted,
		AlreadyDeleted: resp.AlreadyDeleted,
		Waived:         resp.Waived,
		CriteriaCount:  resp.CriteriaCount,
	}
}
