package api

import (
	"context"
	"encoding/json"
	"testing"
)

const artifactSetJSON = `{"artifact_set":{"id":3,"summary":"Signed webhooks","head_sha":null,` +
	`"author":{"role":"coding_agent","model":"claude-fable-5-1","harness":"claude-code","skills":null},"created_at":"2026-11-17T00:00:00+00:00","artifacts":[` +
	`{"id":31,"category":"Services","title":"Gate","diagram":"flowchart LR\n  a --> b",` +
	`"body":null,"references":[{"type":"file","target":"a.py:40-48"}],"pr_numbers":null}]}}`

// TestSubmitArtifacts checks the body goes out byte for byte, since unknown
// keys are the server's to reject.
func TestSubmitArtifacts(t *testing.T) {
	srv, got := recordRequests(t, artifactSetJSON)
	body := `{"summary": "s", "author": {"role": "coding_agent"}, "artifacts": [], "extra": null}` + "\n"
	resp, err := newTestClient(srv).SubmitArtifacts(context.Background(), 123, []byte(body))
	if err != nil || resp.ArtifactSet == nil || len(resp.ArtifactSet.Artifacts) != 1 {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
	if set := resp.ArtifactSet; set.Summary == nil || *set.Summary != "Signed webhooks" || set.Author == nil || set.Author.Harness != "claude-code" || set.CreatedAt == "" {
		t.Errorf("set = %+v", set)
	}
	a := resp.ArtifactSet.Artifacts[0]
	if a.ID != 31 || a.Body != nil || a.Diagram == nil || a.References[0].Target != "a.py:40-48" || a.PRNumbers != nil {
		t.Errorf("artifact = %+v", a)
	}
	if want := "PUT /api/v1/verify/123/artifacts " + body; (*got)[0] != want {
		t.Errorf("request =\n%s\nwant\n%s", (*got)[0], want)
	}
}

func TestArtifactSetNullSummary(t *testing.T) {
	var resp ArtifactSetResponse
	if err := json.Unmarshal([]byte(`{"artifact_set":{"id":3,"summary":null,"artifacts":[]}}`), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ArtifactSet == nil || resp.ArtifactSet.Summary != nil {
		t.Errorf("set = %+v", resp.ArtifactSet)
	}
}
