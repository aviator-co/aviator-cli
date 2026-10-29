package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const invariantSummaryJSON = `{"id":42,"title":"HTTP handlers authenticate","category":"security","status":"active"}`

const invariantReviewJSON = `{"runbook_number":1288,"url":"https://app.aviator.co/r/1288",` +
	`"repositories":[{"org":"acme","name":"web"}],` +
	`"pull_requests":[{"number":4821,"url":"https://github.com/acme/web/pull/4821","step_numbers":["1","2"]}]}`

// usageJSON renders the seven usage keys every usage object and row carries;
// rates are JSON literals so null can be passed.
func usageJSON(applied, failed int, failureRate string, waives, waivedFailures int, waiveRate string, falsePositive int) string {
	return fmt.Sprintf(`"times_applied":%d,"times_failed":%d,"failure_rate":%s,"total_waives":%d,`+
		`"waived_failures":%d,"waive_rate":%s,"waives_by_category":{"false_positive":%d,"doesnt_apply":0,`+
		`"accepted_risk":0,"fix_in_followup":0}`,
		applied, failed, failureRate, waives, waivedFailures, waiveRate, falsePositive)
}

const invariantWaiverRowJSON = `{"id":9,"created_at":"2026-10-21T10:00:00Z","category":"false_positive",` +
	`"justification":"Internal only.","waived_by":{"email":"dana@acme.com","full_name":"Dana K."},` +
	`"version":2,"review":` + invariantReviewJSON + `}`

// newInvariantAnalyticsServer answers one GET with body, recording the path
// and query it was asked for.
func newInvariantAnalyticsServer(t *testing.T, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	got := &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		*got = *r
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestGetInvariantInsights(t *testing.T) {
	srv, got := newInvariantAnalyticsServer(t, `{"since":"2026-09-27T00:00:00Z","status":"active",`+
		`"most_applied":[{"invariant":`+invariantSummaryJSON+`,"times_applied":143,"times_failed":31,"total_waives":18}],`+
		`"failure_ratio":[{"invariant":`+invariantSummaryJSON+`,"times_applied":12,"times_failed":5,"failure_rate":0.417}],`+
		`"most_waived":[{"invariant":`+invariantSummaryJSON+`,"total":18,`+
		`"counts":{"false_positive":11,"doesnt_apply":4,"accepted_risk":2,"fix_in_followup":1}}],`+
		`"waive_rate":[{"invariant":`+invariantSummaryJSON+`,"times_failed":31,"waived_failures":18,"waive_rate":0.581}],`+
		`"never_applied":{"count":7,"invariants":[{"id":71,"title":"Flags","category":"code_style","status":"active",`+
		`"created_at":"2026-09-02T00:00:00Z"}]},`+
		`"reasons":{"total":61,"counts":{"false_positive":34,"doesnt_apply":12,"accepted_risk":10,"fix_in_followup":5}}}`)

	resp, err := newTestClient(srv).GetInvariantInsights(context.Background(), InvariantInsightsQuery{
		Since:  "2026-09-27T00:00:00Z",
		Repo:   &Repository{Org: "acme", Name: "web"},
		Limit:  5,
		Status: "pending",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL.Path != "/api/v1/invariants/insights" {
		t.Errorf("path = %q", got.URL.Path)
	}
	if want := "limit=5&org=acme&repo=web&since=2026-09-27T00%3A00%3A00Z&status=pending"; got.URL.RawQuery != want {
		t.Errorf("query = %q, want %q", got.URL.RawQuery, want)
	}
	if resp.Since == nil || *resp.Since != "2026-09-27T00:00:00Z" || resp.Status != "active" {
		t.Errorf("since = %v, status = %q", resp.Since, resp.Status)
	}
	if r := resp.MostApplied[0]; r.Invariant.ID != 42 || r.TimesApplied != 143 || r.TotalWaives != 18 {
		t.Errorf("most_applied = %+v", r)
	}
	if r := resp.FailureRatio[0]; r.FailureRate == nil || *r.FailureRate != 0.417 {
		t.Errorf("failure_ratio = %+v", r)
	}
	if r := resp.MostWaived[0]; r.Total != 18 || r.Counts.FalsePositive != 11 || r.Counts.FixInFollowup != 1 {
		t.Errorf("most_waived = %+v", r)
	}
	if r := resp.WaiveRate[0]; r.WaivedFailures != 18 || r.WaiveRate == nil || *r.WaiveRate != 0.581 {
		t.Errorf("waive_rate = %+v", r)
	}
	if n := resp.NeverApplied; n.Count != 7 || n.Invariants[0].ID != 71 || n.Invariants[0].CreatedAt != "2026-09-02T00:00:00Z" {
		t.Errorf("never_applied = %+v", n)
	}
	if resp.Reasons.Total != 61 || resp.Reasons.Counts.DoesntApply != 12 {
		t.Errorf("reasons = %+v", resp.Reasons)
	}
}

// TestGetInvariantInsightsDefaultQuery covers the zero query: nothing is
// sent, so the server's own window (all time) and status (active) apply.
func TestGetInvariantInsightsDefaultQuery(t *testing.T) {
	srv, got := newInvariantAnalyticsServer(t, `{"since":null,"status":"active","most_applied":[],`+
		`"failure_ratio":[],"most_waived":[],"waive_rate":[],"never_applied":{"count":0,"invariants":[]},`+
		`"reasons":{"total":0,"counts":{"false_positive":0,"doesnt_apply":0,"accepted_risk":0,"fix_in_followup":0}}}`)

	resp, err := newTestClient(srv).GetInvariantInsights(context.Background(), InvariantInsightsQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL.RawQuery != "" {
		t.Errorf("query = %q, want empty", got.URL.RawQuery)
	}
	if resp.Since != nil || len(resp.MostApplied) != 0 {
		t.Errorf("response = %+v", resp)
	}
}

func TestGetInvariant(t *testing.T) {
	detail := strings.TrimSuffix(invariantJSON, "}") + `,"owner_teams":[{"org":"acme","slug":"platform"}],"version":3,` +
		`"usage":{` + usageJSON(143, 31, "0.217", 18, 18, "0.581", 11) + `},` +
		`"by_version":[{"version":3,"created_at":"2026-10-02T00:00:00Z",` + usageJSON(40, 4, "0.1", 1, 1, "0.25", 1) + `},` +
		`{"version":null,"created_at":null,` + usageJSON(25, 6, "0.24", 3, 3, "0.5", 3) + `}],` +
		`"by_repo":[{"repository":{"org":"acme","name":"web"},` + usageJSON(100, 28, "0.28", 17, 16, "0.571", 9) + `}]}`
	srv, got := newInvariantAnalyticsServer(t, detail)

	resp, err := newTestClient(srv).GetInvariant(context.Background(), 7, GetInvariantQuery{ByRepo: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL.Path != "/api/v1/invariants/7" {
		t.Errorf("path = %q", got.URL.Path)
	}
	if want := "include=by_repo"; got.URL.RawQuery != want {
		t.Errorf("query = %q, want %q", got.URL.RawQuery, want)
	}
	// The plain invariant fields still decode through the embedded struct.
	if resp.ID != 7 || resp.Title != "T" || resp.Repositories[0].Name != "web" || resp.OwnerTeams == nil {
		t.Errorf("invariant = %+v", resp.Invariant)
	}
	if resp.Version == nil || *resp.Version != 3 {
		t.Errorf("version = %v", resp.Version)
	}
	if u := resp.Usage; u == nil || u.TimesApplied != 143 || u.WaivedFailures != 18 || u.FailureRate == nil ||
		*u.FailureRate != 0.217 || u.WaiveRate == nil || *u.WaiveRate != 0.581 || u.WaivesByCategory.FalsePositive != 11 {
		t.Errorf("usage = %+v", u)
	}
	if len(resp.ByVersion) != 2 || *resp.ByVersion[0].Version != 3 || resp.ByVersion[0].WaivedFailures != 1 ||
		*resp.ByVersion[0].WaiveRate != 0.25 || resp.ByVersion[1].Version != nil ||
		resp.ByVersion[1].CreatedAt != nil || resp.ByVersion[1].TotalWaives != 3 ||
		resp.ByVersion[1].WaivesByCategory.FalsePositive != 3 {
		t.Errorf("by_version = %+v", resp.ByVersion)
	}
	if len(resp.ByRepo) != 1 || resp.ByRepo[0].Repository.Org != "acme" || resp.ByRepo[0].TimesFailed != 28 ||
		resp.ByRepo[0].WaivedFailures != 16 || *resp.ByRepo[0].WaiveRate != 0.571 ||
		resp.ByRepo[0].WaivesByCategory.FalsePositive != 9 {
		t.Errorf("by_repo = %+v", resp.ByRepo)
	}
}

// TestGetInvariantDefaultQuery pins that a plain lookup sends no query and
// that an omitted by_repo decodes as nil rather than empty.
func TestGetInvariantDefaultQuery(t *testing.T) {
	srv, got := newInvariantAnalyticsServer(t, strings.TrimSuffix(invariantJSON, "}")+`,"version":null,`+
		`"usage":{`+usageJSON(0, 0, "null", 0, 0, "null", 0)+`},"by_version":[]}`)

	resp, err := newTestClient(srv).GetInvariant(context.Background(), 7, GetInvariantQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL.RawQuery != "" {
		t.Errorf("query = %q, want empty", got.URL.RawQuery)
	}
	if resp.Version != nil || resp.Usage.FailureRate != nil || resp.ByRepo != nil || len(resp.ByVersion) != 0 {
		t.Errorf("response = %+v", resp)
	}
}

func TestListInvariantReviews(t *testing.T) {
	srv, got := newInvariantAnalyticsServer(t, `{"invariant":`+invariantSummaryJSON+`,"reviews":[`+
		`{"review":`+invariantReviewJSON+`,"applied_at":"2026-10-21T09:00:00Z","version":2,`+
		`"verdict_run":{"id":9931,"status":"failed","completed_at":"2026-10-21T09:30:00Z"},"result":"fail",`+
		`"outcome":"waived","waiver":{"category":"false_positive","justification":"Internal only.",`+
		`"waived_by":null,"created_at":"2026-10-21T10:00:00Z","version":2}},`+
		`{"review":`+invariantReviewJSON+`,"applied_at":"2026-10-20T09:00:00Z","version":null,`+
		`"verdict_run":null,"result":null,"outcome":"not_run","waiver":null}`+
		`],"page":1,"per_page":20,"has_more":false}`)

	resp, err := newTestClient(srv).ListInvariantReviews(context.Background(), 42, InvariantReviewsQuery{
		Repo:    &Repository{Org: "acme", Name: "web"},
		Outcome: "waived",
		Page:    1,
		PerPage: 20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL.Path != "/api/v1/invariants/42/reviews" {
		t.Errorf("path = %q", got.URL.Path)
	}
	if want := "org=acme&outcome=waived&page=1&per_page=20&repo=web"; got.URL.RawQuery != want {
		t.Errorf("query = %q, want %q", got.URL.RawQuery, want)
	}
	if resp.HasMore || len(resp.Reviews) != 2 || resp.Invariant.ID != 42 || resp.Invariant.Title != "HTTP handlers authenticate" {
		t.Errorf("response = %+v", resp)
	}
	r := resp.Reviews[0]
	if r.Review.RunbookNumber != 1288 || r.AppliedAt != "2026-10-21T09:00:00Z" || *r.Version != 2 ||
		r.Outcome != "waived" || r.Result == nil || *r.Result != "fail" {
		t.Errorf("row = %+v", r)
	}
	if r.VerdictRun == nil || r.VerdictRun.ID != 9931 || r.VerdictRun.Status != "failed" || r.VerdictRun.CompletedAt == nil {
		t.Errorf("verdict_run = %+v", r.VerdictRun)
	}
	// A deleted user decodes as nil, not as an empty one.
	if r.Waiver == nil || r.Waiver.Category != "false_positive" || r.Waiver.WaivedBy != nil {
		t.Errorf("waiver = %+v", r.Waiver)
	}
	bare := resp.Reviews[1]
	if bare.Version != nil || bare.VerdictRun != nil || bare.Result != nil || bare.Waiver != nil || bare.Outcome != "not_run" {
		t.Errorf("not_run row = %+v", bare)
	}
}

func TestListAccountWaivers(t *testing.T) {
	row := strings.TrimSuffix(invariantWaiverRowJSON, "}") + `,"invariant":` + invariantSummaryJSON + `}`
	srv, got := newInvariantAnalyticsServer(t, `{"waivers":[`+row+`],"page":1,"per_page":20,"has_more":false}`)

	resp, err := newTestClient(srv).ListAccountWaivers(context.Background(), AccountWaiversQuery{
		InvariantID: 42,
		User:        "me",
		Category:    "false_positive",
		Repo:        &Repository{Org: "acme", Name: "web"},
		Page:        1,
		PerPage:     20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL.Path != "/api/v1/invariants/waivers" {
		t.Errorf("path = %q", got.URL.Path)
	}
	want := "category=false_positive&invariant=42&org=acme&page=1&per_page=20&repo=web&user=me"
	if got.URL.RawQuery != want {
		t.Errorf("query = %q, want %q", got.URL.RawQuery, want)
	}
	if len(resp.Waivers) != 1 {
		t.Fatalf("waivers = %+v", resp.Waivers)
	}
	w := resp.Waivers[0]
	if w.ID != 9 || w.Category != "false_positive" || w.Justification != "Internal only." ||
		w.CreatedAt != "2026-10-21T10:00:00Z" || w.Version == nil || *w.Version != 2 {
		t.Errorf("waiver = %+v", w)
	}
	if w.WaivedBy == nil || w.WaivedBy.FullName != "Dana K." || w.WaivedBy.Email != "dana@acme.com" {
		t.Errorf("waived_by = %+v", w.WaivedBy)
	}
	if pr := w.Review.PullRequests[0]; w.Review.RunbookNumber != 1288 || w.Review.Repositories[0].Name != "web" ||
		pr.Number != 4821 || pr.URL == "" || len(pr.StepNumbers) != 2 {
		t.Errorf("review = %+v", w.Review)
	}
	if inv := w.Invariant; inv.ID != 42 || inv.Title != "HTTP handlers authenticate" {
		t.Errorf("invariant = %+v", inv)
	}
}

// TestListAccountWaiversDefaultQuery covers the zero query: no filter is
// sent blank, which the server would read as a filter on "".
func TestListAccountWaiversDefaultQuery(t *testing.T) {
	srv, got := newInvariantAnalyticsServer(t, `{"waivers":[],"page":1,"per_page":20,"has_more":false}`)

	if _, err := newTestClient(srv).ListAccountWaivers(context.Background(), AccountWaiversQuery{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL.RawQuery != "" {
		t.Errorf("query = %q, want empty", got.URL.RawQuery)
	}
}

// TestListInvariantsUsageQuery pins the include=usage additions' encoding and
// that a row's usage decodes, with a null rate kept apart from zero.
func TestListInvariantsUsageQuery(t *testing.T) {
	row := strings.TrimSuffix(invariantJSON, "}") + `,"usage":{` + usageJSON(12, 5, "0.417", 3, 0, "null", 3) + `}}`
	srv, got := newInvariantAnalyticsServer(t, `{"invariants":[`+row+`],"page":1,"per_page":20,"has_more":false}`)

	resp, err := newTestClient(srv).ListInvariants(context.Background(), ListInvariantsQuery{
		Status:       "active",
		IncludeUsage: true,
		Sort:         "failure_rate",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "include=usage&sort=failure_rate&status=active"
	if got.URL.RawQuery != want {
		t.Errorf("query = %q, want %q", got.URL.RawQuery, want)
	}
	u := resp.Invariants[0].Usage
	if u == nil || u.TimesApplied != 12 || u.TimesFailed != 5 || u.TotalWaives != 3 ||
		u.WaivedFailures != 0 || u.WaivesByCategory.FalsePositive != 3 {
		t.Fatalf("usage = %+v", u)
	}
	if u.FailureRate == nil || *u.FailureRate != 0.417 || u.WaiveRate != nil {
		t.Errorf("rates = %v, %v", u.FailureRate, u.WaiveRate)
	}
}

// TestListInvariantsWithoutUsage pins that a plain listing (or an older
// backend) leaves Usage nil, which is what tells the CLI usage is missing.
func TestListInvariantsWithoutUsage(t *testing.T) {
	srv, _ := newInvariantAnalyticsServer(t, `{"invariants":[`+invariantJSON+`],"page":1,"per_page":20,"has_more":false}`)

	resp, err := newTestClient(srv).ListInvariants(context.Background(), ListInvariantsQuery{IncludeUsage: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Invariants[0].Usage != nil || resp.Invariants[0].Version != nil {
		t.Errorf("invariant = %+v", resp.Invariants[0])
	}
}
