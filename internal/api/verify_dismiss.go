package api

import (
	"context"
	"fmt"
)

// Dismissal names one criterion to clear off the verify gate: a task criterion
// by StableKey (deleted), or an invariant by BaselineInvariantID (waived, which
// requires Category and Justification).
type Dismissal struct {
	StableKey           string `json:"stable_key,omitempty"`
	BaselineInvariantID int    `json:"baseline_invariant_id,omitempty"`
	Category            string `json:"category,omitempty"`
	Justification       string `json:"justification,omitempty"`
}

// DismissCriteriaRequest is the body for POST /api/v1/verify/<n>/dismissals.
type DismissCriteriaRequest struct {
	Criteria []Dismissal `json:"criteria"`
}

// DismissCriteriaResponse is the response from POST /api/v1/verify/<n>/dismissals.
type DismissCriteriaResponse struct {
	RunbookNumber  int  `json:"runbook_number"`
	NewVersion     *int `json:"new_version"`
	Waived         int  `json:"waived"`
	Deleted        int  `json:"deleted"`
	AlreadyDeleted int  `json:"already_deleted"`
	CriteriaCount  int  `json:"criteria_count"`
}

// DismissCriteria deletes task criteria and waives invariants on a session.
func (c *Client) DismissCriteria(
	ctx context.Context, runbookNumber int, req DismissCriteriaRequest,
) (*DismissCriteriaResponse, error) {
	path := fmt.Sprintf("/api/v1/verify/%d/dismissals", runbookNumber)
	var out DismissCriteriaResponse
	if err := c.postJSON(ctx, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
