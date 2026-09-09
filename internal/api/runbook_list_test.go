package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListSessions(t *testing.T) {
	const body = `{
		"runbooks": [
			{
				"runbook_number": 42,
				"url": "https://app.aviator.co/r/42",
				"working_branch": "feature/banner",
				"target_branch": "main",
				"status": "active",
				"intent": "Gate the banner behind the beta flag",
				"created_at": "2026-08-25T15:11:10Z",
				"runbook_version": 3,
				"pull_requests": [{"number": 1201, "url": "https://github.com/acme/web/pull/1201", "step_numbers": []}]
			},
			{
				"runbook_number": 41,
				"url": "https://app.aviator.co/r/41",
				"working_branch": null,
				"target_branch": null,
				"status": "active",
				"intent": null,
				"created_at": "2026-08-24T09:00:00Z",
				"runbook_version": null,
				"pull_requests": []
			}
		],
		"page": 1,
		"per_page": 20,
		"has_more": true
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/runbook/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		for param, want := range map[string]string{
			"org":            "acme",
			"repo":           "web",
			"working_branch": "feature/banner",
			"status":         "active",
			"page":           "1",
			"per_page":       "2",
		} {
			if got := q.Get(param); got != want {
				t.Errorf("%s = %q, want %q", param, got, want)
			}
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	sessions, hasMore, err := newTestClient(srv).ListSessions(
		context.Background(), ListSessionsParams{
			Repository:    Repository{Org: "acme", Name: "web"},
			WorkingBranch: "feature/banner",
			Status:        "active",
			Limit:         2,
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %+v", sessions)
	}
	if !hasMore {
		t.Error("has_more = false, want true")
	}

	first := sessions[0]
	if first.RunbookNumber != 42 || first.WorkingBranch != "feature/banner" {
		t.Errorf("first session = %+v", first)
	}
	if first.RunbookVersion == nil || *first.RunbookVersion != 3 {
		t.Errorf("runbook_version = %v", first.RunbookVersion)
	}
	if !first.HasPullRequest(1201) || first.HasPullRequest(1202) {
		t.Errorf("pull_requests = %+v", first.PullRequests)
	}

	// A session with no branch, intent or runbook version yet comes back as
	// JSON nulls, which must not fail the decode.
	second := sessions[1]
	if second.WorkingBranch != "" || second.Intent != "" || second.RunbookVersion != nil {
		t.Errorf("second session = %+v", second)
	}
}

// A page is one request for the same page number, and a limit past the API's
// page size is capped rather than walked, so every page stays reachable.
func TestListSessionsAsksForThePageDirectly(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		if q.Get("page") != "3" || q.Get("per_page") != fmt.Sprint(maxPageSize) {
			t.Errorf("page = %q, per_page = %q, want 3 and %d",
				q.Get("page"), q.Get("per_page"), maxPageSize)
		}
		_, _ = w.Write([]byte(`{"runbooks":[{"runbook_number":9}],"has_more":false}`))
	}))
	defer srv.Close()

	sessions, hasMore, err := newTestClient(srv).ListSessions(
		context.Background(), ListSessionsParams{
			Repository: Repository{Org: "acme", Name: "web"},
			Page:       3,
			Limit:      2000,
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 1 || len(sessions) != 1 || hasMore {
		t.Errorf("requests = %d, sessions = %d, hasMore = %v", requests, len(sessions), hasMore)
	}
}

// The listing has no PR filter, so the match happens over the pages.
func TestFindSessionsForPullRequestPages(t *testing.T) {
	pages := []string{
		`{"runbooks":[{"runbook_number":9,"pull_requests":[{"number":7}]}],"has_more":true}`,
		`{"runbooks":[{"runbook_number":8,"pull_requests":[{"number":12}]}],"has_more":false}`,
	}
	var requested []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		requested = append(requested, q.Get("page"))
		if got := q.Get("per_page"); got != fmt.Sprint(maxPageSize) {
			t.Errorf("per_page = %q, want %d", got, maxPageSize)
		}
		if q.Has("working_branch") {
			t.Errorf("branch filter sent for a PR search: %q", q.Get("working_branch"))
		}
		page := len(requested) - 1
		_, _ = w.Write([]byte(pages[page]))
	}))
	defer srv.Close()

	found, err := newTestClient(srv).FindSessionsForPullRequest(
		context.Background(), Repository{Org: "acme", Name: "web"}, 12, "active",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(requested) != 2 || requested[0] != "1" || requested[1] != "2" {
		t.Errorf("pages requested = %v", requested)
	}
	if len(found) != 1 || found[0].RunbookNumber != 8 {
		t.Errorf("found = %+v", found)
	}
}

// Paging stops as soon as the server says there is nothing more, so a repo
// with one page costs one request.
func TestFindSessionsForPullRequestStopsAtLastPage(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"runbooks":[],"has_more":false}`))
	}))
	defer srv.Close()

	found, err := newTestClient(srv).FindSessionsForPullRequest(
		context.Background(), Repository{Org: "acme", Name: "web"}, 12, "active",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1", requests)
	}
	if len(found) != 0 {
		t.Errorf("found = %+v", found)
	}
}
