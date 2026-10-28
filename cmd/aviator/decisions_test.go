package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/spf13/cobra"
)

// useTestServer points the API client at a server that records each request
// as "METHOD path?query body" and answers with reply. Commands are run through
// RunE directly: going through rootCmd would load the real config and point
// the client away from the test server.
func useTestServer(t *testing.T, reply string) *[]string {
	t.Helper()
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+string(data))
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	host, token := config.Av.Aviator.APIHost, config.Av.Aviator.APIToken
	config.Av.Aviator.APIHost, config.Av.Aviator.APIToken = srv.URL, "test-token"
	t.Cleanup(func() { config.Av.Aviator.APIHost, config.Av.Aviator.APIToken = host, token })
	return &got
}

func runCommand(t *testing.T, cmd *cobra.Command, args ...string) string {
	t.Helper()
	cmd.SetContext(context.Background())
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := cmd.RunE(cmd, cmd.Flags().Args())
	os.Stdout = stdout
	_ = w.Close()
	out, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("%v: %v", args, runErr)
	}
	return string(out)
}

// TestDecisionsApplySendsFile checks apply sends the file unchanged, so the
// server sees the nulls, zero ids, and unknown keys a Go round trip drops.
func TestDecisionsApplySendsFile(t *testing.T) {
	got := useTestServer(t, `{"decisions":[{"id":7,"status":"open","comments":[]}]}`)
	body := `{"decisions": [{"id": 0, "summary": null, "anchor": {"file_path": "a.go", "line": 40}}]}` + "\n"
	path := filepath.Join(t.TempDir(), "batch.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	runCommand(t, decisionsApplyCmd, "r/4", "--file", path)
	if want := "POST /api/v1/verify/4/decisions " + body; len(*got) != 1 || (*got)[0] != want {
		t.Errorf("requests =\n%s\nwant\n%s", strings.Join(*got, "\n"), want)
	}
}

func TestDecisionsListJSON(t *testing.T) {
	got := useTestServer(t, `{"decisions":[{"id":7,"status":"open","comment_count":3}],`+
		`"page":2,"per_page":5,"has_more":true}`)
	decisionsFlags.JSON = true
	t.Cleanup(func() {
		decisionsFlags.JSON = false
		decisionsListFlags.Status, decisionsListFlags.Page, decisionsListFlags.Limit = "", 1, 20
	})
	out := runCommand(t, decisionsListCmd, "r/4", "--status", "open", "--page", "2", "--limit", "5")
	if want := "GET /api/v1/verify/4/decisions?page=2&per_page=5&status=open "; (*got)[0] != want {
		t.Errorf("request = %q, want %q", (*got)[0], want)
	}
	// The list carries no threads, so comments is left out rather than [].
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	d := parsed["decisions"].([]any)[0].(map[string]any)
	if parsed["id"] != "r/4" || parsed["has_more"] != true || d["comment_count"] != 3.0 || d["comments"] != nil {
		t.Errorf("json = %s", out)
	}
	if _, ok := parsed["session"]; ok {
		t.Errorf("json still has session: %s", out)
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

func TestFormatDecisionDetail(t *testing.T) {
	got := formatDecisionDetail(&api.Decision{
		ID: 7, Type: "risk", Title: "T", Status: "settled", Outcome: "accepted_risk", Resolution: "Rare enough",
		RaisedBy: &api.DecisionUser{Name: "Theo"},
		Author:   &api.DecisionAuthor{Role: "coding_agent", Model: "m", Harness: "claude-code"},
		Comments: []api.DecisionComment{
			{ID: 1, User: &api.DecisionUser{Name: "Theo"}, Author: &api.DecisionAuthor{Role: "person"}, Body: "hi"},
			{ID: 2, User: &api.DecisionUser{Name: "Theo"}, Body: "no author"},
		},
	})
	for _, want := range []string{
		"risk · settled (accepted_risk) · raised by Theo · last changed as coding_agent (m, claude-code)\n",
		"Resolution:\n    Rare enough",
		"Theo as person",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail lacks %q:\n%s", want, got)
		}
	}
	// A null comment author leaves out the role instead of printing it blank.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "#2") && strings.Contains(line, " as ") {
			t.Errorf("comment with no author: %q", line)
		}
	}

	// raised_by and author can be null; they're left out rather than blank.
	got = formatDecisionDetail(&api.Decision{ID: 7, Type: "risk", Title: "T", Status: "open"})
	if !strings.Contains(got, "  risk · open\n") {
		t.Errorf("null raised_by and author:\n%s", got)
	}
}
