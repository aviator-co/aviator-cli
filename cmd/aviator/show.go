package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var showFlags struct {
	JSON bool
}

var showCmd = &cobra.Command{
	Use:     "show <id>",
	Aliases: []string{"get"},
	Short:   "Show a review or runbook session (e.g. aviator show r/123)",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		detail, err := client.GetRunbookDetail(cmd.Context(), runbookNumber, nil)
		if err != nil {
			return err
		}
		runs, moreRuns, runsErr := client.ListVerifyRuns(cmd.Context(), runbookNumber, showRunsLimit)
		if runsErr != nil {
			fmt.Fprintf(os.Stderr, "%s couldn't list runs: %v\n", colors.Warning("warning:"), runsErr)
		}
		if showFlags.JSON {
			out := newShowJSON(detail, runs, moreRuns)
			if runsErr != nil {
				out.Runs = nil
			}
			return printJSON(out)
		}
		fmt.Print(formatRunbookDetail(detail, runs, moreRuns))
		return nil
	},
}

const showRunsLimit = 20

func init() {
	showCmd.Flags().BoolVar(&showFlags.JSON, "json", false,
		"print the session as a single JSON object instead of the human summary")
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to encode JSON")
	}
	fmt.Println(string(data))
	return nil
}

// formatRunbookDetail renders a session detail as a short human summary.
func formatRunbookDetail(d *api.RunbookDetail, runs []api.VerificationRun, moreRuns bool) string {
	var b strings.Builder
	b.WriteString(formatDetailHeader(d))

	if intent := deref(d.Intent); intent != "" {
		fmt.Fprintf(&b, "  Intent: %s\n", intent)
	}

	if s := d.RunbookState; s != nil {
		fmt.Fprintf(&b, "  Branch: %s -> %s\n",
			branchOr(s.WorkingBranch), branchOr(s.TargetBranch))
		if len(s.Steps) > 0 {
			done := 0
			for _, step := range s.Steps {
				if step.Status == "completed" {
					done++
				}
			}
			fmt.Fprintf(&b, "  Steps: %d/%d completed\n", done, len(s.Steps))
		}
	}

	if name := deref(d.PreviewConfigName); name != "" {
		fmt.Fprintf(&b, "  Preview config: %s\n", name)
	}

	if len(d.SpecFiles) > 0 {
		names := make([]string, len(d.SpecFiles))
		for i, sf := range d.SpecFiles {
			names[i] = sf.Filename
		}
		fmt.Fprintf(&b, "  Spec files: %s\n", strings.Join(names, ", "))
	}

	if len(d.AcceptanceCriteria) > 0 {
		b.WriteString("  Criteria:\n")
		for _, c := range d.AcceptanceCriteria {
			fmt.Fprintf(&b, "    %d. %s%s\n", c.Ordinal, c.RawText, formatHandle(c.StableKey, nil))
		}
	}

	if v := d.LatestVerification; v != nil {
		b.WriteString(formatVerification("Latest verification", v))
	} else if len(d.AcceptanceCriteria) > 0 {
		b.WriteString("  Latest verification: none yet\n")
	}

	if len(runs) > 0 {
		b.WriteString(formatRuns(runs, moreRuns, d.URL))
	}

	return b.String()
}

func formatRuns(runs []api.VerificationRun, more bool, reviewURL string) string {
	var b strings.Builder
	b.WriteString("  Runs:\n")
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, r := range runs {
		fmt.Fprintf(w, "    %d\t%s\t%s\t%d/%d passed\t%s\t%s\n",
			r.ID, r.Status, orDash(shortSHA(deref(r.CommitSHA))),
			r.CriteriaPassed, r.CriteriaTotal, formatRunVersion(r.RunbookVersion), formatRelativeTime(r.CreatedAt))
	}
	_ = w.Flush()
	if more {
		fmt.Fprintf(&b, "    More runs: %s/verify/history\n", reviewURL)
	}
	return b.String()
}

func formatRunVersion(version *int) string {
	if version == nil {
		return "-"
	}
	return fmt.Sprintf("version %d", *version)
}

func formatRelativeTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return humanize.Time(t)
}

// formatDetailHeader renders the one-line runbook identity header.
func formatDetailHeader(d *api.RunbookDetail) string {
	return fmt.Sprintf("%s %s%s — %s\n",
		colors.Success("✓"), formatRunbookID(d.RunbookNumber), formatVersion(d.RunbookVersion), d.URL)
}

// formatVerification renders a verification run as indented summary lines.
func formatVerification(label string, v *api.VerificationRun) string {
	var b strings.Builder
	sha := ""
	if commit := deref(v.CommitSHA); commit != "" {
		sha = ", " + shortSHA(commit)
	}
	fmt.Fprintf(&b, "  %s: run %d, %s (%d/%d passed, %d failed%s)\n",
		label, v.ID, v.Status, v.CriteriaPassed, v.CriteriaTotal, v.CriteriaFailed, sha)
	if name := deref(v.PreviewConfigName); name != "" {
		if method := deref(v.PreviewMethod); method != "" {
			name += " (" + method + ")"
		}
		fmt.Fprintf(&b, "    Ran on preview config: %s\n", name)
	}
	if msg := deref(v.ErrorMessage); msg != "" {
		fmt.Fprintf(&b, "    Error: %s\n", msg)
	}
	for _, fr := range v.FailedResults {
		reason := ""
		if r := deref(fr.Reason); r != "" {
			reason = ": " + r
		}
		// Warnings don't block the gate; errored rows read as failed, like the web UI.
		mark := colors.Failure("✗")
		if fr.Status == "warn" {
			mark = colors.Warning("⚠")
		}
		fmt.Fprintf(&b, "    %s %s%s%s\n", mark, fr.Criterion,
			formatHandle(fr.StableKey, fr.BaselineInvariantID), reason)
	}
	switch v.Status {
	case "failed":
		fmt.Fprintf(&b, "  %s\n", colors.Faint("Fixing a failure: "+docsResults+"#fixing-a-failure"))
	case "error":
		fmt.Fprintf(&b, "  %s\n", colors.Faint("Runs that didn't finish: "+docsResults+"#runs-that-didnt-finish"))
	}
	return b.String()
}

// formatHandle renders the id `aviator dismiss` takes for a criterion.
func formatHandle(stableKey *string, invariantID *int) string {
	switch {
	case stableKey != nil:
		return " " + colors.Faint("[key "+*stableKey+"]")
	case invariantID != nil:
		return " " + colors.Faint(fmt.Sprintf("[invariant %d]", *invariantID))
	}
	return ""
}

func branchOr(s *string) string {
	if deref(s) == "" {
		return "?"
	}
	return *s
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

type showJSON struct {
	sessionRef
	Version *int `json:"version"`
	// TODO: drop once the verify-submit skill reads version instead.
	RunbookVersion     *int              `json:"runbook_version"`
	Intent             string            `json:"intent"`
	WorkingBranch      string            `json:"working_branch"`
	TargetBranch       string            `json:"target_branch"`
	PullRequests       []pullRequestJSON `json:"pull_requests"`
	SpecFiles          []string          `json:"spec_files"`
	Criteria           []criterionJSON   `json:"criteria"`
	LatestVerification *verificationJSON `json:"latest_verification"`
	Runs               []runJSON         `json:"runs"`
	RunsHasMore        bool              `json:"runs_has_more"`
	PreviewConfigName  *string           `json:"preview_config_name"`
}

type pullRequestJSON struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

type criterionJSON struct {
	Text      string  `json:"text"`
	StableKey *string `json:"stable_key"`
}

// handleJSON names a criterion the way `aviator dismiss --criteria-json` takes it.
type handleJSON struct {
	StableKey           *string `json:"stable_key"`
	BaselineInvariantID *int    `json:"baseline_invariant_id"`
}

func newShowJSON(d *api.RunbookDetail, runs []api.VerificationRun, moreRuns bool) showJSON {
	out := showJSON{
		sessionRef:         newSessionRef(d.RunbookNumber, d.URL),
		Version:            d.RunbookVersion,
		RunbookVersion:     d.RunbookVersion,
		Intent:             deref(d.Intent),
		PullRequests:       make([]pullRequestJSON, 0, len(d.PullRequests)),
		SpecFiles:          make([]string, 0, len(d.SpecFiles)),
		Criteria:           make([]criterionJSON, 0, len(d.AcceptanceCriteria)),
		LatestVerification: newVerificationJSON(d.LatestVerification),
		PreviewConfigName:  d.PreviewConfigName,
		Runs:               make([]runJSON, 0, len(runs)),
		RunsHasMore:        moreRuns,
	}
	for i := range runs {
		out.Runs = append(out.Runs, newRunJSON(&runs[i]))
	}
	if s := d.RunbookState; s != nil {
		out.WorkingBranch, out.TargetBranch = deref(s.WorkingBranch), deref(s.TargetBranch)
	}
	for _, pr := range d.PullRequests {
		out.PullRequests = append(out.PullRequests, pullRequestJSON{Number: pr.Number, URL: pr.URL})
	}
	for _, f := range d.SpecFiles {
		out.SpecFiles = append(out.SpecFiles, f.Filename)
	}
	for _, c := range d.AcceptanceCriteria {
		out.Criteria = append(out.Criteria, criterionJSON{Text: c.RawText, StableKey: c.StableKey})
	}
	return out
}

// verificationJSON is a verification run. Evidence and Location on a failure
// are the evaluator's free-form objects, passed through as is.
type verificationJSON struct {
	runJSON
	Failures []failureJSON `json:"failures"`
}

// runJSON summarizes a verification run without its failures.
type runJSON struct {
	RunID         int     `json:"run_id"`
	Status        string  `json:"status"`
	CommitSHA     *string `json:"commit_sha"`
	Version       *int    `json:"version"`
	Total         int     `json:"total"`
	Passed        int     `json:"passed"`
	Failed        int     `json:"failed"`
	Skipped       int     `json:"skipped"`
	Waived        int     `json:"waived"`
	Error         *string `json:"error"`
	EvaluatorOnly bool    `json:"evaluator_only"`
	CreatedAt     string  `json:"created_at"`
	StartedAt     *string `json:"started_at"`
	CompletedAt   *string `json:"completed_at"`
	PreviewConfig *string `json:"preview_config_name"`
	PreviewMethod *string `json:"preview_method"`
}

func newRunJSON(r *api.VerificationRun) runJSON {
	return runJSON{
		RunID:         r.ID,
		Status:        r.Status,
		CommitSHA:     r.CommitSHA,
		Version:       r.RunbookVersion,
		Total:         r.CriteriaTotal,
		Passed:        r.CriteriaPassed,
		Failed:        r.CriteriaFailed,
		Skipped:       r.CriteriaSkipped,
		Waived:        r.CriteriaWaived,
		Error:         r.ErrorMessage,
		EvaluatorOnly: r.EvaluatorOnly,
		CreatedAt:     r.CreatedAt,
		StartedAt:     r.StartedAt,
		CompletedAt:   r.CompletedAt,
		PreviewConfig: r.PreviewConfigName,
		PreviewMethod: r.PreviewMethod,
	}
}

type failureJSON struct {
	handleJSON
	Criterion string          `json:"criterion"`
	Status    string          `json:"status"`
	Reason    *string         `json:"reason"`
	Invariant bool            `json:"invariant"`
	Waived    bool            `json:"waived"`
	Evidence  json.RawMessage `json:"evidence"`
	Location  json.RawMessage `json:"location"`
}

func newVerificationJSON(v *api.VerificationRun) *verificationJSON {
	if v == nil {
		return nil
	}
	out := &verificationJSON{
		runJSON:  newRunJSON(v),
		Failures: make([]failureJSON, 0, len(v.FailedResults)),
	}
	for _, fr := range v.FailedResults {
		out.Failures = append(out.Failures, failureJSON{
			handleJSON: handleJSON{StableKey: fr.StableKey, BaselineInvariantID: fr.BaselineInvariantID},
			Criterion:  fr.Criterion,
			Status:     fr.Status,
			Reason:     fr.Reason,
			Invariant:  fr.IsInvariant,
			Waived:     fr.IsWaived,
			Evidence:   nullIfEmpty(fr.Evidence),
			Location:   nullIfEmpty(fr.Location),
		})
	}
	return out
}

func nullIfEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}
