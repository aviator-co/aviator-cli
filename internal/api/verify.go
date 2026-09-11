package api

import "context"

// SubmitVerifyRequest is the body for POST /api/v1/verify.
type SubmitVerifyRequest struct {
	Repository         Repository `json:"repository"`
	Intent             string     `json:"intent"`
	AcceptanceCriteria []string   `json:"acceptance_criteria"`
	WorkingBranch      string     `json:"working_branch,omitempty"`
	TargetBranch       string     `json:"target_branch,omitempty"`
	SpecFile           *SpecFile  `json:"spec_file,omitempty"`
}

// SubmitVerifyResponse is the response from POST /api/v1/verify.
type SubmitVerifyResponse struct {
	RunbookNumber      int      `json:"runbook_number"`
	URL                string   `json:"url"`
	WorkingBranch      string   `json:"working_branch"`
	TargetBranch       string   `json:"target_branch"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

// SubmitVerify creates a verify submission.
func (c *Client) SubmitVerify(
	ctx context.Context, req SubmitVerifyRequest,
) (*SubmitVerifyResponse, error) {
	var out SubmitVerifyResponse
	if err := c.postJSON(ctx, "/api/v1/verify", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
