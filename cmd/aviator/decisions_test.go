package main

import (
	"context"
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
	base := &api.DecisionAnchor{FilePath: "a.go", StartLine: 40, EndLine: 52, CommitSHA: "abc"}
	if a, err := buildAnchor(base, "", "", ""); !a.IsZero() || err != nil {
		t.Errorf("no flags: %+v, %v", a, err)
	}
	if _, err := buildAnchor(nil, "", "4", ""); err == nil {
		t.Error("--lines without --path: want an error")
	}
	for _, c := range []struct {
		base                *api.DecisionAnchor
		path, lines, commit string
		want                api.DecisionAnchor
	}{
		{nil, "a.go", "40-52", "abc", *base},
		// A flag changes only its part; the rest comes from the current anchor.
		{base, "", "", "def", api.DecisionAnchor{FilePath: "a.go", StartLine: 40, EndLine: 52, CommitSHA: "def"}},
		{base, "", "7", "", api.DecisionAnchor{FilePath: "a.go", StartLine: 7, EndLine: 7, CommitSHA: "abc"}},
		{base, "b.go", "", "", api.DecisionAnchor{FilePath: "b.go", StartLine: 40, EndLine: 52, CommitSHA: "abc"}},
	} {
		a, err := buildAnchor(c.base, c.path, c.lines, c.commit)
		if err != nil || a.Value == nil || *a.Value != c.want {
			t.Errorf("%+v: anchor = %+v, %v", c, a.Value, err)
		}
	}
	if *base != (api.DecisionAnchor{FilePath: "a.go", StartLine: 40, EndLine: 52, CommitSHA: "abc"}) {
		t.Errorf("base was changed: %+v", *base)
	}
}

// TestDecisionCommandBodies runs write commands against a test server and
// checks each sends its whole change as one batch. It calls RunE directly:
// going through rootCmd would load the real config and point the client away
// from the test server.
func TestDecisionCommandBodies(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+string(data))
		_, _ = w.Write([]byte(`{"decisions":[{"id":7,"status":"open",` +
			`"anchor":{"commit_sha":"abc","file_path":"a.go","start_line":40,"end_line":52}},` +
			`{"id":8,"status":"open","anchor":null}]}`))
	}))
	defer srv.Close()
	host, token := config.Av.Aviator.APIHost, config.Av.Aviator.APIToken
	config.Av.Aviator.APIHost, config.Av.Aviator.APIToken = srv.URL, "test-token"
	t.Cleanup(func() { config.Av.Aviator.APIHost, config.Av.Aviator.APIToken = host, token })
	decisionsFlags.JSON = true
	t.Cleanup(func() { decisionsFlags.JSON = false })

	for _, c := range []struct {
		cmd  *cobra.Command
		args []string
		want string
	}{
		{
			decisionsSettleCmd,
			[]string{
				"r/4", "7", "#8", "--outcome", "fixed", "--resolution", "why",
				"--role", "coding_agent", "--model", "m", "--harness", "claude-code", "--skill", "a", "--skill", "b",
			},
			`POST /api/v1/verify/4/decisions/batch {"decisions":[` +
				`{"id":7,"author":{"role":"coding_agent","model":"m","harness":"claude-code","skills":["a","b"]},` +
				`"outcome":"fixed","resolution":"why"},` +
				`{"id":8,"author":{"role":"coding_agent","model":"m","harness":"claude-code","skills":["a","b"]},` +
				`"outcome":"fixed","resolution":"why"}]}`,
		},
		{
			// The anchor flags are merged into each decision's current anchor,
			// read in one GET, and the change is still one batch.
			decisionsEditCmd,
			[]string{"r/4", "7", "8", "--commit", "def", "--role", "person"},
			"GET /api/v1/verify/4/decisions \n" +
				`POST /api/v1/verify/4/decisions/batch {"decisions":[` +
				`{"id":7,"author":{"role":"person"},"anchor":{"commit_sha":"def","file_path":"a.go","start_line":40,"end_line":52}},` +
				`{"id":8,"author":{"role":"person"},"anchor":{"commit_sha":"def"}}]}`,
		},
		{
			decisionsReopenCmd,
			[]string{"r/4", "7", "--role", "person", "--body", "not fixed"},
			`POST /api/v1/verify/4/decisions/batch {"decisions":[{"id":7,"author":{"role":"person"},` +
				`"outcome":null,"comments":[{"author":{"role":"person"},"body":"not fixed"}]}]}`,
		},
		{
			decisionsCreateCmd,
			[]string{
				"r/4", "--type", "risk", "--title", "T", "--body", "why",
				"--path", "a.go", "--lines", "3", "--role", "review_agent",
			},
			`POST /api/v1/verify/4/decisions/batch {"decisions":[{"author":{"role":"review_agent"},` +
				`"type":"risk","title":"T","anchor":{"file_path":"a.go","start_line":3,"end_line":3},` +
				`"comments":[{"author":{"role":"review_agent"},"body":"why"}]}]}`,
		},
	} {
		got = nil
		c.cmd.SetContext(context.Background())
		if err := c.cmd.ParseFlags(c.args); err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if err := c.cmd.RunE(c.cmd, c.cmd.Flags().Args()); err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if strings.Join(got, "\n") != c.want {
			t.Errorf("%v: requests =\n%s\nwant\n%s", c.args, strings.Join(got, "\n"), c.want)
		}
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
	req, err := readDecisionBatch(write(`{"decisions":[` +
		`{"author":{"role":"coding_agent","model":"m"},"type":"risk","title":"T",` +
		`"comments":[{"author":{"role":"coding_agent"},"body":"b","model_metadata":{"subagent":"r"}}]},` +
		`{"id":8,"author":{"role":"person"},"outcome":"fixed","resolution":"why"},` +
		`{"id":9,"author":{"role":"person"},"outcome":null,"anchor":null}]}`))
	if err != nil || len(req.Decisions) != 3 {
		t.Fatalf("req = %+v, err = %v", req, err)
	}
	if d := req.Decisions[0]; d.Author.Model != "m" || d.Outcome.Set || d.Comments[0].ModelMetadata["subagent"] != "r" {
		t.Errorf("create = %+v", d)
	}
	if d := req.Decisions[1]; *d.Outcome.Value != "fixed" || d.Resolution != "why" {
		t.Errorf("settle = %+v", d)
	}
	if d := req.Decisions[2]; !d.Outcome.Set || d.Outcome.Value != nil || !d.Anchor.Set || d.Anchor.Value != nil {
		t.Errorf("reopen = %+v", d)
	}
	for name, content := range map[string]string{
		"via":                  `{"via":"aviator-cli","decisions":[{"id":7,"author":{"role":"person"}}]}`,
		"batch model_metadata": `{"model_metadata":{"model":"m"},"decisions":[{"id":7,"author":{"role":"person"}}]}`,
		"unknown key":          `{"decisions":[{"id":7,"author":{"role":"person"},"coments":[]}]}`,
		"old nested format":    `{"decisions":[{"decision":{"id":7}}]}`,
		"author name":          `{"decisions":[{"id":7,"author":{"role":"person","name":"Sam"}}]}`,
		"bare array":           `[{"id":7}]`,
		"empty":                `{"decisions":[]}`,
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
