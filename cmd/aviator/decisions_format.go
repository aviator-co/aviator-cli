//go:build experimental

package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/dustin/go-humanize/english"
)

// formatDecisionTable renders decisions one per row. Title is the last column
// so the one unpadded cell carries the long text.
func formatDecisionTable(decisions []api.Decision) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tTYPE\tCOMMENTS\tTITLE")
	for i := range decisions {
		d := &decisions[i]
		fmt.Fprintf(w, "#%d\t%s\t%s\t%d\t%s\n", d.ID, formatDecisionStatus(d), d.Type, len(d.Comments), d.Title)
	}
	_ = w.Flush()
	return b.String()
}

// formatDecisionStatus renders the status with the outcome once settled.
func formatDecisionStatus(d *api.Decision) string {
	if d.Status == "settled" && d.Outcome != "" {
		return "settled (" + d.Outcome + ")"
	}
	return d.Status
}

// formatDecisionDetail renders one decision and its comment thread.
func formatDecisionDetail(d *api.Decision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", colors.Bold(fmt.Sprintf("#%d", d.ID)), colors.Bold(d.Title))
	fmt.Fprintf(&b, "  %s · %s · raised by %s · last changed as %s\n",
		d.Type, formatDecisionStatus(d), d.RaisedBy.Name, formatDecisionAuthor(d.Author))
	if d.Anchor != nil {
		fmt.Fprintf(&b, "  at %s\n", formatDecisionAnchor(d.Anchor))
	}
	if summary := strings.TrimSpace(d.Summary); summary != "" {
		fmt.Fprintf(&b, "\n%s\n", indent(summary, "  "))
	}
	if resolution := strings.TrimSpace(d.Resolution); resolution != "" {
		fmt.Fprintf(&b, "\n  Resolution:\n%s\n", indent(resolution, "    "))
	}
	if len(d.Comments) == 0 {
		return b.String()
	}
	fmt.Fprintf(&b, "\n%s\n", english.Plural(len(d.Comments), "comment", ""))
	for _, c := range d.Comments {
		fmt.Fprintf(&b, "  %s %s as %s %s\n", colors.Faint(fmt.Sprintf("#%d", c.ID)),
			c.User.Name, formatDecisionAuthor(c.Author), colors.Faint(c.CreatedAt))
		fmt.Fprintf(&b, "%s\n", indent(strings.TrimSpace(c.Body), "    "))
	}
	return b.String()
}

// formatDecisionAuthor renders a role, with its model and harness when known.
func formatDecisionAuthor(a api.DecisionAuthor) string {
	var details []string
	for _, s := range []string{a.Model, a.Harness} {
		if s != "" {
			details = append(details, s)
		}
	}
	if len(details) == 0 {
		return a.Role
	}
	return fmt.Sprintf("%s (%s)", a.Role, strings.Join(details, ", "))
}

// formatDecisionAnchor renders an anchor as path:start-end @ short-sha.
func formatDecisionAnchor(a *api.DecisionAnchor) string {
	s := a.FilePath
	switch {
	case a.StartLine > 0 && a.EndLine > a.StartLine:
		s += fmt.Sprintf(":%d-%d", a.StartLine, a.EndLine)
	case a.StartLine > 0:
		s += fmt.Sprintf(":%d", a.StartLine)
	}
	if a.CommitSHA != "" {
		sha := a.CommitSHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		s = strings.TrimSpace(s + " @ " + sha)
	}
	return s
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

type decisionAuthorJSON struct {
	Role    string   `json:"role"`
	Model   string   `json:"model,omitempty"`
	Harness string   `json:"harness,omitempty"`
	Skills  []string `json:"skills,omitempty"`
}

type decisionUserJSON struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type decisionAnchorJSON struct {
	CommitSHA string `json:"commit_sha,omitempty"`
	FilePath  string `json:"file_path,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type decisionCommentJSON struct {
	ID         int                `json:"id"`
	User       decisionUserJSON   `json:"user"`
	Author     decisionAuthorJSON `json:"author"`
	Body       string             `json:"body"`
	CreatedAt  string             `json:"created_at"`
	ModifiedAt string             `json:"modified_at"`
}

type decisionJSON struct {
	ID         int                   `json:"id"`
	Type       string                `json:"type"`
	Title      string                `json:"title"`
	Summary    string                `json:"summary"`
	Status     string                `json:"status"`
	Outcome    string                `json:"outcome"`
	SettledAt  string                `json:"settled_at"`
	Resolution string                `json:"resolution"`
	Anchor     *decisionAnchorJSON   `json:"anchor"`
	RaisedBy   decisionUserJSON      `json:"raised_by"`
	Author     decisionAuthorJSON    `json:"author"`
	Comments   []decisionCommentJSON `json:"comments"`
}

type decisionListJSON struct {
	Session   string         `json:"session"`
	Decisions []decisionJSON `json:"decisions"`
}

func newDecisionJSON(d *api.Decision) decisionJSON {
	out := decisionJSON{
		ID:         d.ID,
		Type:       d.Type,
		Title:      d.Title,
		Summary:    d.Summary,
		Status:     d.Status,
		Outcome:    d.Outcome,
		SettledAt:  d.SettledAt,
		Resolution: d.Resolution,
		RaisedBy:   decisionUserJSON(d.RaisedBy),
		Author:     decisionAuthorJSON(d.Author),
		Comments:   make([]decisionCommentJSON, 0, len(d.Comments)),
	}
	if d.Anchor != nil {
		anchor := decisionAnchorJSON(*d.Anchor)
		out.Anchor = &anchor
	}
	for i := range d.Comments {
		out.Comments = append(out.Comments, newDecisionCommentJSON(&d.Comments[i]))
	}
	return out
}

func newDecisionCommentJSON(c *api.DecisionComment) decisionCommentJSON {
	return decisionCommentJSON{
		ID:         c.ID,
		User:       decisionUserJSON(c.User),
		Author:     decisionAuthorJSON(c.Author),
		Body:       c.Body,
		CreatedAt:  c.CreatedAt,
		ModifiedAt: c.ModifiedAt,
	}
}

func newDecisionListJSON(runbookNumber int, decisions []api.Decision) decisionListJSON {
	out := decisionListJSON{Session: formatRunbookID(runbookNumber), Decisions: make([]decisionJSON, 0, len(decisions))}
	for i := range decisions {
		out.Decisions = append(out.Decisions, newDecisionJSON(&decisions[i]))
	}
	return out
}
