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
