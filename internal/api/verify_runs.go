package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

type listVerifyRunsResponse struct {
	Runs    []VerificationRun `json:"runs"`
	HasMore bool              `json:"has_more"`
}

// ListVerifyRuns returns a session's newest verification runs and whether older ones follow.
func (c *Client) ListVerifyRuns(
	ctx context.Context, runbookNumber, limit int,
) ([]VerificationRun, bool, error) {
	path := fmt.Sprintf("/api/v1/verify/%d/runs", runbookNumber)
	query := url.Values{"per_page": {strconv.Itoa(limit)}}
	var out listVerifyRunsResponse
	if err := c.getJSON(ctx, path, query, &out); err != nil {
		return nil, false, err
	}
	return out.Runs, out.HasMore, nil
}
