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
	PRNumber    int      `json:"pr_number"`
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

// ConfirmVerifyRequest is the body for POST /api/v1/verify/<n>/confirm.
type ConfirmVerifyRequest struct {
	Run bool `json:"run"`
}

// ConfirmVerifyResponse is the confirmed session, with the first run when one
// was started.
type ConfirmVerifyResponse struct {
	StackedVerifySession
	RunID     *int   `json:"run_id"`
	RunStatus string `json:"run_status"`
}

// ConfirmVerify makes a draft stacked session active.
func (c *Client) ConfirmVerify(
	ctx context.Context, runbookNumber int, req ConfirmVerifyRequest,
) (*ConfirmVerifyResponse, error) {
	var out ConfirmVerifyResponse
	if err := c.postJSON(ctx, verifyStackPath(runbookNumber, "confirm"), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RegroupGroup is one new session of a regroup: its PRs, intent, full
// criteria, and the decisions that move to it.
type RegroupGroup struct {
	PRNumbers   []int    `json:"pr_numbers"`
	Intent      string   `json:"intent"`
	Criteria    []string `json:"criteria"`
	DecisionIDs []int    `json:"decision_ids"`
}

// RegroupVerifyRequest is the body for POST /api/v1/verify/<n>/regroup.
type RegroupVerifyRequest struct {
	Groups []RegroupGroup `json:"groups"`
}

// RegroupVerifyResponse lists the sessions that replace the archived one.
type RegroupVerifyResponse struct {
	Sessions []StackedVerifySession `json:"sessions"`
}

// RegroupVerify replaces a stacked session with one session per group.
func (c *Client) RegroupVerify(
	ctx context.Context, runbookNumber int, req RegroupVerifyRequest,
) (*RegroupVerifyResponse, error) {
	var out RegroupVerifyResponse
	if err := c.postJSON(ctx, verifyStackPath(runbookNumber, "regroup"), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func verifyStackPath(runbookNumber int, action string) string {
	return fmt.Sprintf("/api/v1/verify/%d/%s", runbookNumber, action)
}
