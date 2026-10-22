//go:build experimental

package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const decisionJSON = `{"id":7,"type":"risk","title":"Repos share one rate limit","summary":null,` +
	`"status":"settled","outcome":"accepted_risk","settled_at":"2026-10-21T00:00:00Z",` +
	`"anchor":{"commit_sha":"abc","file_path":"a.go","start_line":40,"end_line":52},` +
	`"repo":{"org":"acme","name":"web"},"raised_by":{"role":"coding_agent","name":"claude-code"},` +
	`"comments":[{"id":1,"author":{"role":"person"},"body":"hi",` +
	`"created_at":"2026-10-21T00:00:00Z","modified_at":"2026-10-21T00:00:00Z"}]}`

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
	if d.ID != 7 || d.Outcome != "accepted_risk" || d.Anchor.EndLine != 52 ||
		d.RaisedBy.Name != "claude-code" || len(d.Comments) != 1 || d.Comments[0].Author.Role != "person" {
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

func TestCreateDecisionsBody(t *testing.T) {
	srv, got := recordRequests(t, `{"decisions":[`+decisionJSON+`]}`)
	_, err := newTestClient(srv).CreateDecisions(context.Background(), 123, CreateDecisionsRequest{
		Decisions: []DecisionCreate{
			{Type: "risk", Title: "T", Actor: ActorRef{Role: "coding_agent", Name: "Front End Agent"}},
			{
				Type: "choice", Title: "U", Actor: ActorRef{Role: "person"}, Summary: "S",
				Anchor: &DecisionAnchor{FilePath: "a.go", StartLine: 4, EndLine: 4}, Outcome: "decided",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `POST /api/v1/verify/123/decisions {"decisions":[` +
		`{"type":"risk","title":"T","actor":{"role":"coding_agent","name":"Front End Agent"}},` +
		`{"type":"choice","title":"U","actor":{"role":"person"},"summary":"S",` +
		`"anchor":{"file_path":"a.go","start_line":4,"end_line":4},"outcome":"decided"}]}`
	if (*got)[0] != want {
		t.Errorf("request =\n%s\nwant\n%s", (*got)[0], want)
	}
}

func TestUpdateDecisionBody(t *testing.T) {
	srv, got := recordRequests(t, decisionJSON)
	_, err := newTestClient(srv).UpdateDecision(context.Background(), 123, 7, DecisionPatch{
		Actor: ActorRef{Role: "person"}, Summary: "S", Outcome: "fixed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `PATCH /api/v1/verify/123/decisions/7 {"actor":{"role":"person"},"summary":"S","outcome":"fixed"}`
	if (*got)[0] != want {
		t.Errorf("request =\n%s\nwant\n%s", (*got)[0], want)
	}
}

func TestAddDecisionCommentsBody(t *testing.T) {
	srv, got := recordRequests(t, `{"comments":[{"id":1,"author":{"role":"person","name":"Sam Rivera"},"body":"b"}]}`)
	resp, err := newTestClient(srv).AddDecisionComments(context.Background(), 123, 7, AddDecisionCommentsRequest{
		Via:           "aviator-cli",
		ModelMetadata: &ModelMetadata{Model: "claude-fable-5-1"},
		Comments:      []DecisionCommentInput{{Actor: ActorRef{Role: "person"}, Body: "b"}},
	})
	if err != nil || len(resp.Comments) != 1 || resp.Comments[0].Author.Name != "Sam Rivera" {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
	want := `POST /api/v1/verify/123/decisions/7/comments {"via":"aviator-cli",` +
		`"model_metadata":{"model":"claude-fable-5-1"},"comments":[{"actor":{"role":"person"},"body":"b"}]}`
	if (*got)[0] != want {
		t.Errorf("request =\n%s\nwant\n%s", (*got)[0], want)
	}
}

func TestReopenDecisionBody(t *testing.T) {
	srv, got := recordRequests(t, decisionJSON)
	if _, err := newTestClient(srv).ReopenDecision(context.Background(), 123, 7,
		ActorRef{Role: "person"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `PATCH /api/v1/verify/123/decisions/7 {"actor":{"role":"person"},"outcome":null}`
	if (*got)[0] != want {
		t.Errorf("request =\n%s\nwant\n%s", (*got)[0], want)
	}
}

func TestEditDecisionCommentBody(t *testing.T) {
	srv, got := recordRequests(t, `{"id":23,"author":{"role":"person","name":"Sam Rivera"},"body":"new"}`)
	c, err := newTestClient(srv).EditDecisionComment(context.Background(), 123, 7, 23, "new")
	if err != nil || c.ID != 23 || c.Body != "new" {
		t.Fatalf("comment = %+v, err = %v", c, err)
	}
	if want := `PATCH /api/v1/verify/123/decisions/7/comments/23 {"body":"new"}`; (*got)[0] != want {
		t.Errorf("request =\n%s\nwant\n%s", (*got)[0], want)
	}
}
