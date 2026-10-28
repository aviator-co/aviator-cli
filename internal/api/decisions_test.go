package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const decisionJSON = `{"id":7,"type":"risk","title":"Repos share one rate limit","summary":null,` +
	`"status":"settled","outcome":"accepted_risk","settled_at":"2026-10-21T00:00:00Z",` +
	`"resolution":"Rare enough to ship",` +
	`"anchor":{"commit_sha":"abc","file_path":"a.go","start_line":40,"end_line":52},` +
	`"repo":{"org":"acme","name":"web"},"raised_by":{"id":7,"name":"Theo"},` +
	`"author":{"role":"coding_agent","model":"claude-fable-5-1","harness":"claude-code","skills":["go"]},` +
	`"comments":[{"id":1,"user":{"id":7,"name":"Theo"},` +
	`"author":{"role":"person","model":null,"harness":null,"skills":null},"body":"hi",` +
	`"created_at":"2026-10-21T00:00:00Z","modified_at":"2026-10-21T00:00:00Z"}],` +
	`"created_at":"2026-10-21T00:00:00Z","modified_at":"2026-10-21T00:00:00Z"}`

func TestListDecisions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/verify/123/decisions" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"decisions":[` + decisionJSON + `]}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).ListDecisions(context.Background(), 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Decisions) != 1 {
		t.Fatalf("decisions = %+v", resp.Decisions)
	}
	d := resp.Decisions[0]
	if d.ID != 7 || d.Outcome != "accepted_risk" || d.Resolution != "Rare enough to ship" ||
		d.Anchor.EndLine != 52 || d.RaisedBy.Name != "Theo" || d.Author.Harness != "claude-code" ||
		len(d.Comments) != 1 || d.Comments[0].User.Name != "Theo" || d.Comments[0].Author.Role != "person" {
		t.Errorf("decision = %+v", d)
	}
}

func TestDecisionNullAuthors(t *testing.T) {
	var d Decision
	if err := json.Unmarshal([]byte(`{"id":7,"raised_by":null,"author":null,`+
		`"comments":[{"id":1,"user":null,"author":null,"body":"b"}]}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.RaisedBy != nil || d.Author != nil || d.Comments[0].User != nil || d.Comments[0].Author != nil {
		t.Errorf("decision = %+v", d)
	}
}

// recordRequests serves reply to every request and records each one as
// "METHOD path body".
func recordRequests(t *testing.T, reply string) (*httptest.Server, *[]string) {
	t.Helper()
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+string(data))
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestGetDecision(t *testing.T) {
	srv, got := recordRequests(t, decisionJSON)
	d, err := newTestClient(srv).GetDecision(context.Background(), 123, 7)
	if err != nil || d.ID != 7 || len(d.Comments) != 1 {
		t.Fatalf("decision = %+v, err = %v", d, err)
	}
	if want := "GET /api/v1/verify/123/decisions/7 "; (*got)[0] != want {
		t.Errorf("request = %q, want %q", (*got)[0], want)
	}
}

// TestApplyDecisionsBody checks the batch body: left-out keys stay out, and a
// null outcome or anchor is sent as null.
func TestApplyDecisionsBody(t *testing.T) {
	srv, got := recordRequests(t, `{"decisions":[`+decisionJSON+`]}`)
	agent := DecisionAuthor{Role: "coding_agent", Model: "m", Harness: "claude-code", Skills: []string{"go"}}
	resp, err := newTestClient(srv).ApplyDecisions(context.Background(), 123, DecisionBatchRequest{
		Decisions: []DecisionBatchItem{
			{
				Author: agent, Type: "choice", Title: "T", Summary: "S",
				Anchor:   ValueOf(DecisionAnchor{FilePath: "a.go", StartLine: 4, EndLine: 4}),
				Outcome:  ValueOf("decided"),
				Comments: []DecisionCommentInput{{Author: agent, Body: "b", ModelMetadata: map[string]any{"subagent": "r"}}},
			},
			{ID: 7, Author: DecisionAuthor{Role: "person"}, Outcome: Null[string](), Anchor: Null[DecisionAnchor]()},
			{ID: 8, Author: DecisionAuthor{Role: "person"}, Outcome: ValueOf("fixed"), Resolution: "why"},
		},
	})
	if err != nil || len(resp.Decisions) != 1 {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
	want := `POST /api/v1/verify/123/decisions/batch {"decisions":[` +
		`{"author":{"role":"coding_agent","model":"m","harness":"claude-code","skills":["go"]},` +
		`"type":"choice","title":"T","summary":"S","anchor":{"file_path":"a.go","start_line":4,"end_line":4},` +
		`"outcome":"decided","comments":[{"author":{"role":"coding_agent","model":"m","harness":"claude-code",` +
		`"skills":["go"]},"body":"b","model_metadata":{"subagent":"r"}}]},` +
		`{"id":7,"author":{"role":"person"},"anchor":null,"outcome":null},` +
		`{"id":8,"author":{"role":"person"},"outcome":"fixed","resolution":"why"}]}`
	if (*got)[0] != want {
		t.Errorf("request =\n%s\nwant\n%s", (*got)[0], want)
	}
}

func TestNullableUnmarshal(t *testing.T) {
	for in, want := range map[string]Nullable[string]{
		`{}`:               {},
		`{"outcome":null}`: Null[string](),
		`{"outcome":"x"}`:  ValueOf("x"),
	} {
		var item DecisionBatchItem
		if err := json.Unmarshal([]byte(in), &item); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		got := item.Outcome
		if got.Set != want.Set || (got.Value == nil) != (want.Value == nil) ||
			(got.Value != nil && *got.Value != *want.Value) {
			t.Errorf("%s: outcome = %+v, want %+v", in, got, want)
		}
	}
}

func TestEditDecisionCommentBody(t *testing.T) {
	srv, got := recordRequests(t, `{"id":23,"user":{"id":7,"name":"Theo"},"author":{"role":"person"},"body":"new"}`)
	c, err := newTestClient(srv).EditDecisionComment(context.Background(), 123, 7, 23, "new")
	if err != nil || c.ID != 23 || c.Body != "new" || c.User.Name != "Theo" {
		t.Fatalf("comment = %+v, err = %v", c, err)
	}
	if want := `PATCH /api/v1/verify/123/decisions/7/comments/23 {"body":"new"}`; (*got)[0] != want {
		t.Errorf("request =\n%s\nwant\n%s", (*got)[0], want)
	}
}
