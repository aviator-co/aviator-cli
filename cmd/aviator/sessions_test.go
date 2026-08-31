package main

import (
	"strings"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
)

func intPtr(n int) *int { return &n }

func TestFormatSessions(t *testing.T) {
	out := formatSessions([]api.SessionSummary{
		{
			RunbookNumber: 42,
			WorkingBranch: "feature/banner",
			PullRequests: []api.LinkedPullRequest{
				{Number: 1201}, {Number: 1202},
			},
		},
		{RunbookNumber: 41},
	}, false, 1)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want a header and 2 rows:\n%s", len(lines), out)
	}
	// Without a header the columns don't say what they are.
	for _, want := range []string{"ID", "BRANCH", "PRS"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("header missing %q: %q", want, lines[0])
		}
	}
	for _, want := range []string{"r/42", "feature/banner", "#1201,#1202"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("row missing %q: %q", want, lines[1])
		}
	}
	// A session with no branch and no PR still gets a row; dropping it would
	// read as the branch having no session.
	if !strings.Contains(lines[2], "r/41") || strings.Count(lines[2], "-") != 2 {
		t.Errorf("unbound session row = %q", lines[2])
	}
}

// A truncated listing has to say which page comes next.
func TestFormatSessionsSaysWhenItIsTruncated(t *testing.T) {
	one := []api.SessionSummary{{RunbookNumber: 1}}
	if out := formatSessions(one, true, 2); !strings.Contains(out, "--page 3") {
		t.Errorf("truncated listing does not name the next page: %q", out)
	}
	if out := formatSessions(one, false, 1); strings.Contains(out, "--page") {
		t.Errorf("complete listing offered a next page anyway: %q", out)
	}
}

func TestNewSessionsJSON(t *testing.T) {
	out := newSessionsJSON([]api.SessionSummary{{
		RunbookNumber:  42,
		URL:            "https://app.aviator.co/r/42",
		WorkingBranch:  "feature/banner",
		RunbookVersion: intPtr(3),
		PullRequests:   []api.LinkedPullRequest{{Number: 1201}},
	}}, true)

	if len(out.Sessions) != 1 || !out.HasMore {
		t.Fatalf("json = %+v", out)
	}
	got := out.Sessions[0]
	if got.ID != "r/42" {
		t.Errorf("id = %q, want r/42", got.ID)
	}
	if len(got.PullRequests) != 1 || got.PullRequests[0] != 1201 {
		t.Errorf("pull_requests = %v", got.PullRequests)
	}
	// `aviator edit` takes this one.
	if got.RunbookVersion == nil || *got.RunbookVersion != 3 {
		t.Errorf("runbook_version = %v", got.RunbookVersion)
	}
}

// An empty listing is still an answer, so it repeats what was searched.
func TestNoSessionsMessage(t *testing.T) {
	sessionsFlags.Repo, sessionsFlags.Status = "acme/web", "active"
	defer func() {
		sessionsFlags.Repo, sessionsFlags.Branch, sessionsFlags.PR = "", "", 0
		sessionsFlags.Status, sessionsFlags.Page = "", 0
	}()

	if got := noSessionsMessage(); got != "No active sessions in acme/web." {
		t.Errorf("message = %q", got)
	}
	sessionsFlags.Branch = "feature/banner"
	if got := noSessionsMessage(); got != "No active sessions on branch feature/banner." {
		t.Errorf("message = %q", got)
	}
	sessionsFlags.Branch, sessionsFlags.PR = "", 1201
	if got := noSessionsMessage(); got != "No active sessions for PR #1201." {
		t.Errorf("message = %q", got)
	}

	// An empty page past the end is not the same as having no sessions, and
	// saying so was the bug.
	sessionsFlags.PR, sessionsFlags.Page = 0, 20
	if got := noSessionsMessage(); !strings.Contains(got, "page 20") ||
		strings.Contains(got, "No active sessions") {
		t.Errorf("message = %q, want it to blame the page rather than the repo", got)
	}
}
