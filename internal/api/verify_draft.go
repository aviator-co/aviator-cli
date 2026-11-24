package api

import "context"

// MarkVerifyDraftRequest is the body for POST /api/v1/verify/<n>/draft, which
// takes no fields.
type MarkVerifyDraftRequest struct{}

// MarkVerifyDraft moves a ready stacked session back to draft. A running
// verification finishes.
func (c *Client) MarkVerifyDraft(
	ctx context.Context, runbookNumber int,
) (*StackedVerifySession, error) {
	var out StackedVerifySession
	if err := c.postJSON(ctx, verifyStackPath(runbookNumber, "draft"), MarkVerifyDraftRequest{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
