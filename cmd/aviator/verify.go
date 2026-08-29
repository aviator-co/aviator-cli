package main

import (
	"fmt"
	"os"

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
	JSON          bool
}

// noWorkingBranchWarning covers the one thing a submission gives up without
// --working-branch: with no branch to match on, the session can only reach a PR
// through the "Runbook: <url>" line in the PR body.
const noWorkingBranchWarning = "no --working-branch given, so this session can only bind to a PR " +
	"through a \"Runbook: <url>\" line in the PR body.\n" +
	"  Pass --working-branch <branch> to have the PR opened from that branch bind automatically."

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Submit an intent and acceptance criteria for verification",
	Long: "Create a verification from an intent and a set of acceptance criteria.\n" +
		"Pass --working-branch to tie it to the branch the work lives on so a PR\n" +
		"opened from that branch is verified against these criteria.\n" +
		"\n" +
		"One verify session tracks exactly one PR. Stacked or multi-PR work needs\n" +
		"one submission per PR, each with its own --working-branch, intent, and\n" +
		"acceptance criteria. A single submission cannot cover a stack.",
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
		})
		if err != nil {
			return err
		}

		if verifyFlags.WorkingBranch == "" {
			// stderr, so --json consumers still get a clean object on stdout.
			fmt.Fprintf(os.Stderr, "%s %s\n", colors.Warning("warning:"), noWorkingBranchWarning)
		}
		if verifyFlags.JSON {
			return printJSON(newVerifySubmitJSON(resp))
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
	f.BoolVar(&verifyFlags.JSON, "json", false, "print the submission as a single JSON object instead of the human summary")
	_ = verifyCmd.MarkFlagRequired("repo")
	_ = verifyCmd.MarkFlagRequired("intent")
}

// verifySubmitJSON is the --json shape of a verify submission. It is its own
// struct rather than the raw response so the keys callers parse stay put as the
// response grows.
type verifySubmitJSON struct {
	RunbookNumber int    `json:"runbook_number"`
	RunbookID     string `json:"runbook_id"`
	URL           string `json:"url"`
	WorkingBranch string `json:"working_branch"`
	TargetBranch  string `json:"target_branch"`
	CriteriaCount int    `json:"criteria_count"`
}

func newVerifySubmitJSON(resp *api.SubmitVerifyResponse) verifySubmitJSON {
	return verifySubmitJSON{
		RunbookNumber: resp.RunbookNumber,
		RunbookID:     formatRunbookID(resp.RunbookNumber),
		URL:           resp.URL,
		WorkingBranch: resp.WorkingBranch,
		TargetBranch:  resp.TargetBranch,
		CriteriaCount: len(resp.AcceptanceCriteria),
	}
}
