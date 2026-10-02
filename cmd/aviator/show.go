package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
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
		if showFlags.JSON {
			return printJSON(newShowJSON(detail))
		}
		fmt.Print(formatRunbookDetail(detail))
		return nil
	},
}

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
func formatRunbookDetail(d *api.RunbookDetail) string {
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
			fmt.Fprintf(&b, "    %d. %s\n", c.Ordinal, c.RawText)
		}
	}

	if v := d.LatestVerification; v != nil {
		b.WriteString(formatVerification(v))
	} else if len(d.AcceptanceCriteria) > 0 {
		b.WriteString("  Latest verification: none yet\n")
	}

	return b.String()
}

// formatDetailHeader renders the one-line runbook identity header.
func formatDetailHeader(d *api.RunbookDetail) string {
	return fmt.Sprintf("%s %s%s — %s\n",
		colors.Success("✓"), formatRunbookID(d.RunbookNumber), formatVersion(d.RunbookVersion), d.URL)
}

// formatVerification renders a verification run as indented summary lines.
func formatVerification(v *api.LatestVerification) string {
	var b strings.Builder
	sha := ""
	if commit := deref(v.CommitSHA); commit != "" {
		sha = ", " + shortSHA(commit)
	}
	fmt.Fprintf(&b, "  Latest verification: %s (%d/%d passed, %d failed%s)\n",
		v.Status, v.CriteriaPassed, v.CriteriaTotal, v.CriteriaFailed, sha)
	if msg := deref(v.ErrorMessage); msg != "" {
		fmt.Fprintf(&b, "    Error: %s\n", msg)
	}
	for _, fr := range v.FailedResults {
		reason := ""
		if r := deref(fr.Reason); r != "" {
			reason = ": " + r
		}
		fmt.Fprintf(&b, "    %s %s%s\n", colors.Failure("✗"), fr.Criterion, reason)
	}
	return b.String()
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
}

type pullRequestJSON struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

type criterionJSON struct {
	Text string `json:"text"`
}

func newShowJSON(d *api.RunbookDetail) showJSON {
	out := showJSON{
		sessionRef:         newSessionRef(d.RunbookNumber, d.URL),
		Version:            d.RunbookVersion,
		RunbookVersion:     d.RunbookVersion,
		Intent:             deref(d.Intent),
		PullRequests:       make([]pullRequestJSON, 0, len(d.PullRequests)),
		SpecFiles:          make([]string, 0, len(d.SpecFiles)),
		Criteria:           make([]criterionJSON, 0, len(d.AcceptanceCriteria)),
		LatestVerification: newVerificationJSON(d.LatestVerification),
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
		out.Criteria = append(out.Criteria, criterionJSON{Text: c.RawText})
	}
	return out
}

// verificationJSON is a verification run. Evidence and Location on a failure
// are the evaluator's free-form objects, passed through as is.
type verificationJSON struct {
	Status    string        `json:"status"`
	CommitSHA *string       `json:"commit_sha"`
	Version   *int          `json:"version"`
	Total     int           `json:"total"`
	Passed    int           `json:"passed"`
	Failed    int           `json:"failed"`
	Skipped   int           `json:"skipped"`
	Waived    int           `json:"waived"`
	Error     *string       `json:"error"`
	Failures  []failureJSON `json:"failures"`
}

type failureJSON struct {
	Criterion string          `json:"criterion"`
	Status    string          `json:"status"`
	Reason    *string         `json:"reason"`
	Invariant bool            `json:"invariant"`
	Waived    bool            `json:"waived"`
	Evidence  json.RawMessage `json:"evidence"`
	Location  json.RawMessage `json:"location"`
}

func newVerificationJSON(v *api.LatestVerification) *verificationJSON {
	if v == nil {
		return nil
	}
	out := &verificationJSON{
		Status:    v.Status,
		CommitSHA: v.CommitSHA,
		Version:   v.RunbookVersion,
		Total:     v.CriteriaTotal,
		Passed:    v.CriteriaPassed,
		Failed:    v.CriteriaFailed,
		Skipped:   v.CriteriaSkipped,
		Waived:    v.CriteriaWaived,
		Error:     v.ErrorMessage,
		Failures:  make([]failureJSON, 0, len(v.FailedResults)),
	}
	for _, fr := range v.FailedResults {
		out.Failures = append(out.Failures, failureJSON{
			Criterion: fr.Criterion,
			Status:    fr.Status,
			Reason:    fr.Reason,
			Invariant: fr.IsInvariant,
			Waived:    fr.IsWaived,
			Evidence:  nullIfEmpty(fr.Evidence),
			Location:  nullIfEmpty(fr.Location),
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
