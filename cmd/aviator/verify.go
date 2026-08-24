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
	Repo           string
	Intent         string
	Criteria       []string
	CriteriaFile   string
	WorkingBranch  string
	TargetBranch   string
	Spec           string
	AuthorEmail    string
	EvaluatorOnly  bool
	Force          bool
	RegenScenarios bool
	JSON           bool
}

// verifySubmitOnlyFlags are meaningless when triggering a run on an existing
// session, so trigger mode rejects them by name.
var verifySubmitOnlyFlags = []string{
	"repo", "intent", "criteria", "criteria-file",
	"working-branch", "target-branch", "spec", "author-email",
}

var verifyTriggerOnlyFlags = []string{"evaluator-only", "force", "regen-scenarios"}

// noWorkingBranchWarning covers the one thing a submission gives up without
// --working-branch: with no branch to match on, the session can only reach a PR
// through the "Runbook: <url>" line in the PR body.
const noWorkingBranchWarning = "no --working-branch given, so this session can only bind to a PR " +
	"through a \"Runbook: <url>\" line in the PR body.\n" +
	"  Pass --working-branch <branch> to have the PR opened from that branch bind automatically."

var verifyCmd = &cobra.Command{
	Use:   "verify [r/<number>]",
	Short: "Submit acceptance criteria for verification, or trigger a run on a session",
	Long: "With no argument, create a verification from an intent and a set of\n" +
		"acceptance criteria. Pass --working-branch to tie it to the branch the\n" +
		"work lives on so a PR opened from that branch is verified against these\n" +
		"criteria.\n" +
		"\n" +
		"One verify session tracks exactly one PR. Stacked or multi-PR work needs\n" +
		"one submission per PR, each with its own --working-branch, intent, and\n" +
		"acceptance criteria. A single submission cannot cover a stack.\n" +
		"\n" +
		"With a session ID (aviator verify r/123), trigger a verification run on\n" +
		"that existing session — including its first run. If an equivalent\n" +
		"non-error run already exists for the current head commit and criteria,\n" +
		"the server returns that run instead of starting a new one, so this is\n" +
		"safe to call liberally. Pass --force to start a fresh full run anyway,\n" +
		"or --evaluator-only to re-judge the evidence an earlier run already\n" +
		"collected instead of collecting it again — the cheap path after a\n" +
		"criteria edit; it falls back to a full run when there is nothing to\n" +
		"re-judge.\n" +
		"\n" +
		"The browser interaction scenarios are planned once per session, from the\n" +
		"criteria as they stood at the first run. If you have edited the criteria\n" +
		"to demand interactions that frozen plan never performs, pass\n" +
		"--regen-scenarios to re-plan them from the current criteria before\n" +
		"collecting; it implies a fresh full run and bypasses deduplication like\n" +
		"--force.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return runVerifyTrigger(cmd, args[0])
		}
		return runVerifySubmit(cmd)
	},
}

func runVerifySubmit(cmd *cobra.Command) error {
	ctx := cmd.Context()

	for _, name := range verifyTriggerOnlyFlags {
		if cmd.Flags().Changed(name) {
			return errors.Errorf(
				"--%s only applies when triggering a run on an existing session (aviator verify r/<number>)",
				name)
		}
	}

	if verifyFlags.Repo == "" {
		return errors.New("--repo is required")
	}
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
}

func runVerifyTrigger(cmd *cobra.Command, arg string) error {
	for _, name := range verifySubmitOnlyFlags {
		if cmd.Flags().Changed(name) {
			return errors.Errorf(
				"--%s only applies when submitting a new verification, not when triggering a run on an existing session",
				name)
		}
	}

	if verifyFlags.RegenScenarios && verifyFlags.EvaluatorOnly {
		return errors.New(
			"--regen-scenarios collects fresh evidence, so it cannot be combined with --evaluator-only")
	}

	runbookNumber, err := parseRunbookID(arg)
	if err != nil {
		return err
	}
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	resp, err := client.TriggerVerifyRun(cmd.Context(), runbookNumber, api.TriggerVerifyRunRequest{
		EvaluatorOnly:       verifyFlags.EvaluatorOnly,
		Force:               verifyFlags.Force,
		RegenerateScenarios: verifyFlags.RegenScenarios,
	})
	if err != nil {
		return err
	}
	if verifyFlags.JSON {
		return printJSON(newVerifyRunJSON(resp))
	}

	id := formatRunbookID(resp.RunbookNumber)
	if resp.Deduplicated {
		fmt.Printf("%s %s already has an equivalent run at this commit and criteria version\n",
			colors.Success("✓"), id)
	} else {
		fmt.Printf("%s Verification run started for %s\n", colors.Success("✓"), id)
	}
	if resp.Message != "" {
		fmt.Printf("  %s\n", resp.Message)
	}
	fmt.Printf("  Run status: %s\n", resp.RunStatus)
	fmt.Printf("  URL: %s\n", resp.URL)
	fmt.Printf("  %s\n", colors.Faint("Poll with: aviator results "+id))
	return nil
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
	f.BoolVar(&verifyFlags.EvaluatorOnly, "evaluator-only", false,
		"re-judge the evidence an earlier run collected instead of collecting it again (trigger mode only)")
	f.BoolVar(&verifyFlags.Force, "force", false,
		"start a fresh full run even if an equivalent run already exists (trigger mode only)")
	f.BoolVar(&verifyFlags.RegenScenarios, "regen-scenarios", false,
		"re-plan the browser interaction scenarios from the current criteria before collecting; implies a fresh full run (trigger mode only)")
	f.BoolVar(&verifyFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")
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

// verifyRunJSON is the --json shape of a triggered verification run, its own
// struct for the same key-stability reason as verifySubmitJSON.
type verifyRunJSON struct {
	RunbookNumber int    `json:"runbook_number"`
	RunbookID     string `json:"runbook_id"`
	URL           string `json:"url"`
	RunID         int    `json:"run_id"`
	RunStatus     string `json:"run_status"`
	Deduplicated  bool   `json:"deduplicated"`
	Message       string `json:"message"`
}

func newVerifyRunJSON(resp *api.TriggerVerifyRunResponse) verifyRunJSON {
	return verifyRunJSON{
		RunbookNumber: resp.RunbookNumber,
		RunbookID:     formatRunbookID(resp.RunbookNumber),
		URL:           resp.URL,
		RunID:         resp.RunID,
		RunStatus:     resp.RunStatus,
		Deduplicated:  resp.Deduplicated,
		Message:       resp.Message,
	}
}
