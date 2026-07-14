package api

import "context"

// CreateRunbookRequest is the body for POST /api/v1/runbook.
type CreateRunbookRequest struct {
	Repository   Repository `json:"repository"`
	Prompt       string     `json:"prompt"`
	Title        string     `json:"title,omitempty"`
	Oneshot      bool       `json:"oneshot"`
	TargetBranch string     `json:"target_branch,omitempty"`
	AuthorEmail  string     `json:"author_email,omitempty"`
}

// CreateRunbookResponse is the (partial) response from POST /api/v1/runbook.
type CreateRunbookResponse struct {
	RunbookNumber int    `json:"runbook_number"`
	URL           string `json:"url"`
	Status        string `json:"status"`
}

// CreateRunbook starts a runbook from a prompt.
func (c *Client) CreateRunbook(
	ctx context.Context, req CreateRunbookRequest,
) (*CreateRunbookResponse, error) {
	var out CreateRunbookResponse
	if err := c.postJSON(ctx, "/api/v1/runbook", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
