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
	PreviewConfigName  string     `json:"preview_config_name,omitempty"`
	ScenarioGuidance   string     `json:"scenario_guidance,omitempty"`
	// EvidenceSource is "aviator" (the default) or "external": on an external
	// session Aviator never collects; scenarios and evidence are submitted.
	EvidenceSource string `json:"evidence_source,omitempty"`
}

// SubmitVerifyResponse is the response from POST /api/v1/verify.
type SubmitVerifyResponse struct {
	RunbookNumber      int      `json:"runbook_number"`
	URL                string   `json:"url"`
	WorkingBranch      string   `json:"working_branch"`
	TargetBranch       string   `json:"target_branch"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	PreviewConfigName  *string  `json:"preview_config_name"`
	EvidenceSource     string   `json:"evidence_source"`
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
