package api

import (
	"context"
	"fmt"
)

// EditRunbookCriteriaRequest is the body for
// PATCH /api/v1/runbook/<n>/acceptance-criteria.
type EditRunbookCriteriaRequest struct {
	ExpectedVersion    int      `json:"expected_version"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

// EditRunbookCriteriaResponse is the response from that endpoint. A 200 always
// carries the concrete new version: the server rejects edits before an active
// version exists and every accepted edit creates a new one.
type EditRunbookCriteriaResponse struct {
	RunbookNumber int    `json:"runbook_number"`
	NewVersion    int    `json:"new_version"`
	CriteriaCount int    `json:"criteria_count"`
	Message       string `json:"message"`
}

// EditRunbookCriteria replaces a runbook's acceptance criteria, guarded by the
// caller's expected version.
func (c *Client) EditRunbookCriteria(
	ctx context.Context, runbookNumber int, req EditRunbookCriteriaRequest,
) (*EditRunbookCriteriaResponse, error) {
	path := fmt.Sprintf("/api/v1/runbook/%d/acceptance-criteria", runbookNumber)
	var out EditRunbookCriteriaResponse
	if err := c.patchJSON(ctx, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
