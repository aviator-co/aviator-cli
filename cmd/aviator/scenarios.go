package main

import (
	"fmt"
	"strings"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var scenariosFlags struct {
	JSON bool
}

var scenariosCmd = &cobra.Command{
	Use:   "scenarios <id>",
	Short: "Show what a verification run exercised and the evidence it captured (e.g. aviator scenarios r/123)",
	Long: "List the scenarios behind a review's latest verification run, with each\n" +
		"one's status, the criteria it covers, and its evidence. Every scenario\n" +
		"that ran captures a trace of what the agent did; download any evidence\n" +
		"with `aviator evidence <id> -o <path>`.\n" +
		"\n" +
		"How each criterion was checked: " + docsResults + "#code-scan-and-runtime-verdicts",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		scenarios, err := client.GetVerifyScenarios(cmd.Context(), runbookNumber)
		if err != nil {
			return err
		}
		if scenariosFlags.JSON {
			return printJSON(newScenariosJSON(scenarios))
		}
		fmt.Print(formatScenarios(scenarios))
		return nil
	},
}

func init() {
	scenariosCmd.Flags().BoolVar(&scenariosFlags.JSON, "json", false,
		"print the scenarios as a single JSON object instead of the human summary")
}

func formatScenarios(s *api.VerifyScenarios) string {
	var b strings.Builder
	sha := ""
	if commit := deref(s.CommitSHA); commit != "" {
		sha = " at " + shortSHA(commit)
	}
	fmt.Fprintf(&b, "%s latest run: %s%s\n", formatRunbookID(s.RunbookNumber), s.RunStatus, sha)
	if len(s.ScenarioRuns) == 0 {
		b.WriteString("  No scenario runs\n")
	}
	for _, sr := range s.ScenarioRuns {
		status := sr.Status
		if sr.TerminationReason != nil {
			status += ": " + *sr.TerminationReason
		}
		reused := ""
		if sr.Reused {
			reused = ", reused from an earlier run"
		}
		fmt.Fprintf(&b, "  %s [%s, %d tool calls%s]\n", sr.Scenario.Summary, status, sr.ToolCallCount, reused)
		if reason := deref(sr.FailureReason); reason != "" {
			fmt.Fprintf(&b, "    %s\n", reason)
		}
		var handles []string
		for _, c := range sr.Scenario.Criteria {
			if h := formatHandle(c.StableKey, c.BaselineInvariantID); h != "" {
				handles = append(handles, strings.TrimSpace(h))
			}
		}
		if len(handles) > 0 {
			fmt.Fprintf(&b, "    Criteria: %s\n", strings.Join(handles, " "))
		}
		for _, e := range sr.Evidence {
			label := ""
			if l := deref(e.Label); l != "" {
				label = "  " + l
			}
			fmt.Fprintf(&b, "    %s %s%s\n", colors.Faint(fmt.Sprintf("[evidence %d]", e.ID)), e.Type, label)
		}
	}
	for _, sr := range s.ScenarioRuns {
		if sr.TerminationReason != nil {
			fmt.Fprintf(&b, "  %s\n", colors.Faint("Why a scenario stops early: "+docsResults+"#runs-that-didnt-finish"))
			break
		}
	}
	return b.String()
}

type scenariosJSON struct {
	ID        string         `json:"id"`
	RunStatus string         `json:"run_status"`
	CommitSHA *string        `json:"commit_sha"`
	Scenarios []scenarioJSON `json:"scenarios"`
}

type scenarioJSON struct {
	Summary           string             `json:"summary"`
	Status            string             `json:"status"`
	TerminationReason *string            `json:"termination_reason"`
	FailureReason     *string            `json:"failure_reason"`
	ToolCallCount     int                `json:"tool_call_count"`
	Reused            bool               `json:"reused"`
	StartedAt         *string            `json:"started_at"`
	CompletedAt       *string            `json:"completed_at"`
	Steps             []scenarioStepJSON `json:"steps"`
	Criteria          []handleJSON       `json:"criteria"`
	Evidence          []evidenceJSON     `json:"evidence"`
}

type scenarioStepJSON struct {
	ID            int      `json:"id"`
	Text          string   `json:"text"`
	EvidenceTypes []string `json:"evidence_types"`
}

type evidenceJSON struct {
	ID          int     `json:"id"`
	StepID      *int    `json:"step_id"`
	Type        string  `json:"type"`
	Label       *string `json:"label"`
	ContentType *string `json:"content_type"`
	SizeBytes   *int    `json:"size_bytes"`
}

func newScenariosJSON(s *api.VerifyScenarios) scenariosJSON {
	out := scenariosJSON{
		ID:        formatRunbookID(s.RunbookNumber),
		RunStatus: s.RunStatus,
		CommitSHA: s.CommitSHA,
		Scenarios: make([]scenarioJSON, 0, len(s.ScenarioRuns)),
	}
	for _, sr := range s.ScenarioRuns {
		sc := scenarioJSON{
			Summary:           sr.Scenario.Summary,
			Status:            sr.Status,
			TerminationReason: sr.TerminationReason,
			FailureReason:     sr.FailureReason,
			ToolCallCount:     sr.ToolCallCount,
			Reused:            sr.Reused,
			StartedAt:         optionalRFC3339(sr.StartedAt),
			CompletedAt:       optionalRFC3339(sr.CompletedAt),
			Steps:             make([]scenarioStepJSON, 0, len(sr.Scenario.Steps)),
			Criteria:          make([]handleJSON, 0, len(sr.Scenario.Criteria)),
			Evidence:          make([]evidenceJSON, 0, len(sr.Evidence)),
		}
		for _, c := range sr.Scenario.Criteria {
			sc.Criteria = append(sc.Criteria, handleJSON{StableKey: c.StableKey, BaselineInvariantID: c.BaselineInvariantID})
		}
		for _, st := range sr.Scenario.Steps {
			sc.Steps = append(sc.Steps, scenarioStepJSON{ID: st.ID, Text: st.Text, EvidenceTypes: st.EvidenceTypes})
		}
		for _, e := range sr.Evidence {
			sc.Evidence = append(sc.Evidence, evidenceJSON{
				ID: e.ID, StepID: e.StepID, Type: e.Type, Label: e.Label, ContentType: e.ContentType, SizeBytes: e.SizeBytes,
			})
		}
		out.Scenarios = append(out.Scenarios, sc)
	}
	return out
}

func optionalRFC3339(ts *string) *string {
	if ts == nil {
		return nil
	}
	out := rfc3339(*ts)
	return &out
}
