package main

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var verifyFlags struct {
	Repo          string
	Intent        string
	Criteria      []string
	CriteriaFile  string
	WorkingBranch string
	TargetBranch  string
	Spec          string
	AuthorEmail   string
}

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Submit an intent and acceptance criteria for verification",
	Long: "Create a verification from an intent and a set of acceptance criteria.\n" +
		"Pass --working-branch to tie it to the branch the work lives on so a PR\n" +
		"opened from that branch is verified against these criteria.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		repo, err := parseRepo(verifyFlags.Repo)
		if err != nil {
			return err
		}
		if verifyFlags.Intent == "" {
			return errors.New("--intent is required")
		}
		criteria, err := collectCriteria(verifyFlags.Criteria, verifyFlags.CriteriaFile)
		if err != nil {
			return err
		}
		if len(criteria) == 0 {
			return errors.New("at least one --criteria (or --criteria-file) is required")
		}

		var spec *api.SpecFile
		if verifyFlags.Spec != "" {
			spec, err = readSpecFile(verifyFlags.Spec)
			if err != nil {
				return err
			}
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}

		resp, err := client.SubmitVerify(ctx, api.SubmitVerifyRequest{
			Repository:         repo,
			Intent:             verifyFlags.Intent,
			AcceptanceCriteria: criteria,
			WorkingBranch:      verifyFlags.WorkingBranch,
			TargetBranch:       verifyFlags.TargetBranch,
			SpecFile:           spec,
			AuthorEmail:        verifyFlags.AuthorEmail,
		})
		if err != nil {
			return err
		}

		fmt.Printf("%s Verify submission created: %s\n", colors.Success("✓"), resp.URL)
		fmt.Printf("  Runbook #%d\n", resp.RunbookNumber)
		if resp.WorkingBranch != "" {
			fmt.Printf("  Working branch: %s\n", resp.WorkingBranch)
		}
		if resp.TargetBranch != "" {
			fmt.Printf("  Target branch:  %s\n", resp.TargetBranch)
		}
		fmt.Printf("  Criteria: %d\n", len(resp.AcceptanceCriteria))
		return nil
	},
}

func init() {
	registerCriteriaFlags(verifyCmd, &verifyFlags.Criteria, &verifyFlags.CriteriaFile)
	f := verifyCmd.Flags()
	f.StringVar(&verifyFlags.Repo, "repo", "", "GitHub repo as owner/repo")
	f.StringVar(&verifyFlags.Intent, "intent", "", "short description of the change to verify")
	f.StringVar(&verifyFlags.WorkingBranch, "working-branch", "", "branch the work lives on (optional)")
	f.StringVar(&verifyFlags.TargetBranch, "target-branch", "", "base branch to verify against (defaults to the repo default)")
	f.StringVar(&verifyFlags.Spec, "spec", "", "path to an optional spec file")
	f.StringVar(&verifyFlags.AuthorEmail, "author-email", "", "attribute the submission to this user")
	_ = verifyCmd.MarkFlagRequired("repo")
	_ = verifyCmd.MarkFlagRequired("intent")
}
