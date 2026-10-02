package api

import (
	"context"
	"fmt"
)

// VerifyScenarios is the response from
// GET /api/v1/verify/<n>/runs/latest/scenarios. Timestamps are HTTP-date
// strings, as the backend serializes them.
type VerifyScenarios struct {
	RunbookNumber int           `json:"runbook_number"`
	RunID         int           `json:"run_id"`
	RunStatus     string        `json:"run_status"`
	CommitSHA     *string       `json:"commit_sha"`
	ScenarioRuns  []ScenarioRun `json:"scenario_runs"`
}

// ScenarioRun is one scenario's execution within a verification run. Reused
// runs were carried over from an earlier verification run.
type ScenarioRun struct {
	ID                int        `json:"id"`
	Reused            bool       `json:"reused"`
	Status            string     `json:"status"`
	TerminationReason *string    `json:"termination_reason"`
	FailureReason     *string    `json:"failure_reason"`
	ToolCallCount     int        `json:"tool_call_count"`
	StartedAt         *string    `json:"started_at"`
	CompletedAt       *string    `json:"completed_at"`
	Scenario          Scenario   `json:"scenario"`
	Evidence          []Evidence `json:"evidence"`
}

// Scenario is what a scenario run set out to exercise.
type Scenario struct {
	ID       int                 `json:"id"`
	Summary  string              `json:"summary"`
	Steps    []ScenarioStep      `json:"steps"`
	Criteria []ScenarioCriterion `json:"criteria"`
}

// ScenarioStep is one step of a scenario.
type ScenarioStep struct {
	ID            int      `json:"id"`
	Text          string   `json:"text"`
	EvidenceTypes []string `json:"evidence_types"`
}

// ScenarioCriterion is the handle of a criterion a scenario covers.
type ScenarioCriterion struct {
	StableKey           *string `json:"stable_key"`
	BaselineInvariantID *int    `json:"baseline_invariant_id"`
}

// Evidence is one file a scenario run captured, such as a screenshot or trace.
type Evidence struct {
	ID          int     `json:"id"`
	Type        string  `json:"type"`
	Label       *string `json:"label"`
	ContentType *string `json:"content_type"`
	SizeBytes   *int    `json:"size_bytes"`
	StepID      *int    `json:"step_id"`
	URL         string  `json:"url"`
}

// GetVerifyScenarios fetches the scenario runs behind a session's latest
// verification run.
func (c *Client) GetVerifyScenarios(ctx context.Context, runbookNumber int) (*VerifyScenarios, error) {
	path := fmt.Sprintf("/api/v1/verify/%d/runs/latest/scenarios", runbookNumber)
	var out VerifyScenarios
	if err := c.getJSON(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
