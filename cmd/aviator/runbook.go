package main

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var runbookFlags struct {
	Repo         string
	Intent       string
	Title        string
	Oneshot      bool
	TargetBranch string
	Spec         string
	Criteria     []string
	CriteriaFile string
	AuthorEmail  string
}

var runbookCmd = &cobra.Command{
	Use:   "runbook",
	Short: "Create a runbook from an intent and an optional spec",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		repo, err := parseRepo(runbookFlags.Repo)
		if err != nil {
			return err
		}
		if runbookFlags.Intent == "" {
			return errors.New("--intent is required")
		}
		criteria, err := collectCriteria(runbookFlags.Criteria, runbookFlags.CriteriaFile)
		if err != nil {
			return err
		}

		var spec *api.SpecFile
		if runbookFlags.Spec != "" {
			spec, err = readSpecFile(runbookFlags.Spec)
			if err != nil {
				return err
			}
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}

		resp, err := client.CreateRunbook(ctx, api.CreateRunbookRequest{
			Repository:         repo,
			Intent:             runbookFlags.Intent,
			Title:              runbookFlags.Title,
			Oneshot:            runbookFlags.Oneshot,
			TargetBranch:       runbookFlags.TargetBranch,
			SpecFile:           spec,
			AcceptanceCriteria: criteria,
			AuthorEmail:        runbookFlags.AuthorEmail,
		})
		if err != nil {
			return err
		}

		fmt.Printf("%s Runbook created: %s\n", colors.Success("✓"), resp.URL)
		fmt.Printf("  Runbook #%d\n", resp.RunbookNumber)
		if resp.Status != "" {
			fmt.Printf("  Status:   %s\n", resp.Status)
		}
		if len(criteria) > 0 {
			fmt.Printf("  Criteria: %d\n", len(criteria))
		}
		return nil
	},
}

func init() {
	registerCriteriaFlags(runbookCmd, &runbookFlags.Criteria, &runbookFlags.CriteriaFile)
	f := runbookCmd.Flags()
	f.StringVar(&runbookFlags.Repo, "repo", "", "GitHub repo as owner/repo")
	f.StringVar(&runbookFlags.Intent, "intent", "", "what the runbook should accomplish and why (detail goes in --spec)")
	f.StringVar(&runbookFlags.Title, "title", "", "optional runbook title")
	f.BoolVar(&runbookFlags.Oneshot, "oneshot", true, "run the runbook in one-shot mode")
	f.StringVar(&runbookFlags.TargetBranch, "target-branch", "", "base branch for the runbook (defaults to repo default)")
	f.StringVar(&runbookFlags.Spec, "spec", "", "path to an optional spec file")
	f.StringVar(&runbookFlags.AuthorEmail, "author-email", "", "attribute the runbook to this user")
	_ = runbookCmd.MarkFlagRequired("repo")
	_ = runbookCmd.MarkFlagRequired("intent")
}
