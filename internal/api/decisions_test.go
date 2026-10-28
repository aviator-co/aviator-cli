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
	srv, got := recordRequests(t, `{"decisions":[{"id":7,"status":"open","comment_count":3}],`+
		`"page":2,"per_page":5,"has_more":true}`)
	resp, err := newTestClient(srv).ListDecisions(context.Background(), 123,
		ListDecisionsParams{Status: "open", Page: 2, PerPage: 5})
	if err != nil || len(resp.Decisions) != 1 || !resp.HasMore || resp.Page != 2 {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
	if d := resp.Decisions[0]; d.NumComments() != 3 || d.Comments != nil {
		t.Errorf("decision = %+v", d)
	}
	if want := "GET /api/v1/verify/123/decisions?page=2&per_page=5&status=open "; (*got)[0] != want {
		t.Errorf("request = %q, want %q", (*got)[0], want)
	}

	// Left-out params leave the defaults to the server.
	if _, err := newTestClient(srv).ListDecisions(context.Background(), 123, ListDecisionsParams{}); err != nil {
		t.Fatal(err)
	}
	if want := "GET /api/v1/verify/123/decisions "; (*got)[1] != want {
		t.Errorf("request = %q, want %q", (*got)[1], want)
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
// "METHOD path?query body".
func recordRequests(t *testing.T, reply string) (*httptest.Server, *[]string) {
	t.Helper()
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+string(data))
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

// TestApplyDecisions checks the body goes out byte for byte: nulls, a zero
// id, unknown keys, and whitespace are the server's to judge.
func TestApplyDecisions(t *testing.T) {
	srv, got := recordRequests(t, `{"decisions":[`+decisionJSON+`]}`)
	body := `{"decisions": [{"id": 0, "summary": null, "anchor": {"file_path": "a.go", "line": 40}}]}` + "\n"
	resp, err := newTestClient(srv).ApplyDecisions(context.Background(), 123, []byte(body))
	if err != nil || len(resp.Decisions) != 1 || resp.Decisions[0].NumComments() != 1 {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
	if want := "POST /api/v1/verify/123/decisions " + body; (*got)[0] != want {
		t.Errorf("request =\n%s\nwant\n%s", (*got)[0], want)
	}
}
