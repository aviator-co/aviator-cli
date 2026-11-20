package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
)

// formatArtifactSet renders the set's summary, when it has one, and one row
// per artifact. Title is the last column so the one unpadded cell carries the
// long text.
func formatArtifactSet(runbookNumber int, s *api.ArtifactSet) string {
	var b strings.Builder
	b.WriteString(colors.Bold(formatRunbookID(runbookNumber)))
	if summary := strings.TrimSpace(deref(s.Summary)); summary != "" {
		b.WriteString(" " + strings.ReplaceAll(summary, "\n", "\n  "))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  %s\n\n", colors.Faint(formatArtifactSetOrigin(s)))
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCATEGORY\tDIAGRAM\tBODY\tREFS\tTITLE")
	for i := range s.Artifacts {
		a := &s.Artifacts[i]
		fmt.Fprintf(w, "#%d\t%s\t%s\t%s\t%d\t%s\n", a.ID, a.Category,
			yesNo(strings.TrimSpace(deref(a.Diagram)) != ""), yesNo(strings.TrimSpace(deref(a.Body)) != ""),
			len(a.References), a.Title)
	}
	_ = w.Flush()
	return b.String()
}

// formatArtifactSetOrigin says when, by whom, and at which commit the set
// was submitted.
func formatArtifactSetOrigin(s *api.ArtifactSet) string {
	parts := []string{"submitted " + s.CreatedAt}
	if s.Author != nil {
		parts = append(parts, "as "+formatDecisionAuthor(*s.Author))
	}
	if sha := deref(s.HeadSHA); sha != "" {
		parts = append(parts, "at "+shortSHA(sha))
	}
	return strings.Join(parts, " · ")
}

// formatArtifactDetail renders one artifact of set in full. The diagram is
// printed unindented in a mermaid fence so it can be copied out as is.
func formatArtifactDetail(set *api.ArtifactSet, a *api.Artifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", colors.Bold(fmt.Sprintf("#%d", a.ID)), colors.Bold(a.Title))
	parts := []string{a.Category}
	if len(a.PRNumbers) > 0 {
		prs := make([]string, 0, len(a.PRNumbers))
		for _, n := range a.PRNumbers {
			prs = append(prs, fmt.Sprintf("#%d", n))
		}
		parts = append(parts, "PRs "+strings.Join(prs, ", "))
	}
	fmt.Fprintf(&b, "  %s\n", strings.Join(parts, " · "))
	fmt.Fprintf(&b, "  %s\n", colors.Faint(formatArtifactSetOrigin(set)))
	if len(a.References) > 0 {
		b.WriteString("\nReferences:\n")
		for _, r := range a.References {
			fmt.Fprintf(&b, "  %s\n", r.Target)
		}
	}
	if body := strings.TrimSpace(deref(a.Body)); body != "" {
		fmt.Fprintf(&b, "\nBody:\n%s\n", indent(body, "  "))
	}
	if diagram := strings.TrimSpace(deref(a.Diagram)); diagram != "" {
		fmt.Fprintf(&b, "\nDiagram:\n```mermaid\n%s\n```\n", diagram)
	}
	return b.String()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

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

// artifactShowJSON is one artifact with the context of the set it came from,
// under the same names list --json gives them inside artifact_set.
type artifactShowJSON struct {
	ID        string              `json:"id"`
	HeadSHA   *string             `json:"head_sha,omitempty"`
	Author    *decisionAuthorJSON `json:"author"`
	CreatedAt string              `json:"created_at"`
	Artifact  artifactJSON        `json:"artifact"`
}

func newArtifactShowJSON(runbookNumber int, set *api.ArtifactSet, a *api.Artifact) artifactShowJSON {
	return artifactShowJSON{
		ID:        formatRunbookID(runbookNumber),
		HeadSHA:   set.HeadSHA,
		Author:    newDecisionAuthorJSON(set.Author),
		CreatedAt: set.CreatedAt,
		Artifact:  newArtifactJSON(a),
	}
}
