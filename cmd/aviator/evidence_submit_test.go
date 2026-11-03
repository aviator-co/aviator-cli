package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
)

func writeManifestFixture(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"shots/home.png": "\x89PNG",
		"api/flags.json": `{"banner": false}`,
	} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "evidence.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const goodManifest = `
commit: 4f2c9e1a7b3d5c6e8f9a0b1c2d3e4f5a6b7c8d9e
scenarios:
  - summary: Toggle the flag and reload
    criteria:
      - stable_key: ac-1
    steps:
      - text: Open the home page
      - text: Capture it without the banner
        evidence:
          - type: screenshot
            file: shots/home.png
            label: Home page, no banner
          - type: api_response
            file: ./api/flags.json
            label: GET /api/flags shows banner=false
`

func TestLoadEvidenceManifest(t *testing.T) {
	path := writeManifestFixture(t, goodManifest)

	loaded, err := loadEvidenceManifest(path, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := loaded.manifest
	if m.CommitSHA != "4f2c9e1a7b3d5c6e8f9a0b1c2d3e4f5a6b7c8d9e" {
		t.Errorf("commit = %q", m.CommitSHA)
	}
	if len(m.Scenarios) != 1 || len(m.Scenarios[0].Steps) != 2 {
		t.Fatalf("manifest = %+v", m)
	}
	if key := m.Scenarios[0].Criteria[0].StableKey; key == nil || *key != "ac-1" {
		t.Errorf("criteria = %+v", m.Scenarios[0].Criteria)
	}
	evidence := m.Scenarios[0].Steps[1].Evidence
	want := []api.ManifestEvidence{
		{Type: "screenshot", Label: "Home page, no banner", ContentType: "image/png", SizeBytes: 4},
		{Type: "api_response", Label: "GET /api/flags shows banner=false", ContentType: "application/json", SizeBytes: 17},
	}
	for i, w := range want {
		if evidence[i] != w {
			t.Errorf("evidence[%d] = %+v, want %+v", i, evidence[i], w)
		}
	}
	if p, ok := loaded.pathAt(0, 1, 1); !ok || !strings.HasSuffix(p, filepath.Join("api", "flags.json")) {
		t.Errorf("pathAt(0,1,1) = %q, %v", p, ok)
	}
	if _, ok := loaded.pathAt(0, 0, 0); ok {
		t.Errorf("a step without evidence has no paths")
	}
}

func TestLoadEvidenceManifestCommitFlagWins(t *testing.T) {
	path := writeManifestFixture(t, goodManifest)
	loaded, err := loadEvidenceManifest(path, "9a01bb7c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded.manifest.CommitSHA != "9a01bb7c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a" {
		t.Errorf("commit = %q", loaded.manifest.CommitSHA)
	}
}

func TestLoadEvidenceManifestRejectsBadEntries(t *testing.T) {
	cases := map[string]string{
		"missing file":  strings.Replace(goodManifest, "shots/home.png", "shots/nope.png", 1),
		"bad extension": strings.Replace(goodManifest, "api/flags.json", "api/flags.csv", 1),
		"no label":      strings.Replace(goodManifest, "label: Home page, no banner", "label: ''", 1),
		"no scenarios":  "commit: 4f2c9e1a7b3d5c6e8f9a0b1c2d3e4f5a6b7c8d9e\nscenarios: []\n",
		"no commit":     strings.Replace(goodManifest, "commit: 4f2c9e1a7b3d5c6e8f9a0b1c2d3e4f5a6b7c8d9e\n", "", 1),
		"short commit":  strings.Replace(goodManifest, "commit: 4f2c9e1a7b3d5c6e8f9a0b1c2d3e4f5a6b7c8d9e", "commit: 4f2c9e1", 1),
	}
	for name, manifest := range cases {
		path := writeManifestFixture(t, manifest)
		if _, err := loadEvidenceManifest(path, ""); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestEvidenceSubmitJSON(t *testing.T) {
	got := decodeJSON(t, newEvidenceSubmitJSON("r/123", &api.EvidenceSubmission{
		CommitSHA: "4f2c9e1a",
		Scenarios: []api.SubmittedScenario{{ID: 7, ScenarioRunID: 70}},
	}, &api.CompleteUploadsResponse{
		UploadedEvidenceIDs:     []int{1, 2},
		MissingEvidenceIDs:      []int{},
		CompletedScenarioRunIDs: []int{70},
	}, nil))
	if got["id"] != "r/123" || got["commit_sha"] != "4f2c9e1a" {
		t.Errorf("top level = %v", got)
	}
	if ids := got["uploaded_evidence_ids"].([]any); len(ids) != 2 {
		t.Errorf("uploaded = %v", ids)
	}
	if errs := got["upload_errors"].([]any); len(errs) != 0 {
		t.Errorf("upload_errors = %v", errs)
	}
}
