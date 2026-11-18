package api

import (
	"context"
	"fmt"
	"net/http"
)

// ArtifactReference points an artifact at code. Type is file; Target is a
// path, path:40, or path:40-48.
type ArtifactReference struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

// Artifact describes one part of how a change is built: a Mermaid diagram, a
// markdown body, or both, with the files it's about.
type Artifact struct {
	ID         int                 `json:"id"`
	Category   string              `json:"category"`
	Title      string              `json:"title"`
	Diagram    *string             `json:"diagram"`
	Body       *string             `json:"body"`
	References []ArtifactReference `json:"references"`
	PRNumbers  []int               `json:"pr_numbers"`
}

// ArtifactSet is one submission of a session's artifacts. Author is who
// submitted it, the same shape decisions carry.
type ArtifactSet struct {
	ID        int             `json:"id"`
	Summary   *string         `json:"summary"`
	HeadSHA   *string         `json:"head_sha"`
	Author    *DecisionAuthor `json:"author"`
	CreatedAt string          `json:"created_at"`
	Artifacts []Artifact      `json:"artifacts"`
}

// ArtifactSetResponse wraps a session's current artifact set, null on a GET
// before anything was submitted.
type ArtifactSetResponse struct {
	ArtifactSet *ArtifactSet `json:"artifact_set"`
}

// SubmitArtifacts sends body, a whole artifact set, to PUT
// /api/v1/verify/<n>/artifacts as is. The server's strict models validate it,
// and a Go round trip would drop the unknown keys they reject.
func (c *Client) SubmitArtifacts(ctx context.Context, runbookNumber int, body []byte) (*ArtifactSetResponse, error) {
	var out ArtifactSetResponse
	if err := c.doPayload(ctx, http.MethodPut, artifactsPath(runbookNumber), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetArtifacts fetches a session's current artifact set. ArtifactSet is nil
// when nothing has been submitted.
func (c *Client) GetArtifacts(ctx context.Context, runbookNumber int) (*ArtifactSetResponse, error) {
	var out ArtifactSetResponse
	if err := c.getJSON(ctx, artifactsPath(runbookNumber), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func artifactsPath(runbookNumber int) string {
	return fmt.Sprintf("/api/v1/verify/%d/artifacts", runbookNumber)
}
