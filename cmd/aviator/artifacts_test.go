package main

import (
	"context"
	"encoding/json"
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

const artifactSetReply = `{"artifact_set":{"id":3,"summary":"Signed webhooks","head_sha":"abc",` +
	`"author":{"role":"coding_agent","model":"claude-fable-5-1","harness":"claude-code","skills":null},"created_at":"2026-11-17T00:00:00+00:00","artifacts":[` +
	`{"id":31,"category":"Services","title":"Gate","diagram":"flowchart LR\n  a --> b",` +
	`"body":null,"references":[{"type":"file","target":"a.py:40-48"}],"pr_numbers":[12]},` +
	`{"id":32,"category":"Data model","title":"Model","diagram":null,` +
	`"body":"Adds a table.","references":[],"pr_numbers":null}]}}`

func writeArtifactFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifacts.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArtifactsSubmitSendsFile(t *testing.T) {
	got := useTestServer(t, artifactSetReply)
	body := `{"summary": "s", "author": {"role": "coding_agent"}, "artifacts": [{"title": "gate", "x": null}]}` + "\n"
	out := runCommand(t, artifactsSubmitCmd, "r/4", "--file", writeArtifactFile(t, body))
	if want := "PUT /api/v1/verify/4/artifacts " + body; len(*got) != 1 || (*got)[0] != want {
		t.Errorf("requests =\n%s\nwant\n%s", strings.Join(*got, "\n"), want)
	}
	if !strings.Contains(out, "r/4: 2 artifacts submitted") {
		t.Errorf("output = %q", out)
	}
}

func TestArtifactsSubmitJSON(t *testing.T) {
	useTestServer(t, artifactSetReply)
	artifactsFlags.JSON = true
	t.Cleanup(func() { artifactsFlags.JSON = false })
	out := runCommand(t, artifactsSubmitCmd, "r/4", "--file", writeArtifactFile(t, "{}"))
	var parsed struct {
		ID          string `json:"id"`
		ArtifactSet struct {
			ID        int            `json:"id"`
			Summary   string         `json:"summary"`
			Author    map[string]any `json:"author"`
			CreatedAt string         `json:"created_at"`
			Artifacts []struct {
				ID         int              `json:"id"`
				References []map[string]any `json:"references"`
			} `json:"artifacts"`
		} `json:"artifact_set"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	s := parsed.ArtifactSet
	if parsed.ID != "r/4" || s.ID != 3 || len(s.Artifacts) != 2 || s.Artifacts[0].ID != 31 || s.Artifacts[1].ID != 32 ||
		s.Artifacts[1].References == nil ||
		s.Author["role"] != "coding_agent" || s.Author["model"] != "claude-fable-5-1" ||
		s.CreatedAt != "2026-11-17T00:00:00+00:00" || s.Summary != "Signed webhooks" {
		t.Errorf("json = %s", out)
	}
}

func TestArtifactsList(t *testing.T) {
	got := useTestServer(t, artifactSetReply)
	out := runCommand(t, artifactsListCmd, "r/4")
	if want := "GET /api/v1/verify/4/artifacts "; (*got)[0] != want {
		t.Errorf("request = %q, want %q", (*got)[0], want)
	}
	for _, want := range []string{
		"r/4 Signed webhooks\n",
		"submitted 2026-11-17T00:00:00+00:00 · as coding_agent (claude-fable-5-1, claude-code) · at abc\n",
		"ID   CATEGORY    DIAGRAM  BODY  REFS  TITLE\n",
		"#31  Services    yes      no    1     Gate\n",
		"#32  Data model  no       yes   0     Model\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
}

func TestArtifactsListNone(t *testing.T) {
	useTestServer(t, `{"artifact_set":null}`)
	if out := runCommand(t, artifactsListCmd, "r/4"); !strings.HasPrefix(out, "No artifacts on r/4 yet.") {
		t.Errorf("output = %q", out)
	}

	artifactsFlags.JSON = true
	t.Cleanup(func() { artifactsFlags.JSON = false })
	if out := runCommand(t, artifactsListCmd, "r/4"); out != "{\n  \"id\": \"r/4\",\n  \"artifact_set\": null\n}\n" {
		t.Errorf("json = %q", out)
	}
}

func TestArtifactsShow(t *testing.T) {
	useTestServer(t, artifactSetReply)
	out := runCommand(t, artifactsShowCmd, "r/4", "31")
	for _, want := range []string{
		"#31 Gate\n  Services · PRs #12\n" +
			"  submitted 2026-11-17T00:00:00+00:00 · as coding_agent (claude-fable-5-1, claude-code) · at abc\n",
		"References:\n  a.py:40-48\n",
		"Diagram:\n```mermaid\nflowchart LR\n  a --> b\n```\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Body:") {
		t.Errorf("show has a body for a null one:\n%s", out)
	}

	artifactsFlags.JSON = true
	t.Cleanup(func() { artifactsFlags.JSON = false })
	var parsed struct {
		ID        string            `json:"id"`
		HeadSHA   string            `json:"head_sha"`
		Author    map[string]string `json:"author"`
		CreatedAt string            `json:"created_at"`
		Artifact  struct {
			ID        int    `json:"id"`
			Body      string `json:"body"`
			PRNumbers []int  `json:"pr_numbers"`
		} `json:"artifact"`
	}
	out = runCommand(t, artifactsShowCmd, "r/4", "#32")
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if parsed.ID != "r/4" || parsed.Artifact.ID != 32 || parsed.Artifact.Body != "Adds a table." ||
		parsed.HeadSHA != "abc" || parsed.Author["role"] != "coding_agent" ||
		parsed.CreatedAt != "2026-11-17T00:00:00+00:00" {
		t.Errorf("json = %s", out)
	}
}

// TestArtifactsShowJSONNullHeadSHA checks show --json leaves out a set's null
// head_sha, like list --json does.
func TestArtifactsShowJSONNullHeadSHA(t *testing.T) {
	useTestServer(t, strings.Replace(artifactSetReply, `"head_sha":"abc"`, `"head_sha":null`, 1))
	artifactsFlags.JSON = true
	t.Cleanup(func() { artifactsFlags.JSON = false })
	out := runCommand(t, artifactsShowCmd, "r/4", "31")
	if strings.Contains(out, `"head_sha"`) || !strings.Contains(out, `"created_at"`) {
		t.Errorf("json = %s", out)
	}
}

// TestArtifactsShowUnknownID checks an id that isn't in the set, or isn't a
// number, fails with the ids and titles there are.
func TestArtifactsShowUnknownID(t *testing.T) {
	useTestServer(t, artifactSetReply)
	cmd := artifactsShowCmd
	cmd.SetContext(context.Background())
	const known = ". The artifacts on r/4 are:\n  #31  Gate\n  #32  Model"
	for arg, want := range map[string]string{
		"99":   "no artifact #99" + known,
		"gate": `invalid artifact ID "gate", expected a positive number` + known,
		"0":    `invalid artifact ID "0", expected a positive number` + known,
		"#-3":  `invalid artifact ID "#-3", expected a positive number` + known,
	} {
		if err := cmd.RunE(cmd, []string{"r/4", arg}); err == nil || err.Error() != want {
			t.Errorf("show %s: err = %v, want %q", arg, err, want)
		}
	}
}

func TestArtifactsErrorUnsupported(t *testing.T) {
	bare := &api.Error{Status: http.StatusNotFound, Method: http.MethodPut, Path: "/api/v1/verify/4/artifacts"}
	want := "this Aviator backend doesn't support artifacts yet; PUT /api/v1/verify/4/artifacts returned 404"
	if err := artifactsError(bare); err == nil || err.Error() != want {
		t.Errorf("bare 404: %v", err)
	}
	notFound := &api.Error{Status: http.StatusNotFound, Code: "not-found", Message: "Runbook not found."}
	if err := artifactsError(notFound); err != notFound {
		t.Errorf("JSON not-found was rewritten: %v", err)
	}
}

// TestArtifactsSubmitForbidden checks a 403 for a credential that may not
// write reaches the user with the server's message.
func TestArtifactsSubmitForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error": "forbidden", "message": "Artifacts are written from the Aviator web app, ` +
			`aviator-cli or a user access token."}`))
	}))
	t.Cleanup(srv.Close)
	host, token := config.Av.Aviator.APIHost, config.Av.Aviator.APIToken
	config.Av.Aviator.APIHost, config.Av.Aviator.APIToken = srv.URL, "test-token"
	t.Cleanup(func() { config.Av.Aviator.APIHost, config.Av.Aviator.APIToken = host, token })

	cmd := artifactsSubmitCmd
	cmd.SetContext(context.Background())
	if err := cmd.ParseFlags([]string{"--file", writeArtifactFile(t, "{}")}); err != nil {
		t.Fatal(err)
	}
	err := cmd.RunE(cmd, []string{"r/4"})
	if err == nil || !strings.Contains(err.Error(), "Artifacts are written from the Aviator web app") {
		t.Errorf("err = %v", err)
	}
}

// TestArtifactsJSONOmitsAbsentFields checks a field the server sent as null
// is left out of --json, while references stays an array and fields with a
// value stay in.
func TestArtifactsJSONOmitsAbsentFields(t *testing.T) {
	reply := strings.Replace(artifactSetReply, `"head_sha":"abc"`, `"head_sha":null`, 1)
	useTestServer(t, strings.Replace(reply, `"summary":"Signed webhooks"`, `"summary":null`, 1))
	artifactsFlags.JSON = true
	t.Cleanup(func() { artifactsFlags.JSON = false })
	out := runCommand(t, artifactsSubmitCmd, "r/4", "--file", writeArtifactFile(t, "{}"))
	var parsed struct {
		ArtifactSet struct {
			Artifacts []map[string]any `json:"artifacts"`
		} `json:"artifact_set"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	for _, key := range []string{`"head_sha"`, `"summary"`} {
		if strings.Contains(out, key) {
			t.Errorf("null %s printed:\n%s", key, out)
		}
	}
	diagramOnly, textOnly := parsed.ArtifactSet.Artifacts[0], parsed.ArtifactSet.Artifacts[1]
	for key, present := range map[string]bool{"diagram": true, "body": false, "pr_numbers": true} {
		if _, ok := diagramOnly[key]; ok != present {
			t.Errorf("diagram-only artifact has %s = %v, want %v:\n%s", key, ok, present, out)
		}
	}
	for key, present := range map[string]bool{"diagram": false, "body": true, "pr_numbers": false} {
		if _, ok := textOnly[key]; ok != present {
			t.Errorf("text-only artifact has %s = %v, want %v:\n%s", key, ok, present, out)
		}
	}
	if refs, ok := textOnly["references"].([]any); !ok || len(refs) != 0 {
		t.Errorf("empty references = %v, want []", textOnly["references"])
	}
}

// TestArtifactsReadVerifyNotEnabled checks list and show fail with the
// server's message when the account doesn't have Verify.
func TestArtifactsReadVerifyNotEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"verify-not-enabled","message":"Verification is not enabled."}`))
	}))
	t.Cleanup(srv.Close)
	host, token := config.Av.Aviator.APIHost, config.Av.Aviator.APIToken
	config.Av.Aviator.APIHost, config.Av.Aviator.APIToken = srv.URL, "test-token"
	t.Cleanup(func() { config.Av.Aviator.APIHost, config.Av.Aviator.APIToken = host, token })

	for _, tc := range []struct {
		cmd  *cobra.Command
		args []string
	}{
		{artifactsListCmd, []string{"r/4"}},
		{artifactsShowCmd, []string{"r/4", "31"}},
	} {
		tc.cmd.SetContext(context.Background())
		err := tc.cmd.RunE(tc.cmd, tc.args)
		if err == nil || !strings.Contains(err.Error(), "(403): Verification is not enabled.") {
			t.Errorf("%s: err = %v", tc.cmd.Name(), err)
		}
	}
}

func TestArtifactsListWithoutSummary(t *testing.T) {
	useTestServer(t, strings.Replace(artifactSetReply, `"summary":"Signed webhooks"`, `"summary":null`, 1))
	out := runCommand(t, artifactsListCmd, "r/4")
	if want := "r/4\n  submitted 2026-11-17T00:00:00+00:00 · "; !strings.HasPrefix(out, want) {
		t.Errorf("list without a summary:\n%s", out)
	}
	if !strings.Contains(out, "#31  Services    yes      no    1     Gate\n") {
		t.Errorf("list lost its table:\n%s", out)
	}
}

func TestArtifactsListMultilineSummary(t *testing.T) {
	useTestServer(t, strings.Replace(artifactSetReply, `"summary":"Signed webhooks"`,
		`"summary":"Signed webhooks.\nThe queue never sees a bad one."`, 1))
	out := runCommand(t, artifactsListCmd, "r/4")
	if want := "r/4 Signed webhooks.\n  The queue never sees a bad one.\n  submitted "; !strings.HasPrefix(out, want) {
		t.Errorf("multi-line summary:\n%s", out)
	}
}
