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

// HasPullRequest reports whether the session is linked to a PR number.
func (s SessionSummary) HasPullRequest(number int) bool {
	for _, pr := range s.PullRequests {
		if pr.Number == number {
			return true
		}
	}
	return false
}

// ListSessionsParams are the filters for GET /api/v1/runbook/. Limit is capped
// at the API's own page size, so a page here is a page there.
type ListSessionsParams struct {
	Repository    Repository
	WorkingBranch string
	Status        string
	Page          int
	Limit         int
}

const (
	maxPageSize  = 100
	maxPageWalks = 10
)

// ListSessions returns the caller's sessions in a repository, newest first:
// one page of Limit, and whether more follow it.
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

// FindSessionsForPullRequest returns the sessions linked to a PR number. The
// listing filters on working branch only, so the match happens here, over the
// PRs each summary carries.
func (c *Client) FindSessionsForPullRequest(
	ctx context.Context, repo Repository, prNumber int, status string,
) ([]SessionSummary, error) {
	params := ListSessionsParams{Repository: repo, Status: status}
	var found []SessionSummary
	for page := 1; page <= maxPageWalks; page++ {
		resp, err := c.listSessionPage(ctx, params, page, maxPageSize)
		if err != nil {
			return nil, err
		}
		for _, session := range resp.Sessions {
			if session.HasPullRequest(prNumber) {
				found = append(found, session)
			}
		}
		if !resp.HasMore {
			break
		}
	}
	return found, nil
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
	if params.Status != "" {
		query.Set("status", params.Status)
	}

	var out listSessionsResponse
	if err := c.getJSON(ctx, "/api/v1/runbook/", query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
