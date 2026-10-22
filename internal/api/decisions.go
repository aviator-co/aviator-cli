//go:build experimental

package api

import (
	"context"
	"encoding/json"
	"fmt"

	"emperror.dev/errors"
)

// DecisionAuthor is who wrote a decision or comment, as the server reports it.
type DecisionAuthor struct {
	Role      string     `json:"role"`
	Name      string     `json:"name,omitempty"`
	AgentMeta *AgentMeta `json:"agent_meta,omitempty"`
}

// AgentMeta describes the agent behind an author.
type AgentMeta struct {
	Model string `json:"model,omitempty"`
}

// DecisionAnchor pins a decision to lines of a file at a commit.
type DecisionAnchor struct {
	CommitSHA string `json:"commit_sha,omitempty"`
	FilePath  string `json:"file_path,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// DecisionComment is one entry in a decision's thread.
type DecisionComment struct {
	ID         int            `json:"id"`
	Author     DecisionAuthor `json:"author"`
	Body       string         `json:"body"`
	CreatedAt  string         `json:"created_at"`
	ModifiedAt string         `json:"modified_at"`
}

// Decision is a question, possible bug, risk, or choice raised about a
// runbook's pull request. Status is open or settled; Outcome and SettledAt are
// set once it is settled. Comments are in creation order.
type Decision struct {
	ID        int               `json:"id"`
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Summary   string            `json:"summary"`
	Status    string            `json:"status"`
	Outcome   string            `json:"outcome"`
	SettledAt string            `json:"settled_at"`
	Anchor    *DecisionAnchor   `json:"anchor"`
	RaisedBy  DecisionAuthor    `json:"raised_by"`
	Comments  []DecisionComment `json:"comments"`
}

// CreateDecisionsRequest is the body for POST /api/v1/verify/<n>/decisions,
// which only creates. The items are created all-or-nothing.
type CreateDecisionsRequest struct {
	Decisions []DecisionCreate `json:"decisions"`
}

// DecisionCreate is one decision to create, raised by Actor. An Outcome
// creates it already settled.
type DecisionCreate struct {
	Type    string          `json:"type"`
	Title   string          `json:"title"`
	Actor   ActorRef        `json:"actor"`
	Summary string          `json:"summary,omitempty"`
	Anchor  *DecisionAnchor `json:"anchor,omitempty"`
	Outcome string          `json:"outcome,omitempty"`
}

// DecisionPatch is the body for PATCH /api/v1/verify/<n>/decisions/<id>. Only
// the keys present change; an Outcome settles the decision. Actor is required,
// but permission is on the user behind the token: only the user who raised a
// decision can patch it, as any of their actors.
type DecisionPatch struct {
	Actor   ActorRef        `json:"actor"`
	Type    string          `json:"type,omitempty"`
	Title   string          `json:"title,omitempty"`
	Summary string          `json:"summary,omitempty"`
	Anchor  *DecisionAnchor `json:"anchor,omitempty"`
	Outcome string          `json:"outcome,omitempty"`
}

// AddDecisionCommentsRequest is the body for POST
// /api/v1/verify/<n>/decisions/<id>/comments. The comments are added
// all-or-nothing. ModelMetadata describes the model that drafted them,
// whichever actors they are written as.
type AddDecisionCommentsRequest struct {
	Via           string                 `json:"via"`
	ModelMetadata *ModelMetadata         `json:"model_metadata,omitempty"`
	Comments      []DecisionCommentInput `json:"comments"`
}

// ModelMetadata names the model behind a set of comments.
type ModelMetadata struct {
	Model string `json:"model,omitempty"`
}

// ActorRef is the actor behind a write. A batch file names an actor by name
// alone, so it decodes from a JSON string; the CLI fills Role from the local
// actors file before sending. A person is sent as a bare role, and the server
// names it after the Aviator user behind the token.
type ActorRef struct {
	Role string `json:"role"`
	Name string `json:"name,omitempty"`
}

// UnmarshalJSON reads an actor's name.
func (a *ActorRef) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return errors.New("actor must be the name of an actor in your local actors file")
	}
	*a = ActorRef{Name: name}
	return nil
}

// DecisionCommentInput is one comment to add, written as the named actor.
type DecisionCommentInput struct {
	Actor ActorRef `json:"actor"`
	Body  string   `json:"body"`
}

// DecisionsResponse is the response from GET and POST
// /api/v1/verify/<n>/decisions: open decisions first for GET, request order
// for POST.
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

// CreateDecisions creates decisions on a runbook, returned in request order.
func (c *Client) CreateDecisions(
	ctx context.Context, runbookNumber int, req CreateDecisionsRequest,
) (*DecisionsResponse, error) {
	var out DecisionsResponse
	if err := c.postJSON(ctx, decisionsPath(runbookNumber), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDecision changes or settles one open decision.
func (c *Client) UpdateDecision(
	ctx context.Context, runbookNumber, decisionID int, req DecisionPatch,
) (*Decision, error) {
	var out Decision
	if err := c.patchJSON(ctx, decisionPath(runbookNumber, decisionID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReopenDecision reopens a settled decision as actor by clearing its outcome,
// the only change a settled decision accepts.
func (c *Client) ReopenDecision(ctx context.Context, runbookNumber, decisionID int, actor ActorRef) (*Decision, error) {
	req := struct {
		Actor   ActorRef `json:"actor"`
		Outcome *string  `json:"outcome"`
	}{Actor: actor}
	var out Decision
	if err := c.patchJSON(ctx, decisionPath(runbookNumber, decisionID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DecisionCommentsResponse is the response from POST
// /api/v1/verify/<n>/decisions/<id>/comments, in request order.
type DecisionCommentsResponse struct {
	Comments []DecisionComment `json:"comments"`
}

// AddDecisionComments adds comments to one decision's thread.
func (c *Client) AddDecisionComments(
	ctx context.Context, runbookNumber, decisionID int, req AddDecisionCommentsRequest,
) (*DecisionCommentsResponse, error) {
	var out DecisionCommentsResponse
	if err := c.postJSON(ctx, decisionPath(runbookNumber, decisionID)+"/comments", req, &out); err != nil {
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
