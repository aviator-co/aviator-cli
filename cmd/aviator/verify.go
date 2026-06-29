package main

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var verifyFlags struct {
	Repo         string
	Intent       string
	Criteria     []string
	CriteriaFile string
	Branch       string
	Spec         string
	AuthorEmail  string
}

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Submit an intent and acceptance criteria for verification",
	Long: "Create a verification from an intent and a set of acceptance criteria.\n" +
		"Pass --branch to tie it to the branch the work lives on so a PR opened\n" +
		"from that branch is verified against these criteria.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
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
			BranchName:         verifyFlags.Branch,
			SpecFile:           spec,
			AuthorEmail:        verifyFlags.AuthorEmail,
		})
		if err != nil {
			return err
		}

		fmt.Printf("%s Verify submission created: %s\n", colors.Success("✓"), resp.URL)
		fmt.Printf("  Runbook #%d\n", resp.RunbookNumber)
		if resp.BranchName != "" {
			fmt.Printf("  Branch:   %s\n", resp.BranchName)
		}
		fmt.Printf("  Criteria: %d\n", len(resp.AcceptanceCriteria))
		return nil
	},
}

func init() {
	f := verifyCmd.Flags()
	f.StringVar(&verifyFlags.Repo, "repo", "", "GitHub repo as owner/repo")
	f.StringVar(&verifyFlags.Intent, "intent", "", "short description of the change to verify")
	f.StringArrayVar(&verifyFlags.Criteria, "criteria", nil, "acceptance criterion (repeatable)")
	f.StringVar(&verifyFlags.CriteriaFile, "criteria-file", "", "file with one acceptance criterion per line")
	f.StringVar(&verifyFlags.Branch, "branch", "", "branch the work lives on (optional)")
	f.StringVar(&verifyFlags.Spec, "spec", "", "path to an optional spec file")
	f.StringVar(&verifyFlags.AuthorEmail, "author-email", "", "attribute the submission to this user")
	_ = verifyCmd.MarkFlagRequired("repo")
	_ = verifyCmd.MarkFlagRequired("intent")
}
