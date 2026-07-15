package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var knownDetailFields = []string{
	"steps_markdown", "spec_files", "runbook_state", "acceptance_criteria",
}

var showFlags struct {
	Fields []string
	JSON   bool
}

var showCmd = &cobra.Command{
	Use:     "show <id>",
	Aliases: []string{"get"},
	Short:   "Show a runbook/verify session (e.g. aviator show r/123)",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		fields, err := validateDetailFields(showFlags.Fields)
		if err != nil {
			return err
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		if showFlags.JSON {
			raw, err := client.GetRunbookDetailRaw(cmd.Context(), runbookNumber, fields)
			if err != nil {
				return err
			}
			return printJSON(raw)
		}
		detail, err := client.GetRunbookDetail(cmd.Context(), runbookNumber, fields)
		if err != nil {
			return err
		}
		fmt.Print(formatRunbookDetail(detail, slices.Contains(fields, "steps_markdown")))
		return nil
	},
}

func init() {
	f := showCmd.Flags()
	f.StringSliceVar(&showFlags.Fields, "fields", nil,
		"comma-separated subset of "+strings.Join(knownDetailFields, ", "))
	f.BoolVar(&showFlags.JSON, "json", false, "print the raw response as pretty JSON")
}

// validateDetailFields trims and validates requested fields against the known
// set, returning a helpful error for anything unrecognized.
func validateDetailFields(fields []string) ([]string, error) {
	var out []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !slices.Contains(knownDetailFields, f) {
			return nil, errors.Errorf(
				"unknown field %q; valid fields are %s",
				f, strings.Join(knownDetailFields, ", "),
			)
		}
		out = append(out, f)
	}
	return out, nil
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to encode JSON")
	}
	fmt.Println(string(data))
	return nil
}

// formatRunbookDetail renders a runbook detail as a short human summary. The
// steps markdown is long-form, so it is only included when explicitly
// requested via --fields.
func formatRunbookDetail(d *api.RunbookDetail, includeStepsMarkdown bool) string {
	var b strings.Builder
	b.WriteString(formatDetailHeader(d))

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

	if includeStepsMarkdown && d.StepsMarkdown != nil && *d.StepsMarkdown != "" {
		b.WriteString("\n" + strings.TrimRight(*d.StepsMarkdown, "\n") + "\n")
	}

	return b.String()
}

// formatDetailHeader renders the one-line runbook identity header.
func formatDetailHeader(d *api.RunbookDetail) string {
	version := ""
	if d.RunbookVersion != nil {
		version = fmt.Sprintf(" (version %d)", *d.RunbookVersion)
	}
	return fmt.Sprintf("%s Runbook %s%s — %s\n",
		colors.Success("✓"), formatRunbookID(d.RunbookNumber), version, d.URL)
}

// formatVerification renders a verification run as indented summary lines.
func formatVerification(v *api.LatestVerification) string {
	var b strings.Builder
	sha := ""
	if v.CommitSHA != nil && *v.CommitSHA != "" {
		sha = ", " + shortSHA(*v.CommitSHA)
	}
	fmt.Fprintf(&b, "  Latest verification: %s (%d/%d passed, %d failed%s)\n",
		v.Status, v.CriteriaPassed, v.CriteriaTotal, v.CriteriaFailed, sha)
	if v.ErrorMessage != nil && *v.ErrorMessage != "" {
		fmt.Fprintf(&b, "    Error: %s\n", *v.ErrorMessage)
	}
	for _, fr := range v.FailedResults {
		reason := ""
		if fr.Reason != nil && *fr.Reason != "" {
			reason = ": " + *fr.Reason
		}
		fmt.Fprintf(&b, "    %s %s%s\n", colors.Failure("✗"), fr.Criterion, reason)
	}
	return b.String()
}

func branchOr(s *string) string {
	if s == nil || *s == "" {
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
