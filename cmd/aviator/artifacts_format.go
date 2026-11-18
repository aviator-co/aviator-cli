package main

import "github.com/aviator-co/aviator-cli/internal/api"

type artifactReferenceJSON struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

type artifactJSON struct {
	ID         int                     `json:"id"`
	Category   string                  `json:"category"`
	Title      string                  `json:"title"`
	Diagram    *string                 `json:"diagram,omitempty"`
	Body       *string                 `json:"body,omitempty"`
	References []artifactReferenceJSON `json:"references"`
	PRNumbers  []int                   `json:"pr_numbers,omitzero"`
}

type artifactSetJSON struct {
	ID        int                 `json:"id"`
	Summary   *string             `json:"summary,omitempty"`
	HeadSHA   *string             `json:"head_sha,omitempty"`
	Author    *decisionAuthorJSON `json:"author"`
	CreatedAt string              `json:"created_at"`
	Artifacts []artifactJSON      `json:"artifacts"`
}

type artifactSetResponseJSON struct {
	ID          string           `json:"id"`
	ArtifactSet *artifactSetJSON `json:"artifact_set"`
}

func newArtifactJSON(a *api.Artifact) artifactJSON {
	out := artifactJSON{
		ID:         a.ID,
		Category:   a.Category,
		Title:      a.Title,
		Diagram:    a.Diagram,
		Body:       a.Body,
		References: make([]artifactReferenceJSON, 0, len(a.References)),
		PRNumbers:  a.PRNumbers,
	}
	for _, r := range a.References {
		out.References = append(out.References, artifactReferenceJSON(r))
	}
	return out
}

func newArtifactSetJSON(s *api.ArtifactSet) *artifactSetJSON {
	if s == nil {
		return nil
	}
	out := &artifactSetJSON{
		ID:        s.ID,
		Summary:   s.Summary,
		HeadSHA:   s.HeadSHA,
		Author:    newDecisionAuthorJSON(s.Author),
		CreatedAt: s.CreatedAt,
		Artifacts: make([]artifactJSON, 0, len(s.Artifacts)),
	}
	for i := range s.Artifacts {
		out.Artifacts = append(out.Artifacts, newArtifactJSON(&s.Artifacts[i]))
	}
	return out
}

func newArtifactSetResponseJSON(runbookNumber int, s *api.ArtifactSet) artifactSetResponseJSON {
	return artifactSetResponseJSON{ID: formatRunbookID(runbookNumber), ArtifactSet: newArtifactSetJSON(s)}
}
