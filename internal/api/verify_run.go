package api

import (
	"context"
	"fmt"
)

// TriggerVerifyRunRequest is the body for POST /api/v1/verify/<number>/run.
type TriggerVerifyRunRequest struct {
	EvaluatorOnly       bool `json:"evaluator_only,omitempty"`
	Force               bool `json:"force,omitempty"`
	RegenerateScenarios bool `json:"regenerate_scenarios,omitempty"`
}

// TriggerVerifyRunResponse is the response from POST /api/v1/verify/<number>/run.
// Deduplicated is true when the server returned an existing equivalent run
// instead of enqueueing a new one.
type TriggerVerifyRunResponse struct {
	RunbookNumber int    `json:"runbook_number"`
	URL           string `json:"url"`
	RunID         int    `json:"run_id"`
	RunStatus     string `json:"run_status"`
	Deduplicated  bool   `json:"deduplicated"`
	Message       string `json:"message"`
}

// TriggerVerifyRun starts a verification run on an existing verify session.
func (c *Client) TriggerVerifyRun(
	ctx context.Context, runbookNumber int, req TriggerVerifyRunRequest,
) (*TriggerVerifyRunResponse, error) {
	var out TriggerVerifyRunResponse
	path := fmt.Sprintf("/api/v1/verify/%d/run", runbookNumber)
	if err := c.postJSON(ctx, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
