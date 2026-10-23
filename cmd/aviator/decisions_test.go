//go:build experimental

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/config"
)

func TestParseLineRange(t *testing.T) {
	for _, c := range []struct {
		in         string
		start, end int
	}{{"40", 40, 40}, {"40-52", 40, 52}, {" 3 - 4 ", 3, 4}} {
		start, end, err := parseLineRange(c.in)
		if err != nil || start != c.start || end != c.end {
			t.Errorf("%q: %d-%d, %v", c.in, start, end, err)
		}
	}
	for _, in := range []string{"", "x", "0", "52-40", "1-x"} {
		if _, _, err := parseLineRange(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}

func TestBuildAnchor(t *testing.T) {
	if a, err := buildAnchor("", "", ""); a != nil || err != nil {
		t.Errorf("no flags: %+v, %v", a, err)
	}
	if _, err := buildAnchor("", "4", ""); err == nil {
		t.Error("--lines without --path: want an error")
	}
	a, err := buildAnchor("a.go", "40-52", "abc")
	if err != nil || *a != (api.DecisionAnchor{FilePath: "a.go", StartLine: 40, EndLine: 52, CommitSHA: "abc"}) {
		t.Errorf("anchor = %+v, %v", a, err)
	}
}

func TestResolveActors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "aviator"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aviator", "actors.yaml"),
		[]byte("actors:\n  - name: Front End Agent\n    role: coding_agent\n  - name: Sam Rivera\n    role: person\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	agent, self := &api.ActorRef{Name: "Front End Agent"}, &api.ActorRef{Name: "person"}
	if err := resolveActors([]*api.ActorRef{agent, self}); err != nil {
		t.Fatalf("known actors: %v", err)
	}
	if *agent != (api.ActorRef{Role: "coding_agent", Name: "Front End Agent"}) || *self != (api.ActorRef{Role: "person"}) {
		t.Errorf("resolved = %+v, %+v", *agent, *self)
	}
	if _, err := resolveActor("Nobody"); err == nil || !strings.Contains(err.Error(), `"Nobody"`) {
		t.Errorf("unknown actor: err = %v", err)
	}
	// The built-in actor is the only person; one in the file is refused.
	if _, err := resolveActor("Sam Rivera"); err == nil || !strings.Contains(err.Error(), `built-in actor "person"`) {
		t.Errorf("person in the file: err = %v", err)
	}
}

func TestBatchActors(t *testing.T) {
	refs, err := batchActors([]decisionBatchEntry{
		{
			Decision: batchDecision{Type: "risk", Title: "T", Actor: &api.ActorRef{Name: "A"}},
			Comments: []api.DecisionCommentInput{{Actor: api.ActorRef{Name: "B"}, Body: "x"}},
		},
		{
			Decision:   batchDecision{ID: 7},
			Settlement: &batchSettlement{Outcome: "fixed", Actor: api.ActorRef{Name: "person"}},
		},
	})
	var names []string
	for _, r := range refs {
		names = append(names, r.Name)
	}
	if err != nil || strings.Join(names, ",") != "A,B,person" {
		t.Errorf("names = %v, err = %v", names, err)
	}

	for name, entry := range map[string]decisionBatchEntry{
		"create without actor": {Decision: batchDecision{Type: "risk", Title: "T"}},
		"change without actor": {Decision: batchDecision{ID: 7, Title: "T"}},
		"comment without actor": {
			Decision: batchDecision{ID: 7},
			Comments: []api.DecisionCommentInput{{Body: "x"}},
		},
		"settlement without actor": {
			Decision:   batchDecision{ID: 7},
			Settlement: &batchSettlement{Outcome: "fixed"},
		},
		"settlement without outcome": {
			Decision:   batchDecision{ID: 7},
			Settlement: &batchSettlement{Actor: api.ActorRef{Name: "person"}},
		},
		"nothing to do": {Decision: batchDecision{ID: 7}},
	} {
		if _, err := batchActors([]decisionBatchEntry{entry}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// TestApplyBatch checks how one batch is split into the API's requests: all
// creates in one POST (settled ones with their outcome), a PATCH per existing
// decision it changes or settles, then each decision's comments with the
// settlement body last.
func TestApplyBatch(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+string(data))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/verify/4/decisions":
			_, _ = w.Write([]byte(`{"decisions":[{"id":10},{"id":11}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/verify/4/decisions/8":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"conflict","message":"decision is settled"}`))
		case r.Method == http.MethodPatch:
			_, _ = w.Write([]byte(`{"id":7}`))
		default:
			_, _ = w.Write([]byte(`{"comments":[]}`))
		}
	}))
	defer srv.Close()
	host, token := config.Av.Aviator.APIHost, config.Av.Aviator.APIToken
	config.Av.Aviator.APIHost, config.Av.Aviator.APIToken = srv.URL, "test-token"
	t.Cleanup(func() { config.Av.Aviator.APIHost, config.Av.Aviator.APIToken = host, token })
	client, err := api.NewClient()
	if err != nil {
		t.Fatal(err)
	}

	agent, me := api.ActorRef{Role: "coding_agent", Name: "A"}, api.ActorRef{Role: "person"}
	batch := &decisionBatch{
		ModelMetadata: &api.ModelMetadata{Model: "m"},
		Decisions: []decisionBatchEntry{
			{
				Decision: batchDecision{Type: "risk", Title: "T", Actor: &agent},
				Comments: []api.DecisionCommentInput{{Actor: agent, Body: "why"}},
			},
			{Decision: batchDecision{ID: 7}, Comments: []api.DecisionCommentInput{{Actor: me, Body: "c"}}},
			{
				Decision:   batchDecision{ID: 7, Summary: "S", Actor: &agent},
				Settlement: &batchSettlement{Outcome: "fixed", Actor: me, Body: "done"},
			},
			{
				Decision:   batchDecision{Type: "choice", Title: "U", Actor: &agent},
				Settlement: &batchSettlement{Outcome: "decided", Actor: me},
			},
		},
	}
	ids, err := applyBatch(context.Background(), client, 4, batch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fmt.Sprint(ids) != "[10 7 7 11]" {
		t.Errorf("ids = %v, want [10 7 7 11]", ids)
	}
	want := []string{
		`POST /api/v1/verify/4/decisions {"decisions":[` +
			`{"type":"risk","title":"T","actor":{"role":"coding_agent","name":"A"}},` +
			`{"type":"choice","title":"U","actor":{"role":"coding_agent","name":"A"},"outcome":"decided"}]}`,
		`PATCH /api/v1/verify/4/decisions/7 {"actor":{"role":"coding_agent","name":"A"},"summary":"S","outcome":"fixed"}`,
		`POST /api/v1/verify/4/decisions/10/comments {"via":"aviator-cli","model_metadata":{"model":"m"},` +
			`"comments":[{"actor":{"role":"coding_agent","name":"A"},"body":"why"}]}`,
		`POST /api/v1/verify/4/decisions/7/comments {"via":"aviator-cli","model_metadata":{"model":"m"},` +
			`"comments":[{"actor":{"role":"person"},"body":"c"}]}`,
		`POST /api/v1/verify/4/decisions/7/comments {"via":"aviator-cli","model_metadata":{"model":"m"},` +
			`"comments":[{"actor":{"role":"person"},"body":"done"}]}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// A settle-only entry patches as the settler; a refusal names what was
	// already recorded and stops before any comments.
	got = nil
	_, err = applyBatch(context.Background(), client, 4, &decisionBatch{Decisions: []decisionBatchEntry{
		{Decision: batchDecision{ID: 7}, Settlement: &batchSettlement{Outcome: "fixed", Actor: me, Body: "x"}},
		{Decision: batchDecision{ID: 8}, Settlement: &batchSettlement{Outcome: "fixed", Actor: me, Body: "y"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "changing #8 was refused after recording changed #7") ||
		!strings.Contains(err.Error(), "(409)") {
		t.Errorf("err = %v", err)
	}
	if len(got) != 2 || got[0] != `PATCH /api/v1/verify/4/decisions/7 {"actor":{"role":"person"},"outcome":"fixed"}` {
		t.Errorf("requests = %q", got)
	}

	// Reopening sends a null outcome, then the reason as a comment.
	got = nil
	if _, err := applyBatch(context.Background(), client, 4, &decisionBatch{Decisions: []decisionBatchEntry{{
		Decision: batchDecision{ID: 7, Actor: &me},
		Comments: []api.DecisionCommentInput{{Actor: me, Body: "why"}},
		Reopen:   true,
	}}}); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	want = []string{
		`PATCH /api/v1/verify/4/decisions/7 {"actor":{"role":"person"},"outcome":null}`,
		`POST /api/v1/verify/4/decisions/7/comments {"via":"aviator-cli","comments":[{"actor":{"role":"person"},"body":"why"}]}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestReadDecisionBatch(t *testing.T) {
	write := func(content string) string {
		path := filepath.Join(t.TempDir(), "batch.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	req, err := readDecisionBatch(write(`{"model_metadata":{"model":"m"},"decisions":[` +
		`{"decision":{"type":"risk","title":"T","actor":"A"},"comments":[{"actor":"A","body":"b"}]},` +
		`{"decision":{"id":8},"settlement":{"outcome":"fixed","actor":"person"}}]}`))
	if err != nil || len(req.Decisions) != 2 || req.ModelMetadata.Model != "m" ||
		req.Decisions[1].Settlement.Outcome != "fixed" {
		t.Errorf("req = %+v, err = %v", req, err)
	}
	if _, err := readDecisionBatch(write(`{"via":"aviator-cli","decisions":[{"decision":{"id":7}}]}`)); err != nil {
		t.Errorf("via aviator-cli: %v", err)
	}
	for name, content := range map[string]string{
		"other via":    `{"via":"cli","decisions":[{"decision":{"id":7}}]}`,
		"unknown key":  `{"decisions":[{"decision":{"id":7},"coments":[]}]}`,
		"flat entry":   `{"decisions":[{"id":7,"body":"old format"}]}`,
		"bare array":   `[{"decision":{"id":7}}]`,
		"actor object": `{"decisions":[{"decision":{"id":7},"comments":[{"actor":{"name":"A"},"body":"x"}]}]}`,
		"empty":        `{"decisions":[]}`,
	} {
		if _, err := readDecisionBatch(write(content)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestFormatDecisionAnchor(t *testing.T) {
	for want, a := range map[string]api.DecisionAnchor{
		"a.go:40-52 @ abcdef1": {FilePath: "a.go", StartLine: 40, EndLine: 52, CommitSHA: "abcdef1234"},
		"a.go:40":              {FilePath: "a.go", StartLine: 40, EndLine: 40},
		"@ abc":                {CommitSHA: "abc"},
	} {
		if got := formatDecisionAnchor(&a); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
