package main

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var editFlags struct {
	Criteria        []string
	CriteriaFile    string
	ExpectedVersion int
}

var editCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Replace a runbook/verify session's acceptance criteria (e.g. aviator edit r/123)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		if len(editFlags.Criteria) > 0 && editFlags.CriteriaFile != "" {
			return errors.New("--criteria and --criteria-file cannot be set together")
		}
		criteria, err := collectCriteria(editFlags.Criteria, editFlags.CriteriaFile)
		if err != nil {
			return err
		}
		if len(criteria) == 0 {
			return errors.New("at least one --criteria (or --criteria-file) is required")
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.EditRunbookCriteria(cmd.Context(), runbookNumber, api.EditRunbookCriteriaRequest{
			ExpectedVersion:    editFlags.ExpectedVersion,
			AcceptanceCriteria: criteria,
		})
		if err != nil {
			return err
		}

		fmt.Printf("%s Runbook %s criteria updated\n",
			colors.Success("✓"), formatRunbookID(resp.RunbookNumber))
		if resp.NewVersion != nil {
			fmt.Printf("  Version: %d -> %d\n", editFlags.ExpectedVersion, *resp.NewVersion)
		} else {
			fmt.Printf("  Version: unchanged (%d)\n", editFlags.ExpectedVersion)
		}
		fmt.Printf("  Criteria: %d\n", resp.CriteriaCount)
		if resp.Message != "" {
			fmt.Printf("  %s\n", resp.Message)
		}
		return nil
	},
}

func init() {
	f := editCmd.Flags()
	f.StringArrayVar(&editFlags.Criteria, "criteria", nil, "acceptance criterion (repeatable)")
	f.StringVar(&editFlags.CriteriaFile, "criteria-file", "", "file with one acceptance criterion per line")
	f.IntVar(&editFlags.ExpectedVersion, "expected-version", 0, "runbook version you expect to be editing (guards against stale edits)")
	_ = editCmd.MarkFlagRequired("expected-version")
}
