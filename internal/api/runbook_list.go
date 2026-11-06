package api

import (
	"context"
	"net/url"
	"strconv"
)

// SessionSummary is one entry from GET /api/v1/runbook/.
type SessionSummary struct {
	RunbookNumber  int                 `json:"runbook_number"`
	URL            string              `json:"url"`
	WorkingBranch  string              `json:"working_branch"`
	TargetBranch   string              `json:"target_branch"`
	Status         string              `json:"status"`
	Intent         string              `json:"intent"`
	CreatedAt      string              `json:"created_at"`
	RunbookVersion *int                `json:"runbook_version"`
	PullRequests   []LinkedPullRequest `json:"pull_requests"`
}

// LinkedPullRequest is a pull request bound to a session.
type LinkedPullRequest struct {
	Number      int      `json:"number"`
	URL         string   `json:"url"`
	StepNumbers []string `json:"step_numbers"`
}

// ListSessionsParams are the filters for GET /api/v1/runbook/. Limit is capped
// at the API's own page size, so a page here is a page there.
type ListSessionsParams struct {
	Repository    Repository
	WorkingBranch string
	PRNumber      int
	AllUsers      bool
	Status        string
	Page          int
	Limit         int
}

const maxPageSize = 100

// ListSessions returns sessions in a repository, newest first: one page of
// Limit, and whether more follow it. Only the caller's, unless AllUsers.
func (c *Client) ListSessions(
	ctx context.Context, params ListSessionsParams,
) ([]SessionSummary, bool, error) {
	limit := params.Limit
	if limit <= 0 || limit > maxPageSize {
		limit = maxPageSize
	}
	resp, err := c.listSessionPage(ctx, params, max(params.Page, 1), limit)
	if err != nil {
		return nil, false, err
	}
	return resp.Sessions, resp.HasMore, nil
}

type listSessionsResponse struct {
	Sessions []SessionSummary `json:"runbooks"`
	HasMore  bool             `json:"has_more"`
}

func (c *Client) listSessionPage(
	ctx context.Context, params ListSessionsParams, page, perPage int,
) (*listSessionsResponse, error) {
	query := url.Values{
		"org":      {params.Repository.Org},
		"repo":     {params.Repository.Name},
		"page":     {strconv.Itoa(page)},
		"per_page": {strconv.Itoa(perPage)},
	}
	if params.WorkingBranch != "" {
		query.Set("working_branch", params.WorkingBranch)
	}
	if params.PRNumber > 0 {
		query.Set("pr_number", strconv.Itoa(params.PRNumber))
	}
	if params.AllUsers {
		query.Set("all_users", "true")
	}
	if params.Status != "" {
		query.Set("status", params.Status)
	}

	var out listSessionsResponse
	if err := c.getJSON(ctx, "/api/v1/runbook/", query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
