package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// DecisionAuthor is the role, and the agent details when known, behind a
// decision write or a comment. Role is person, coding_agent, or review_agent.
type DecisionAuthor struct {
	Role    string   `json:"role"`
	Model   string   `json:"model,omitempty"`
	Harness string   `json:"harness,omitempty"`
	Skills  []string `json:"skills,omitempty"`
}

// DecisionUser is the Aviator user behind a decision or comment.
type DecisionUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// DecisionAnchor pins a decision to lines of a file at a commit.
type DecisionAnchor struct {
	CommitSHA string `json:"commit_sha,omitempty"`
	FilePath  string `json:"file_path,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// DecisionRepo is the repository a decision's pull request is in.
type DecisionRepo struct {
	Org  string `json:"org"`
	Name string `json:"name"`
}

// DecisionComment is one entry in a decision's thread. User is the account
// that wrote it; Author is the role it was written as. Either can be null.
type DecisionComment struct {
	ID         int             `json:"id"`
	User       *DecisionUser   `json:"user"`
	Author     *DecisionAuthor `json:"author"`
	Body       string          `json:"body"`
	CreatedAt  string          `json:"created_at"`
	ModifiedAt string          `json:"modified_at"`
}

// Decision is a question, possible bug, risk, or choice raised about a
// runbook's pull request. Status is open or settled; Outcome, SettledAt, and
// Resolution are set once it is settled. RaisedBy is the user who created it
// and never changes; Author is the author of the latest change. Either can be
// null. The list carries CommentCount and no Comments; one decision, and the
// decisions a POST returns, carry Comments in id order and no CommentCount.
type Decision struct {
	ID           int               `json:"id"`
	Type         string            `json:"type"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	Status       string            `json:"status"`
	Outcome      string            `json:"outcome"`
	SettledAt    string            `json:"settled_at"`
	Resolution   string            `json:"resolution"`
	Anchor       *DecisionAnchor   `json:"anchor"`
	Repo         *DecisionRepo     `json:"repo"`
	RaisedBy     *DecisionUser     `json:"raised_by"`
	Author       *DecisionAuthor   `json:"author"`
	CreatedAt    string            `json:"created_at"`
	ModifiedAt   string            `json:"modified_at"`
	CommentCount int               `json:"comment_count"`
	Comments     []DecisionComment `json:"comments"`
}

// NumComments is the length of the thread, from Comments when the response
// carried them and from CommentCount when it didn't.
func (d *Decision) NumComments() int {
	if d.Comments != nil {
		return len(d.Comments)
	}
	return d.CommentCount
}

// ListDecisionsParams filters and pages GET /api/v1/verify/<n>/decisions.
// Status is open or settled, blank for both. A zero Page or PerPage leaves it
// to the server (page 1, 20 per page, at most 100).
type ListDecisionsParams struct {
	Status  string
	Page    int
	PerPage int
}

// ListDecisionsResponse is one page of a runbook's decisions, in id order.
type ListDecisionsResponse struct {
	Decisions []Decision `json:"decisions"`
	Page      int        `json:"page"`
	PerPage   int        `json:"per_page"`
	HasMore   bool       `json:"has_more"`
}

// ListDecisions fetches one page of a runbook's decisions, without comments.
func (c *Client) ListDecisions(
	ctx context.Context, runbookNumber int, params ListDecisionsParams,
) (*ListDecisionsResponse, error) {
	query := url.Values{}
	if params.Status != "" {
		query.Set("status", params.Status)
	}
	if params.Page > 0 {
		query.Set("page", strconv.Itoa(params.Page))
	}
	if params.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(params.PerPage))
	}
	var out ListDecisionsResponse
	if err := c.getJSON(ctx, decisionsPath(runbookNumber), query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetDecision fetches one decision with its comment thread.
func (c *Client) GetDecision(ctx context.Context, runbookNumber, decisionID int) (*Decision, error) {
	var out Decision
	if err := c.getJSON(ctx, decisionPath(runbookNumber, decisionID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApplyDecisionsResponse is the decisions a POST touched, in request order,
// each with its comments.
type ApplyDecisionsResponse struct {
	Decisions []Decision `json:"decisions"`
}

// ApplyDecisions sends body, a {"decisions": [...]} batch, to POST
// /api/v1/verify/<n>/decisions as is. The server validates it strictly and
// applies it all or nothing; decoding and re-encoding it here would drop
// nulls, zero ids, and unknown keys before the server could see them.
func (c *Client) ApplyDecisions(ctx context.Context, runbookNumber int, body []byte) (*ApplyDecisionsResponse, error) {
	var out ApplyDecisionsResponse
	if err := c.doPayload(ctx, http.MethodPost, decisionsPath(runbookNumber), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func decisionsPath(runbookNumber int) string {
	return fmt.Sprintf("/api/v1/verify/%d/decisions", runbookNumber)
}

func decisionPath(runbookNumber, decisionID int) string {
	return fmt.Sprintf("%s/%d", decisionsPath(runbookNumber), decisionID)
}
