package api

import (
	"context"
	"encoding/json"
	"fmt"
)

// DecisionAuthor is the role, and the agent details when known, behind a
// decision write or a comment. Role is person, coding_agent, review_agent, or
// external_bot.
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
// null. Comments are in creation order.
type Decision struct {
	ID         int               `json:"id"`
	Type       string            `json:"type"`
	Title      string            `json:"title"`
	Summary    string            `json:"summary"`
	Status     string            `json:"status"`
	Outcome    string            `json:"outcome"`
	SettledAt  string            `json:"settled_at"`
	Resolution string            `json:"resolution"`
	Anchor     *DecisionAnchor   `json:"anchor"`
	RaisedBy   *DecisionUser     `json:"raised_by"`
	Author     *DecisionAuthor   `json:"author"`
	Comments   []DecisionComment `json:"comments"`
}

// DecisionBatchRequest is the body for POST
// /api/v1/verify/<n>/decisions/batch. The items are applied all-or-nothing.
type DecisionBatchRequest struct {
	Decisions []DecisionBatchItem `json:"decisions"`
}

// DecisionBatchItem creates a decision when ID is zero (Type and Title are
// required), and otherwise changes only the keys it sends. Author is credited
// with the creation or change. An Outcome settles the decision and a null one
// reopens it; a null Anchor clears it.
type DecisionBatchItem struct {
	ID         int                      `json:"id,omitempty"`
	Author     DecisionAuthor           `json:"author,omitzero"`
	Type       string                   `json:"type,omitempty"`
	Title      string                   `json:"title,omitempty"`
	Summary    string                   `json:"summary,omitempty"`
	Anchor     Nullable[DecisionAnchor] `json:"anchor,omitzero"`
	Outcome    Nullable[string]         `json:"outcome,omitzero"`
	Resolution string                   `json:"resolution,omitempty"`
	Comments   []DecisionCommentInput   `json:"comments,omitempty"`
}

// DecisionCommentInput is one comment to add. ModelMetadata is free-form agent
// info the server stores but never returns.
type DecisionCommentInput struct {
	Author        DecisionAuthor `json:"author,omitzero"`
	Body          string         `json:"body"`
	ModelMetadata map[string]any `json:"model_metadata,omitempty"`
}

// Nullable is a key that can be left out, set to a value, or sent as null.
// The zero value is left out (with the omitzero tag).
type Nullable[T any] struct {
	Set   bool
	Value *T
}

// Null returns a Nullable that is sent as null.
func Null[T any]() Nullable[T] { return Nullable[T]{Set: true} }

// ValueOf returns a Nullable that is sent as v.
func ValueOf[T any](v T) Nullable[T] { return Nullable[T]{Set: true, Value: &v} }

// IsZero reports whether the key is left out.
func (n Nullable[T]) IsZero() bool { return !n.Set }

// MarshalJSON writes the value, or null.
func (n Nullable[T]) MarshalJSON() ([]byte, error) { return json.Marshal(n.Value) }

// UnmarshalJSON marks the key present, keeping null apart from a left-out key.
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	*n = Nullable[T]{Set: true}
	return json.Unmarshal(data, &n.Value)
}

// DecisionsResponse is the response from GET /api/v1/verify/<n>/decisions,
// open decisions first, and from the batch POST, in request order.
type DecisionsResponse struct {
	Decisions []Decision `json:"decisions"`
}

// ListDecisions fetches a runbook's decisions.
func (c *Client) ListDecisions(ctx context.Context, runbookNumber int) (*DecisionsResponse, error) {
	var out DecisionsResponse
	if err := c.getJSON(ctx, decisionsPath(runbookNumber), nil, &out); err != nil {
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

// ApplyDecisions creates, changes, settles, and comments on decisions in one
// all-or-nothing batch, returning each item's decision in request order.
func (c *Client) ApplyDecisions(
	ctx context.Context, runbookNumber int, req DecisionBatchRequest,
) (*DecisionsResponse, error) {
	var out DecisionsResponse
	if err := c.postJSON(ctx, decisionsPath(runbookNumber)+"/batch", req, &out); err != nil {
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

// EditDecisionComment replaces a comment's body. Only the user who wrote the
// comment can edit it.
func (c *Client) EditDecisionComment(
	ctx context.Context, runbookNumber, decisionID, commentID int, body string,
) (*DecisionComment, error) {
	req := struct {
		Body string `json:"body"`
	}{Body: body}
	var out DecisionComment
	path := fmt.Sprintf("%s/comments/%d", decisionPath(runbookNumber, decisionID), commentID)
	if err := c.patchJSON(ctx, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
