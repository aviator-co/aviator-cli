package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 27, 15, 4, 5, 0, time.FixedZone("PDT", -7*3600))
	good := map[string]sinceWindow{
		"":           {Label: "all time"},
		"all":        {Label: "all time"},
		" all ":      {Label: "all time"}, //nolint:gocritic // the padding is what this case asserts gets trimmed
		"1d":         {Param: "2026-10-26T22:04:05Z", Label: "last 1 day"},
		"30d":        {Param: "2026-09-27T22:04:05Z", Label: "last 30 days"},
		"2026-07-29": {Param: "2026-07-29T00:00:00Z", Label: "since 2026-07-29"},
	}
	for in, want := range good {
		got, err := parseSince(in, now)
		if err != nil {
			t.Errorf("parseSince(%q) error: %v", in, err)
		} else if got != want {
			t.Errorf("parseSince(%q) = %+v, want %+v", in, got, want)
		}
	}
	// A Duration multiply overflows past ~106,000 days; calendar arithmetic does not.
	if got, err := parseSince("200000d", now); err != nil || got.Param != now.UTC().AddDate(0, 0, -200000).Format(time.RFC3339) {
		t.Errorf("parseSince(200000d) = %+v, %v", got, err)
	}
	for _, in := range []string{"0d", "-3d", "d", "30", "abc", "2026-13-01", "2026-7-29", "2026-07-29T00:00:00Z"} {
		if _, err := parseSince(in, now); err == nil {
			t.Errorf("parseSince(%q) expected error", in)
		}
	}
}

// TestAnalyticsError covers the older-backend mapping: a bare 404 or 405
// becomes the "not supported yet" message, while a JSON not-found (a real
// unknown invariant) and everything else pass through untouched.
func TestAnalyticsError(t *testing.T) {
	bare := &api.Error{
		Status: http.StatusNotFound, Message: "Page not found",
		Method: http.MethodGet, Path: "/api/v1/invariants/insights",
	}
	want := "this Aviator backend doesn't support invariant analytics yet; GET /api/v1/invariants/insights returned 404"
	if got := analyticsError(bare).Error(); got != want {
		t.Errorf("bare 404 = %q, want %q", got, want)
	}
	wrapped := errors.Wrap(bare, "listing failed")
	if got := analyticsError(wrapped).Error(); got != want {
		t.Errorf("wrapped bare 404 = %q, want %q", got, want)
	}

	// A backend without GET on /<id> answers 405 from its PATCH/DELETE rule.
	methodNotAllowed := &api.Error{
		Status: http.StatusMethodNotAllowed, Method: http.MethodGet, Path: "/api/v1/invariants/42",
	}
	if got := analyticsError(methodNotAllowed).Error(); !strings.HasSuffix(got, "; GET /api/v1/invariants/42 returned 405") {
		t.Errorf("bare 405 = %q", got)
	}

	notFound := &api.Error{
		Status: http.StatusNotFound, Code: "not-found", Message: "Baseline invariant not found.",
		Method: http.MethodGet, Path: "/api/v1/invariants/999/reviews",
	}
	if got := analyticsError(notFound); got != notFound {
		t.Errorf("JSON not-found should pass through, got %v", got)
	}
	coded405 := &api.Error{Status: http.StatusMethodNotAllowed, Code: "method-not-allowed"}
	if got := analyticsError(coded405); got != coded405 {
		t.Errorf("a coded 405 should pass through, got %v", got)
	}
	forbidden := &api.Error{Status: http.StatusForbidden, Message: "needs a user token"}
	if got := analyticsError(forbidden); got != forbidden {
		t.Errorf("403 should pass through, got %v", got)
	}
	plain := errors.New("request failed")
	if got := analyticsError(plain); got != plain {
		t.Errorf("a non-API error should pass through, got %v", got)
	}
}

// TestUsageOnlyFlags covers the list flags that only mean something with
// --usage: each is refused on its own, and the defaults pass.
func TestUsageOnlyFlags(t *testing.T) {
	previous := invariantsListFlags
	t.Cleanup(func() { invariantsListFlags = previous })

	invariantsListFlags.Sort, invariantsListFlags.NeverApplied = "", false
	if usage, err := applyUsageFlags(&api.ListInvariantsQuery{}, false); err != nil || usage != nil {
		t.Errorf("nothing passed: usage = %+v, err = %v", usage, err)
	}
	for name, set := range map[string]func(){
		"sort":          func() { invariantsListFlags.Sort = "applied" },
		"never-applied": func() { invariantsListFlags.NeverApplied = true },
	} {
		invariantsListFlags.Sort, invariantsListFlags.NeverApplied = "", false
		set()
		_, err := applyUsageFlags(&api.ListInvariantsQuery{}, false)
		if err == nil || !strings.Contains(err.Error(), "--"+name) || !strings.Contains(err.Error(), "--usage") {
			t.Errorf("--%s without --usage: err = %v", name, err)
		}
	}
}

// TestApplyUsageFlags pins what --usage puts on the query: active when no
// status was given, and the sort as given.
func TestApplyUsageFlags(t *testing.T) {
	previous := invariantsListFlags
	t.Cleanup(func() { invariantsListFlags = previous })

	invariantsListFlags.Sort, invariantsListFlags.NeverApplied = "failure_rate", false
	query := api.ListInvariantsQuery{Page: 2, PerPage: 50}
	usage, err := applyUsageFlags(&query, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Status != "active" || !query.IncludeUsage || query.Sort != "failure_rate" || query.NeverApplied ||
		query.Page != 2 || query.PerPage != 50 {
		t.Errorf("query = %+v", query)
	}
	if *usage != (invariantListUsage{Status: "active"}) {
		t.Errorf("footer = %+v", usage)
	}

	// An explicit status is kept, so pending drafts can be inspected too.
	invariantsListFlags.Sort, invariantsListFlags.NeverApplied = "", true
	query = api.ListInvariantsQuery{Status: "pending"}
	if usage, err = applyUsageFlags(&query, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Status != "pending" || query.Sort != "" || !query.NeverApplied || usage.Status != "pending" {
		t.Errorf("query = %+v, footer = %+v", query, usage)
	}
}

func TestRequireUsage(t *testing.T) {
	withUsage := api.Invariant{Usage: &api.InvariantUsage{}}
	if err := requireUsage([]api.Invariant{withUsage}); err != nil {
		t.Errorf("a row with usage is supported, got %v", err)
	}
	if err := requireUsage([]api.Invariant{withUsage, {}}); !errors.Is(err, errNoUsage) {
		t.Errorf("a backend without usage should be refused, got %v", err)
	}
}

func rate(r float64) *float64 { return &r }

// TestFormatInvariantListUsage covers the --usage listing: the provenance
// columns give way to the counts, a null rate is an empty cell, the reason
// row still lands under TITLE, and the footer says which invariants and
// window the counts cover.
func TestFormatInvariantListUsage(t *testing.T) {
	resp := &api.ListInvariantsResponse{
		Page:    1,
		HasMore: true,
		Invariants: []api.Invariant{
			{
				ID: 42, Title: "HTTP handlers authenticate", Category: "security", Status: "active", Enabled: true,
				Usage: &api.InvariantUsage{TimesApplied: 143, TimesFailed: 31, FailureRate: rate(0.217), TotalWaives: 18, WaiveRate: rate(0.581)},
			},
			{
				ID: 71, Title: "Flags have a ticket", Category: "code_style", Status: "active", Enabled: true,
				Reason: "Reviewers keep asking.",
				Usage:  &api.InvariantUsage{},
			},
		},
	}
	out := formatInvariantList(resp, &invariantListUsage{Status: "active"})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("want header, 2 rows, 1 reason row, footer; got %d lines:\n%s", len(lines), out)
	}
	if want := "ID   STATUS  CATEGORY    APPLIED  FAILED  RATE  WAIVED  TITLE"; lines[0] != want {
		t.Errorf("header = %q, want %q", lines[0], want)
	}
	if want := "#42  active  security    143      31      22%   18      HTTP handlers authenticate"; lines[1] != want {
		t.Errorf("row = %q, want %q", lines[1], want)
	}
	if want := "#71  active  code_style  0        0       -     0       Flags have a ticket"; lines[2] != want {
		t.Errorf("row = %q, want %q", lines[2], want)
	}
	titleCol := strings.Index(lines[0], "TITLE")
	if got := strings.Index(lines[3], "reason: Reviewers"); got != titleCol {
		t.Errorf("reason starts at column %d, want %d:\n%s", got, titleCol, out)
	}
	if lines[4] != "Active invariants · all time · See more invariants --page 2" {
		t.Errorf("footer = %q", lines[4])
	}
	for _, absent := range []string{"SOURCE", "SCOPE", "OWNER"} {
		if strings.Contains(out, absent) {
			t.Errorf("usage table should drop %s:\n%s", absent, out)
		}
	}

	resp.HasMore = false
	out = formatInvariantList(resp, &invariantListUsage{Status: "pending"})
	if !strings.HasSuffix(out, "Pending invariants · all time\n") {
		t.Errorf("last page footer:\n%s", out)
	}
}

func TestInvariantJSONUsage(t *testing.T) {
	got := decodeJSON(t, newInvariantJSON(&api.Invariant{ID: 7}))
	if _, ok := got["usage"]; ok {
		t.Errorf("usage should be omitted from a plain row, got %v", got["usage"])
	}
	// Like owner_teams, a missing version is null rather than absent.
	if v, ok := got["version"]; !ok || v != nil {
		t.Errorf("version = %v (present %t), want null", v, ok)
	}
	got = decodeJSON(t, newInvariantJSON(&api.Invariant{
		ID: 7, Version: ptr(3),
		Usage: &api.InvariantUsage{
			TimesApplied: 12, TimesFailed: 5, FailureRate: rate(0.417), TotalWaives: 3, WaivedFailures: 2,
			WaivesByCategory: api.WaiveCounts{FalsePositive: 3},
		},
	}))
	if got["version"] != float64(3) {
		t.Errorf("version = %v", got["version"])
	}
	usage, ok := got["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage = %v", got["usage"])
	}
	counts, ok := usage["waives_by_category"].(map[string]any)
	if !ok || counts["false_positive"] != float64(3) || len(counts) != 4 {
		t.Errorf("waives_by_category = %v", usage["waives_by_category"])
	}
	delete(usage, "waives_by_category")
	assertJSONFields(t, usage, map[string]any{
		"times_applied": float64(12), "times_failed": float64(5), "failure_rate": 0.417,
		"total_waives": float64(3), "waived_failures": float64(2), "waive_rate": nil,
	})
}

var insightsResponse = &api.InvariantInsightsResponse{
	Status: "active",
	MostApplied: []api.InvariantAppliedRow{{
		Invariant: api.InvariantSummary{ID: 42, Title: "HTTP handlers authenticate"}, TimesApplied: 143, TimesFailed: 31, TotalWaives: 18,
	}},
	FailureRatio: []api.InvariantFailureRow{{
		Invariant: api.InvariantSummary{ID: 58, Title: "Background jobs are idempotent"}, TimesApplied: 12, TimesFailed: 5, FailureRate: rate(0.417),
	}},
	MostWaived: []api.InvariantWaivedRow{{
		Invariant: api.InvariantSummary{ID: 42, Title: "HTTP handlers authenticate"}, Total: 18,
		Counts: api.WaiveCounts{FalsePositive: 11, DoesntApply: 4, AcceptedRisk: 2, FixInFollowup: 1},
	}},
	NeverApplied: api.InvariantNeverApplied{
		Count: 7,
		Invariants: []api.NeverAppliedInvariant{{
			InvariantSummary: api.InvariantSummary{ID: 71, Title: "Feature flags have a removal ticket"},
			CreatedAt:        "2026-09-02T10:00:00Z",
		}},
	},
	Reasons: api.InvariantWaiveReasons{Total: 61, Counts: api.WaiveCounts{FalsePositive: 34, DoesntApply: 12, AcceptedRisk: 10, FixInFollowup: 5}},
}

func TestFormatInvariantInsights(t *testing.T) {
	out := formatInvariantInsights(insightsResponse, "last 30 days", "all repos", "aviator invariants list --usage --never-applied")
	for _, want := range []string{
		"Invariant insights · active invariants · last 30 days · all repos\n",
		"\nMost applied\nID   APPLIED  FAILED  WAIVED  TITLE\n#42  143      31      18      HTTP handlers authenticate\n",
		"\nHighest failure rate\nID   APPLIED  FAILED  RATE  TITLE\n#58  12       5       42%   Background jobs are idempotent\n",
		"\nMost waived, by reason\nID   TOTAL  FALSE+  N/A  RISK  FOLLOWUP  TITLE\n#42  18     11      4    2     1         HTTP handlers authenticate\n",
		// An empty table says so rather than printing a bare header.
		"\nHighest waive rate\nnone\n",
		"\nNever applied (7)\nID   CREATED     TITLE\n#71  2026-09-02  Feature flags have a removal ticket\n" +
			"See all: aviator invariants list --usage --never-applied\n",
		"\nWaiver reasons: 61 · false positive 34 · doesn't apply 12 · accepted risk 10 · fix in follow-up 5\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}

	// Once the table holds every never-applied invariant there is nothing
	// more to see.
	whole := *insightsResponse
	whole.NeverApplied.Count = 1
	if out := formatInvariantInsights(&whole, "all time", "acme/web", "hint"); strings.Contains(out, "See all") ||
		!strings.Contains(out, "· all time · acme/web") {
		t.Errorf("output = %s", out)
	}
}

// TestNeverAppliedHint pins that the hinted listing covers the same repo and
// status the never-applied count does.
func TestNeverAppliedHint(t *testing.T) {
	base := "aviator invariants list --usage --never-applied"
	if got := neverAppliedHint(nil, ""); got != base {
		t.Errorf("hint = %q, want %q", got, base)
	}
	got := neverAppliedHint(&api.Repository{Org: "acme", Name: "web"}, "pending")
	if want := base + " --repo acme/web --status pending"; got != want {
		t.Errorf("hint = %q, want %q", got, want)
	}
}

var showDetail = &api.InvariantDetail{
	Invariant: api.Invariant{
		ID: 42, Title: "HTTP handlers authenticate", Category: "security", Status: "active", Enabled: true,
		Source: "manual", Body: "Call the auth middleware first.", Version: ptr(3),
		OwnerTeams: &[]api.OwnerTeam{{Org: "acme", Slug: "platform"}},
		Usage: &api.InvariantUsage{
			TimesApplied: 143, TimesFailed: 31, FailureRate: rate(0.217), TotalWaives: 18, WaivedFailures: 18, WaiveRate: rate(0.581),
			WaivesByCategory: api.WaiveCounts{FalsePositive: 11, DoesntApply: 4, AcceptedRisk: 2, FixInFollowup: 1},
		},
	},
	ByVersion: []api.InvariantVersionUsage{
		{
			Version: ptr(3), CreatedAt: ptr("2026-10-02T09:00:00Z"),
			InvariantUsage: api.InvariantUsage{TimesApplied: 40, TimesFailed: 4, FailureRate: rate(0.1), TotalWaives: 1},
		},
		{InvariantUsage: api.InvariantUsage{TimesApplied: 25, TimesFailed: 6, FailureRate: rate(0.24), TotalWaives: 3}},
	},
}

var waiverRows = []api.InvariantWaiverRow{{
	ID: 9,
	InvariantWaiver: api.InvariantWaiver{
		Category: "false_positive", Justification: "Endpoint is internal-only,\nbehind the VPN gate.",
		WaivedBy:  &api.InvariantUser{Email: "dana@acme.com", FullName: "Dana K."},
		CreatedAt: "2026-10-21T10:00:00Z", Version: ptr(2),
	},
	Review: api.InvariantReview{
		RunbookNumber: 1288, URL: "https://app.aviator.co/r/1288",
		Repositories: []api.Repository{{Org: "acme", Name: "web"}},
		PullRequests: []api.LinkedPullRequest{{Number: 4821, URL: "https://github.com/acme/web/pull/4821", StepNumbers: []string{"1"}}},
	},
	Invariant: api.InvariantSummary{ID: 42, Title: "HTTP handlers authenticate"},
}}

func TestFormatInvariantShow(t *testing.T) {
	out := formatInvariantShow(showDetail, &api.InvariantWaiversResponse{Waivers: waiverRows, Page: 1, HasMore: true})
	for _, want := range []string{
		"#42 HTTP handlers authenticate\n  Category: security\n",
		"  Owner: @acme/platform\n  Version: 3\n  Body: Call the auth middleware first.\n",
		"\nUsage · all time\n  Applied 143 · Failed 31 (22%) · Waived 18 of 31 failures (58%)\n" +
			"  Waived as: false positive 11 · doesn't apply 4 · accepted risk 2 · fix in follow-up 1\n",
		"\nBy version\n  VERSION  SINCE       APPLIED  FAILED  RATE  WAIVED\n" +
			"  3        2026-10-02  40       4       10%   1\n" +
			"  -        before      25       6       24%   3\n",
		"\nWaivers (18)\n  WHEN        REVIEW  PR             REASON          BY       JUSTIFICATION\n" +
			"  2026-10-21  r/1288  acme/web#4821  false positive  Dana K.  Endpoint is internal-only, behind the VPN gate.\n" +
			"  See more waivers --page 2\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	// The title is the heading, not a field, and by-repo only shows when asked.
	if strings.Contains(out, "Title:") || strings.Contains(out, "By repo") {
		t.Errorf("output = %s", out)
	}

	withRepos := *showDetail
	withRepos.ByRepo = []api.InvariantRepoUsage{{
		Repository:     api.Repository{Org: "acme", Name: "web"},
		InvariantUsage: api.InvariantUsage{TimesApplied: 100, TimesFailed: 28, FailureRate: rate(0.28), TotalWaives: 17},
	}}
	out = formatInvariantShow(&withRepos, &api.InvariantWaiversResponse{Page: 1})
	for _, want := range []string{
		"\nUsage · all time\n",
		"\nBy repo\n  REPO      APPLIED  FAILED  RATE  WAIVED\n  acme/web  100      28      28%   17\n",
		"\nWaivers (18)\n  none\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "See more") {
		t.Errorf("last page should have no footer:\n%s", out)
	}

	// Past the last waiver the empty page says so instead of "none".
	out = formatInvariantShow(showDetail, &api.InvariantWaiversResponse{Page: 4})
	if want := "\nWaivers (18)\n  Nothing on page 4, the listing ends before it. Try a lower --page.\n"; !strings.HasSuffix(out, want) {
		t.Errorf("output missing %q\n---\n%s", want, out)
	}

	// Nothing applied yet: no rates, no waive breakdown, an empty version table.
	bare := &api.InvariantDetail{Invariant: api.Invariant{ID: 5, Title: "New", Source: "manual"}}
	out = formatInvariantShow(bare, &api.InvariantWaiversResponse{Page: 1})
	if !strings.Contains(out, "  Applied 0 · Failed 0 (-) · Waived 0 of 0 failures (-)\n") ||
		strings.Contains(out, "Waived as") || strings.Contains(out, "Version:") ||
		!strings.Contains(out, "\nBy version\n  none\n") {
		t.Errorf("output = %s", out)
	}
}

func TestFormatInvariantReviews(t *testing.T) {
	review := func(n int, url string) api.InvariantReview {
		r := api.InvariantReview{RunbookNumber: n, Repositories: []api.Repository{{Org: "acme", Name: "web"}}}
		if url != "" {
			r.PullRequests = []api.LinkedPullRequest{{Number: 4800 + n, URL: url}}
		}
		return r
	}
	resp := &api.InvariantReviewsResponse{
		Invariant: api.InvariantSummary{ID: 42, Title: "HTTP handlers authenticate"},
		Page:      1,
		HasMore:   true,
		Reviews: []api.InvariantReviewRow{
			{Review: review(1302, "https://github.com/acme/web/pull/6102"), AppliedAt: "2026-10-24T08:00:00Z", Version: ptr(3), Outcome: "passed"},
			{
				Review: review(1296, "https://github.com/acme/web/pull/6096"), AppliedAt: "2026-10-22T08:00:00Z", Version: ptr(3),
				Outcome: "fixed", VerdictRun: &api.InvariantReviewRun{ID: 9958, Status: "succeeded"},
			},
			{
				Review: review(1288, "https://github.com/acme/web/pull/6088"), AppliedAt: "2026-10-21T08:00:00Z", Version: ptr(2),
				Outcome: "waived", Waiver: &api.InvariantWaiver{Category: "false_positive", WaivedBy: &api.InvariantUser{FullName: "Dana K."}},
			},
			{Review: review(1270, ""), AppliedAt: "2026-10-17T08:00:00Z", Outcome: "not_run"},
		},
	}
	out := formatInvariantReviews(resp)
	want := "#42 HTTP handlers authenticate\n" +
		"WHEN        REVIEW  PR             OUTCOME  VERSION  NOTE\n" +
		"2026-10-24  r/1302  acme/web#6102  passed   3        \n" +
		"2026-10-22  r/1296  acme/web#6096  fixed    3        failed earlier run, passed run 9958\n" +
		"2026-10-21  r/1288  acme/web#6088  waived   2        false positive, Dana K.\n" +
		"2026-10-17  r/1270  -              not_run  -        \n" +
		"See more reviews --page 2\n"
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}

	// An empty page still names the invariant, and says why it is empty.
	empty := &api.InvariantReviewsResponse{Invariant: resp.Invariant, Page: 1}
	if out = formatInvariantReviews(empty); out != "#42 HTTP handlers authenticate\nNo reviews match.\n" {
		t.Errorf("output = %q", out)
	}
	empty.Page = 3
	out = formatInvariantReviews(empty)
	if !strings.Contains(out, "Nothing on page 3") {
		t.Errorf("output = %q", out)
	}
}

func TestFormatAccountWaivers(t *testing.T) {
	rows := []api.InvariantWaiverRow{waiverRows[0], waiverRows[0]}
	rows[0].Invariant = api.InvariantSummary{ID: 42, Title: "HTTP handlers authenticate before business logic"}
	rows[1].Invariant = api.InvariantSummary{ID: 7, Title: "Short"}
	rows[1].WaivedBy = nil
	rows[1].Justification = ""
	rows[1].Review.PullRequests = nil
	out := formatAccountWaivers(&api.InvariantWaiversResponse{Waivers: rows, Page: 2, HasMore: true})
	want := "WHEN        INVARIANT                               REVIEW  PR             REASON          BY       JUSTIFICATION\n" +
		"2026-10-21  #42 HTTP handlers authenticate before…  r/1288  acme/web#4821  false positive  Dana K.  Endpoint is internal-only, behind the VPN gate.\n" +
		"2026-10-21  #7 Short                                r/1288  -              false positive  -        -\n" +
		"See more waivers --page 3\n"
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
	if out := formatAccountWaivers(&api.InvariantWaiversResponse{Page: 1}); out != "No waivers match.\n" {
		t.Errorf("empty = %q", out)
	}
}

func TestTruncateTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Short": "Short",
		"HTTP handlers authenticate before business logic": "HTTP handlers authenticate before…",
		"0123456789012345678901234567890123456789":         "0123456789012345678901234567890123456789",
		"0123456789012345678901234567890123456789X":        "0123456789012345678901234567890123456789…",
		"Ünïcödé títlé thät ïs lóng énóügh tö bé cüt öff":  "Ünïcödé títlé thät ïs lóng énóügh tö bé…",
	} {
		if got := truncateTitle(in, waiverTitleWidth); got != want {
			t.Errorf("truncateTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatReviewPR(t *testing.T) {
	web := []api.Repository{{Org: "acme", Name: "web"}}
	repos := []api.Repository{{Org: "acme", Name: "web"}, {Org: "acme", Name: "api"}}
	for want, r := range map[string]api.InvariantReview{
		"-":             {Repositories: repos},
		"acme/web#12":   {Repositories: web, PullRequests: []api.LinkedPullRequest{{Number: 12}}},
		"acme/web#3,#4": {Repositories: web, PullRequests: []api.LinkedPullRequest{{Number: 3}, {Number: 4}}},
		"#12":           {PullRequests: []api.LinkedPullRequest{{Number: 12}}},
		"#3,#4":         {Repositories: repos, PullRequests: []api.LinkedPullRequest{{Number: 3}, {Number: 4}}},
	} {
		if got := formatReviewPR(&r); got != want {
			t.Errorf("formatReviewPR(%+v) = %q, want %q", r, got, want)
		}
	}
}

func TestFormatRateAndDate(t *testing.T) {
	for want, in := range map[string]*float64{"-": nil, "0%": rate(0), "42%": rate(0.417), "58%": rate(0.581), "100%": rate(1)} {
		if got := formatRate(in); got != want {
			t.Errorf("formatRate = %q, want %q", got, want)
		}
	}
	for in, want := range map[string]string{
		"": "-", "2026-10-21T23:59:59-07:00": "2026-10-21", "2026-10-21T10:00:00Z": "2026-10-21", "garbage": "garbage",
	} {
		if got := formatDate(in); got != want {
			t.Errorf("formatDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInsightsJSON(t *testing.T) {
	got := decodeJSON(t, newInsightsJSON(insightsResponse))
	if got["since"] != nil || got["status"] != "active" {
		t.Errorf("since = %v, status = %v", got["since"], got["status"])
	}
	applied := got["most_applied"].([]any)[0].(map[string]any)
	if inv := applied["invariant"].(map[string]any); inv["id"] != float64(42) || applied["times_applied"] != float64(143) {
		t.Errorf("most_applied = %v", applied)
	}
	failure := got["failure_ratio"].([]any)[0].(map[string]any)
	if failure["failure_rate"] != 0.417 {
		t.Errorf("failure_ratio = %v", failure)
	}
	waived := got["most_waived"].([]any)[0].(map[string]any)
	assertJSONFields(t, waived["counts"].(map[string]any), map[string]any{
		"false_positive": float64(11), "doesnt_apply": float64(4), "accepted_risk": float64(2), "fix_in_followup": float64(1),
	})
	// An empty ranking is [] so callers can range over it.
	if rows, ok := got["waive_rate"].([]any); !ok || len(rows) != 0 {
		t.Errorf("waive_rate = %v", got["waive_rate"])
	}
	never := got["never_applied"].(map[string]any)
	if never["count"] != float64(7) || never["invariants"].([]any)[0].(map[string]any)["created_at"] != "2026-09-02T10:00:00Z" {
		t.Errorf("never_applied = %v", never)
	}
	if got["reasons"].(map[string]any)["total"] != float64(61) {
		t.Errorf("reasons = %v", got["reasons"])
	}
}

func TestInvariantShowJSON(t *testing.T) {
	got := decodeJSON(t, newInvariantShowJSON(showDetail, &api.InvariantWaiversResponse{Waivers: waiverRows, HasMore: true}))
	if got["id"] != float64(42) || got["title"] != "HTTP handlers authenticate" || got["version"] != float64(3) {
		t.Errorf("invariant fields = %v", got)
	}
	usage := got["usage"].(map[string]any)
	if usage["times_applied"] != float64(143) || usage["waived_failures"] != float64(18) || usage["waive_rate"] != 0.581 {
		t.Errorf("usage = %v", usage)
	}
	if _, ok := usage["waives_by_category"].(map[string]any); !ok {
		t.Errorf("usage = %v", usage)
	}
	versions := got["by_version"].([]any)
	if len(versions) != 2 || versions[1].(map[string]any)["version"] != nil {
		t.Errorf("by_version = %v", versions)
	}
	// Every usage row carries the same seven keys as usage itself.
	if v := versions[0].(map[string]any); v["times_applied"] != float64(40) || v["failure_rate"] != 0.1 ||
		v["waived_failures"] != float64(0) || len(v) != 9 {
		t.Errorf("by_version[0] = %v", v)
	}
	if _, ok := got["by_repo"]; ok {
		t.Errorf("by_repo should be omitted when not asked for, got %v", got["by_repo"])
	}
	waiver := got["waivers"].([]any)[0].(map[string]any)
	if waiver["id"] != float64(9) || waiver["category"] != "false_positive" || waiver["version"] != float64(2) {
		t.Errorf("waiver = %v", waiver)
	}
	if by := waiver["waived_by"].(map[string]any); by["full_name"] != "Dana K." {
		t.Errorf("waived_by = %v", by)
	}
	review := waiver["review"].(map[string]any)
	if review["id"] != "r/1288" || review["url"] != "https://app.aviator.co/r/1288" {
		t.Errorf("review = %v", review)
	}
	if repos := review["repositories"].([]any); len(repos) != 1 || repos[0] != "acme/web" {
		t.Errorf("repositories = %v", repos)
	}
	if prs := review["pull_requests"].([]any); prs[0].(map[string]any)["number"] != float64(4821) {
		t.Errorf("pull_requests = %v", prs)
	}
	if inv, ok := waiver["invariant"].(map[string]any); !ok || inv["id"] != float64(42) {
		t.Errorf("invariant = %v", waiver["invariant"])
	}
	if got["waivers_has_more"] != true {
		t.Errorf("waivers_has_more = %v", got["waivers_has_more"])
	}

	withRepos := *showDetail
	withRepos.ByRepo = []api.InvariantRepoUsage{}
	got = decodeJSON(t, newInvariantShowJSON(&withRepos, &api.InvariantWaiversResponse{}))
	if repos, ok := got["by_repo"].([]any); !ok || len(repos) != 0 {
		t.Errorf("by_repo should be [] when asked for and empty, got %v", got["by_repo"])
	}
	withRepos.ByRepo = []api.InvariantRepoUsage{{
		Repository:     api.Repository{Org: "acme", Name: "web"},
		InvariantUsage: api.InvariantUsage{TimesApplied: 100, WaivedFailures: 16, WaiveRate: rate(0.571)},
	}}
	got = decodeJSON(t, newInvariantShowJSON(&withRepos, &api.InvariantWaiversResponse{}))
	if r := got["by_repo"].([]any)[0].(map[string]any); r["repository"] != "acme/web" || r["times_applied"] != float64(100) ||
		r["waived_failures"] != float64(16) || r["waive_rate"] != 0.571 || len(r) != 8 {
		t.Errorf("by_repo[0] = %v", r)
	}
	if waivers, ok := got["waivers"].([]any); !ok || len(waivers) != 0 {
		t.Errorf("waivers should be [], got %v", got["waivers"])
	}
}

func TestInvariantReviewsAndWaiversJSON(t *testing.T) {
	resp := &api.InvariantReviewsResponse{Invariant: api.InvariantSummary{ID: 42, Title: "T", Category: "security", Status: "active"}, Reviews: []api.InvariantReviewRow{{
		Review:     waiverRows[0].Review,
		AppliedAt:  "2026-10-21T08:00:00Z",
		Result:     ptr("fail"),
		Outcome:    "waived",
		VerdictRun: &api.InvariantReviewRun{ID: 9931, Status: "failed"},
		Waiver:     &waiverRows[0].InvariantWaiver,
	}}}
	got := decodeJSON(t, newInvariantReviewsJSON(resp))
	assertJSONFields(t, got["invariant"].(map[string]any), map[string]any{
		"id": float64(42), "title": "T", "category": "security", "status": "active",
	})
	row := got["reviews"].([]any)[0].(map[string]any)
	if row["outcome"] != "waived" || row["result"] != "fail" || row["version"] != nil || row["applied_at"] != "2026-10-21T08:00:00Z" {
		t.Errorf("row = %v", row)
	}
	if run := row["verdict_run"].(map[string]any); run["run_id"] != float64(9931) || run["completed_at"] != nil {
		t.Errorf("verdict_run = %v", run)
	}
	if w := row["waiver"].(map[string]any); w["category"] != "false_positive" || w["waived_by"].(map[string]any)["email"] != "dana@acme.com" {
		t.Errorf("waiver = %v", w)
	}
	if row["review"].(map[string]any)["id"] != "r/1288" {
		t.Errorf("review = %v", row["review"])
	}
	got = decodeJSON(t, newInvariantReviewsJSON(&api.InvariantReviewsResponse{Invariant: resp.Invariant}))
	if got["invariant"].(map[string]any)["id"] != float64(42) || len(got["reviews"].([]any)) != 0 || got["has_more"] != false {
		t.Errorf("empty = %v", got)
	}

	rows := []api.InvariantWaiverRow{waiverRows[0]}
	rows[0].Invariant = api.InvariantSummary{ID: 42, Title: "T"}
	rows[0].WaivedBy = nil
	got = decodeJSON(t, newInvariantWaiversJSON(&api.InvariantWaiversResponse{Waivers: rows, HasMore: true}))
	waiver := got["waivers"].([]any)[0].(map[string]any)
	if waiver["invariant"].(map[string]any)["id"] != float64(42) || waiver["waived_by"] != nil || got["has_more"] != true {
		t.Errorf("waivers = %v", got)
	}
}
