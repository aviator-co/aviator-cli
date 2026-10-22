//go:build experimental

package api

import "context"

const actorsPath = "/api/v1/verify/actors"

// Actor is one of the caller's actors, as returned by /api/v1/verify/actors.
type Actor struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// ListActorsResponse is the response from GET /api/v1/verify/actors.
type ListActorsResponse struct {
	Actors []Actor `json:"actors"`
}

// ListActors fetches the caller's actors.
func (c *Client) ListActors(ctx context.Context) (*ListActorsResponse, error) {
	var out ListActorsResponse
	if err := c.getJSON(ctx, actorsPath, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
