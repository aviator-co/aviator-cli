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
	JSON         bool
}

var runbookCmd = &cobra.Command{
	Use:   "runbook",
	Short: "Create a runbook from an intent and an optional spec",
	Long: "Create a runbook from an intent and an optional spec. Aviator's agent\n" +
		"implements the spec and opens its own pull request with the result.\n" +
		"\n" +
		"This is not the command for code you wrote yourself. To have your own PR\n" +
		"checked against acceptance criteria, use `aviator verify` instead.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
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

		if runbookFlags.JSON {
			return printJSON(newRunbookCreateJSON(resp, len(criteria)))
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
	f.BoolVar(&runbookFlags.JSON, "json", false, "print the new runbook as a single JSON object instead of the human summary")
	_ = runbookCmd.MarkFlagRequired("repo")
	_ = runbookCmd.MarkFlagRequired("intent")
}

// runbookCreateJSON is the --json shape of a new runbook. It is its own struct
// rather than the raw response so the keys callers parse stay put as the
// response grows.
type runbookCreateJSON struct {
	RunbookNumber int    `json:"runbook_number"`
	RunbookID     string `json:"runbook_id"`
	URL           string `json:"url"`
	Status        string `json:"status"`
	CriteriaCount int    `json:"criteria_count"`
}

func newRunbookCreateJSON(resp *api.CreateRunbookResponse, criteriaCount int) runbookCreateJSON {
	return runbookCreateJSON{
		RunbookNumber: resp.RunbookNumber,
		RunbookID:     formatRunbookID(resp.RunbookNumber),
		URL:           resp.URL,
		Status:        resp.Status,
		CriteriaCount: criteriaCount,
	}
}
