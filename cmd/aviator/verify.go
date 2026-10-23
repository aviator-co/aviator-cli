package main

import (
	"fmt"
	"os"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var verifyFlags struct {
	Repo             string
	Intent           string
	Criteria         []string
	CriteriaFile     string
	WorkingBranch    string
	TargetBranch     string
	Spec             string
	ScenarioGuidance string
	EvaluatorOnly    bool
	Force            bool
	RegenScenarios   string
	Preview          string
	JSON             bool
}

// verifySubmitOnlyFlags are meaningless when triggering a run on an existing
// session, so trigger mode rejects them by name.
var verifySubmitOnlyFlags = []string{
	"repo", "intent", "criteria", "criteria-file",
	"working-branch", "target-branch", "spec", "scenario-guidance",
}

var verifyTriggerOnlyFlags = []string{"evaluator-only", "force", "regen-scenarios"}

// noWorkingBranchWarning covers the one thing a submission gives up without
// --working-branch: with no branch to match on, the session can only reach a PR
// through the "Review: <url>" line in the PR body.
const noWorkingBranchWarning = "no --working-branch given, so this session can only bind to a PR " +
	"through a \"Review: <url>\" line in the PR body.\n" +
	"  Pass --working-branch <branch> to have the PR opened from that branch bind automatically.\n" +
	"  How a PR gets its review: " + docsPRLink

var verifyCmd = &cobra.Command{
	Use:   "verify [r/<number>]",
	Short: "Submit acceptance criteria for verification, or trigger a run on a session",
	Long: "With no argument, create a verification from an intent and a set of\n" +
		"acceptance criteria. Pass --working-branch to tie it to the branch the\n" +
		"work lives on so a PR opened from that branch is verified against these\n" +
		"criteria.\n" +
		"\n" +
		"One review tracks exactly one PR. Stacked or multi-PR work needs\n" +
		"one submission per PR, each with its own --working-branch, intent, and\n" +
		"acceptance criteria. A single submission cannot cover a stack.\n" +
		"\n" +
		"--scenario-guidance is a note on how to verify the change (say, which\n" +
		"seeded user to log in as). It is read until the session's first\n" +
		"scenarios are written, normally on its first run; later plans build on\n" +
		"those scenarios instead of reading it again. It can't be changed.\n" +
		"The evaluator that grades the criteria never sees it.\n" +
		"\n" +
		"With a session ID (aviator verify r/123), trigger a verification run on\n" +
		"that existing session — including its first run. If an equivalent\n" +
		"non-error run already exists for the current head commit and criteria,\n" +
		"the server returns that run instead of starting a new one, so this is\n" +
		"safe to call liberally. Pass --force to start a fresh full run anyway,\n" +
		"or --evaluator-only to re-judge the evidence an earlier run already\n" +
		"collected instead of collecting it again. Evaluator-only is refused once\n" +
		"criteria have been added or reworded, since those need a full run.\n" +
		"\n" +
		"--preview names one of the repo's preview configs for the session's runs\n" +
		"to use instead of the repo default, in either mode. The choice sticks:\n" +
		"later runs on the session, including automatic ones, keep using it until\n" +
		"it is changed again (here or with aviator edit --preview). Without a\n" +
		"choice, the first run saves the repo default by name, so going back to\n" +
		"the repo default later means passing that config's name.\n" +
		"\n" +
		"The scenarios each criterion is checked by (in a preview, or by code\n" +
		"scan) are written once, on the first run, and --force reuses them.\n" +
		"--regen-scenarios \"<feedback>\" writes them again before the run (say,\n" +
		"after changing the preview's skill): every criterion goes back through\n" +
		"planning, with the feedback saying what the new scenarios should do\n" +
		"differently. Once scenarios exist, --scenario-guidance from the\n" +
		"submission isn't read again, so restate anything from it that still\n" +
		"matters. It needs a preview, and is capped at a few per session in any\n" +
		"24 hours (dashboard regenerations count too), except on accounts that\n" +
		"bring their own model key.\n" +
		"\n" +
		"Coding agents: use the /verify-submit skill from\n" +
		pluginRepo + ", which drafts the intent and\n" +
		"criteria with you.\n" +
		"\n" +
		"How a PR gets its review: " + docsPRLink + "\n" +
		"Starting a new run: " + docsResults + "#starting-a-new-run",
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

	guidance := strings.TrimSpace(verifyFlags.ScenarioGuidance)
	if cmd.Flags().Changed("scenario-guidance") && guidance == "" {
		return errors.New("--scenario-guidance can't be blank")
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
		PreviewConfigName:  verifyFlags.Preview,
		ScenarioGuidance:   guidance,
	})
	if err != nil {
		return err
	}

	if verifyFlags.WorkingBranch == "" {
		// stderr, so --json consumers still get a clean object on stdout.
		fmt.Fprintf(os.Stderr, "%s %s\n", colors.Warning("warning:"), noWorkingBranchWarning)
	}
	warnPreviewNotApplied(verifyFlags.Preview, resp.PreviewConfigName)
	if verifyFlags.JSON {
		return printJSON(newVerifySubmitJSON(resp))
	}

	fmt.Printf("%s Verify submission created: %s\n", colors.Success("✓"), resp.URL)
	fmt.Printf("  Review %s\n", formatRunbookID(resp.RunbookNumber))
	if resp.WorkingBranch != "" {
		fmt.Printf("  Working branch: %s\n", resp.WorkingBranch)
	}
	if resp.TargetBranch != "" {
		fmt.Printf("  Target branch:  %s\n", resp.TargetBranch)
	}
	fmt.Printf("  Criteria: %d\n", len(resp.AcceptanceCriteria))
	if name := deref(resp.PreviewConfigName); name != "" {
		fmt.Printf("  Preview config: %s\n", name)
	}
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

	var regenFeedback string
	if cmd.Flags().Changed("regen-scenarios") {
		regenFeedback = strings.TrimSpace(verifyFlags.RegenScenarios)
		if regenFeedback == "" {
			return errors.New("--regen-scenarios needs feedback saying what the new scenarios should do differently")
		}
		if verifyFlags.EvaluatorOnly {
			return errors.New("--regen-scenarios replaces the scenarios, so it can't be combined with --evaluator-only")
		}
	}

	runbookNumber, err := parseRunbookID(arg)
	if err != nil {
		return err
	}
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	req := api.TriggerVerifyRunRequest{
		EvaluatorOnly:      verifyFlags.EvaluatorOnly,
		Force:              verifyFlags.Force,
		PreviewConfigName:  verifyFlags.Preview,
		RegenerateFeedback: regenFeedback,
	}
	resp, err := client.TriggerVerifyRun(cmd.Context(), runbookNumber, req)
	if err != nil {
		return err
	}
	warnPreviewNotApplied(verifyFlags.Preview, resp.PreviewConfigName)
	if verifyFlags.JSON {
		return printJSON(newVerifyRunJSON(resp))
	}

	id := formatRunbookID(resp.RunbookNumber)
	if resp.Deduplicated {
		fmt.Printf("%s %s already has an equivalent run at this commit and criteria version\n",
			colors.Success("✓"), id)
		fmt.Printf("  %s\n", colors.Faint("To run again anyway: aviator verify "+id+" --force"))
	} else {
		fmt.Printf("%s Verification run started for %s\n", colors.Success("✓"), id)
	}
	if resp.Message != "" {
		fmt.Printf("  %s\n", resp.Message)
	}
	if name := deref(resp.PreviewConfigName); name != "" {
		fmt.Printf("  Preview config: %s\n", name)
	}
	fmt.Printf("  Run %d: %s\n", resp.RunID, resp.RunStatus)
	fmt.Printf("  URL: %s\n", resp.URL)
	fmt.Printf("  %s\n", colors.Faint(fmt.Sprintf("Poll with: aviator results %s --run %d", id, resp.RunID)))
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
	f.StringVar(&verifyFlags.ScenarioGuidance, "scenario-guidance", "",
		"note on how to verify the change, read when the first scenarios are written (submit mode only)")
	f.BoolVar(&verifyFlags.EvaluatorOnly, "evaluator-only", false,
		"re-judge the evidence an earlier run collected instead of collecting it again (trigger mode only)")
	f.BoolVar(&verifyFlags.Force, "force", false,
		"start a fresh full run even if an equivalent run already exists (trigger mode only)")
	f.StringVar(&verifyFlags.Preview, "preview", "",
		"preview config the session's runs use instead of the repo default; sticks until changed")
	f.StringVar(&verifyFlags.RegenScenarios, "regen-scenarios", "",
		"rewrite the scenarios before running, with feedback on what to change (trigger mode only)")
	f.BoolVar(&verifyFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")
}

type verifySubmitJSON struct {
	sessionRef
	WorkingBranch     string  `json:"working_branch"`
	TargetBranch      string  `json:"target_branch"`
	CriteriaCount     int     `json:"criteria_count"`
	PreviewConfigName *string `json:"preview_config_name"`
}

func newVerifySubmitJSON(resp *api.SubmitVerifyResponse) verifySubmitJSON {
	return verifySubmitJSON{
		sessionRef:        newSessionRef(resp.RunbookNumber, resp.URL),
		WorkingBranch:     resp.WorkingBranch,
		TargetBranch:      resp.TargetBranch,
		CriteriaCount:     len(resp.AcceptanceCriteria),
		PreviewConfigName: resp.PreviewConfigName,
	}
}

type verifyRunJSON struct {
	sessionRef
	RunID             int     `json:"run_id"`
	RunStatus         string  `json:"run_status"`
	Deduplicated      bool    `json:"deduplicated"`
	Message           string  `json:"message"`
	PreviewConfigName *string `json:"preview_config_name"`
}

func newVerifyRunJSON(resp *api.TriggerVerifyRunResponse) verifyRunJSON {
	return verifyRunJSON{
		sessionRef:        newSessionRef(resp.RunbookNumber, resp.URL),
		RunID:             resp.RunID,
		RunStatus:         resp.RunStatus,
		Deduplicated:      resp.Deduplicated,
		Message:           resp.Message,
		PreviewConfigName: resp.PreviewConfigName,
	}
}
