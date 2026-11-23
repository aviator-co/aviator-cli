package api

import (
	"context"
	"fmt"
)

// SessionMember is one PR of a stacked session, bottom of the stack first.
type SessionMember struct {
	PRNumber int     `json:"pr_number"`
	Position int     `json:"position"`
	HeadSHA  *string `json:"head_sha"`
}

// StackedVerifySession is the session shape the stacked endpoints return: the
// PATCH /api/v1/verify/<n> shape plus the session's state and members.
type StackedVerifySession struct {
	VerifySession
	State   *string         `json:"state"`
	Members []SessionMember `json:"members"`
}

// AddVerifyPRRequest is the body for POST /api/v1/verify/<n>/prs. Criteria is
// the full list for every member, replacing the session's criteria; Intent,
// when set, replaces the session's intent the same way.
type AddVerifyPRRequest struct {
	PRNumbers   []int    `json:"pr_numbers"`
	Criteria    []string `json:"criteria"`
	BaseVersion int      `json:"base_version"`
	Intent      string   `json:"intent,omitempty"`
}

// AddVerifyPR adds a PR to a draft stacked session.
func (c *Client) AddVerifyPR(
	ctx context.Context, runbookNumber int, req AddVerifyPRRequest,
) (*StackedVerifySession, error) {
	var out StackedVerifySession
	if err := c.postJSON(ctx, verifyStackPath(runbookNumber, "prs"), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkVerifyReady marks a draft stacked session ready. It starts no run.
func (c *Client) MarkVerifyReady(
	ctx context.Context, runbookNumber int,
) (*StackedVerifySession, error) {
	var out StackedVerifySession
	if err := c.postJSON(ctx, verifyStackPath(runbookNumber, "ready"), struct{}{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveVerifyPRsRequest is the body for POST /api/v1/verify/<n>/prs/remove.
// Criteria is the full list for the PRs that stay, replacing the session's
// criteria; Intent, when set, replaces the session's intent the same way.
type RemoveVerifyPRsRequest struct {
	PRNumbers   []int    `json:"pr_numbers"`
	Criteria    []string `json:"criteria"`
	BaseVersion int      `json:"base_version"`
	Intent      string   `json:"intent,omitempty"`
}

// RemoveVerifyPRs takes PRs out of a stacked session, which stays open.
func (c *Client) RemoveVerifyPRs(
	ctx context.Context, runbookNumber int, req RemoveVerifyPRsRequest,
) (*StackedVerifySession, error) {
	var out StackedVerifySession
	if err := c.postJSON(ctx, verifyStackPath(runbookNumber, "prs/remove"), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// NewStackedVerifyRequest is the body for POST /api/v1/verify/stack.
// FromRunbookNumber links the new session to the one its PRs were removed from.
type NewStackedVerifyRequest struct {
	Repository        Repository `json:"repository"`
	PRNumbers         []int      `json:"pr_numbers"`
	Intent            string     `json:"intent"`
	Criteria          []string   `json:"criteria"`
	FromRunbookNumber *int       `json:"from_runbook_number,omitempty"`
}

// NewStackedVerify opens a ready stacked session holding the PRs. It starts
// no run.
func (c *Client) NewStackedVerify(
	ctx context.Context, req NewStackedVerifyRequest,
) (*StackedVerifySession, error) {
	var out StackedVerifySession
	if err := c.postJSON(ctx, "/api/v1/verify/stack", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func verifyStackPath(runbookNumber int, action string) string {
	return fmt.Sprintf("/api/v1/verify/%d/%s", runbookNumber, action)
}
