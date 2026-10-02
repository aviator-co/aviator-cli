package main

import (
	"strings"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
)

func TestFormatScenarios(t *testing.T) {
	sha, key, reason, label := "abcdef1234", "k1", "give_up", "Run trace"
	invariant := 42
	out := formatScenarios(&api.VerifyScenarios{
		RunbookNumber: 123,
		RunID:         900,
		RunStatus:     "failed",
		CommitSHA:     &sha,
		ScenarioRuns: []api.ScenarioRun{{
			Status:            "failed",
			TerminationReason: &reason,
			ToolCallCount:     12,
			Reused:            true,
			Scenario: api.Scenario{
				Summary:  "log in and see the banner",
				Criteria: []api.ScenarioCriterion{{StableKey: &key}, {BaselineInvariantID: &invariant}},
			},
			Evidence: []api.Evidence{{ID: 4567, Type: "trace", Label: &label}},
		}},
	})
	for _, want := range []string{
		"r/123 latest run: failed at abcdef1",
		"log in and see the banner [failed: give_up, 12 tool calls, reused from an earlier run]",
		"Criteria: [key k1] [invariant 42]",
		"[evidence 4567] trace  Run trace",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestScenariosJSON(t *testing.T) {
	key, started := "k1", "Fri, 02 Oct 2026 20:24:50 GMT"
	got := decodeJSON(t, newScenariosJSON(&api.VerifyScenarios{
		RunbookNumber: 123,
		RunStatus:     "passed",
		ScenarioRuns: []api.ScenarioRun{{
			Status:    "completed",
			StartedAt: &started,
			Scenario: api.Scenario{
				Summary:  "log in",
				Steps:    []api.ScenarioStep{{ID: 1, Text: "open the page", EvidenceTypes: []string{"screenshot"}}},
				Criteria: []api.ScenarioCriterion{{StableKey: &key}},
			},
			Evidence: []api.Evidence{
				{ID: 4567, Type: "trace", URL: "https://api/evidence/4567"},
				{ID: 4568, Type: "screenshot", StepID: ptr(1)},
			},
		}},
	}))
	if got["id"] != "r/123" || got["run_status"] != "passed" {
		t.Errorf("top level = %v", got)
	}
	scenario := got["scenarios"].([]any)[0].(map[string]any)
	if scenario["started_at"] != "2026-10-02T20:24:50Z" {
		t.Errorf("started_at = %v", scenario["started_at"])
	}
	if c := scenario["criteria"].([]any)[0].(map[string]any); c["stable_key"] != "k1" {
		t.Errorf("criteria = %v", scenario["criteria"])
	}
	evidence := scenario["evidence"].([]any)
	if trace := evidence[0].(map[string]any); trace["step_id"] != nil || trace["id"] != float64(4567) {
		t.Errorf("trace = %v", trace)
	}
	if _, ok := evidence[0].(map[string]any)["url"]; ok {
		t.Errorf("evidence should not carry the API url: %v", evidence[0])
	}
	step := scenario["steps"].([]any)[0].(map[string]any)
	if shot := evidence[1].(map[string]any); shot["step_id"] != step["id"] {
		t.Errorf("screenshot step_id = %v, step id = %v", shot["step_id"], step["id"])
	}
}
