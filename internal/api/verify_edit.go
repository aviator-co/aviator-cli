package api

import (
	"context"
	"fmt"
)

// EditVerifyRequest is the body for PATCH /api/v1/verify/<n>. ExpectedVersion
// is required whenever AcceptanceCriteria is sent.
type EditVerifyRequest struct {
	Intent             string   `json:"intent,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	ExpectedVersion    int      `json:"expected_version,omitempty"`
}

// VerifySession is the session shape returned by PATCH /api/v1/verify/<n>.
// AcceptanceCriteria includes baseline-invariant criteria as raw text.
type VerifySession struct {
	RunbookNumber      int      `json:"runbook_number"`
	URL                string   `json:"url"`
	WorkingBranch      string   `json:"working_branch"`
	TargetBranch       string   `json:"target_branch"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Intent             string   `json:"intent"`
	Version            *int     `json:"version"`
}

// EditVerify updates a session's intent and/or replaces its acceptance criteria.
func (c *Client) EditVerify(
	ctx context.Context, runbookNumber int, req EditVerifyRequest,
) (*VerifySession, error) {
	path := fmt.Sprintf("/api/v1/verify/%d", runbookNumber)
	var out VerifySession
	if err := c.patchJSON(ctx, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
