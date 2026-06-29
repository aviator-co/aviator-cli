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
	Prompt       string
	Title        string
	Oneshot      bool
	TargetBranch string
	AuthorEmail  string
}

var runbookCmd = &cobra.Command{
	Use:   "runbook",
	Short: "Create a runbook from a prompt",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		repo, err := parseRepo(runbookFlags.Repo)
		if err != nil {
			return err
		}
		if runbookFlags.Prompt == "" {
			return errors.New("--prompt is required")
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}

		resp, err := client.CreateRunbook(ctx, api.CreateRunbookRequest{
			Repository:   repo,
			Prompt:       runbookFlags.Prompt,
			Title:        runbookFlags.Title,
			Oneshot:      runbookFlags.Oneshot,
			TargetBranch: runbookFlags.TargetBranch,
			AuthorEmail:  runbookFlags.AuthorEmail,
		})
		if err != nil {
			return err
		}

		fmt.Printf("%s Runbook created: %s\n", colors.Success("✓"), resp.URL)
		fmt.Printf("  Runbook #%d\n", resp.RunbookNumber)
		if resp.Status != "" {
			fmt.Printf("  Status:   %s\n", resp.Status)
		}
		return nil
	},
}

func init() {
	f := runbookCmd.Flags()
	f.StringVar(&runbookFlags.Repo, "repo", "", "GitHub repo as owner/repo")
	f.StringVar(&runbookFlags.Prompt, "prompt", "", "the task description / prompt for the runbook")
	f.StringVar(&runbookFlags.Title, "title", "", "optional runbook title")
	f.BoolVar(&runbookFlags.Oneshot, "oneshot", true, "run the runbook in one-shot mode")
	f.StringVar(&runbookFlags.TargetBranch, "target-branch", "", "base branch for the runbook (defaults to repo default)")
	f.StringVar(&runbookFlags.AuthorEmail, "author-email", "", "attribute the runbook to this user")
	_ = runbookCmd.MarkFlagRequired("repo")
	_ = runbookCmd.MarkFlagRequired("prompt")
}
