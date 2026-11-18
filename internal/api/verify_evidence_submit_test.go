package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSubmitEvidenceManifest(t *testing.T) {
	var got EvidenceManifest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/verify/123/evidence" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"runbook_number":123,"commit_sha":"4f2c9e1a","upload_url_ttl_seconds":900,` +
			`"scenarios":[{"id":7,"scenario_run_id":70,"steps":[{"id":1,"evidence":[]},` +
			`{"id":2,"evidence":[{"id":900,"upload_url":"https://bucket/900","content_type":"image/png"}]}]}]}`))
	}))
	defer srv.Close()

	key := "ac-1"
	resp, err := newTestClient(srv).SubmitEvidenceManifest(context.Background(), 123, EvidenceManifest{
		CommitSHA: "4f2c9e1a",
		Scenarios: []ManifestScenario{{
			Summary:  "toggle the flag",
			Criteria: []ManifestCriterion{{StableKey: &key}},
			Steps: []ManifestStep{
				{Text: "open"},
				{Text: "capture", Evidence: []ManifestEvidence{{Type: "screenshot", Label: "home", ContentType: "image/png", SizeBytes: 4}}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CommitSHA != "4f2c9e1a" || len(got.Scenarios) != 1 || got.Scenarios[0].Criteria[0].StableKey == nil {
		t.Errorf("sent = %+v", got)
	}
	if resp.Scenarios[0].ScenarioRunID != 70 || resp.Scenarios[0].Steps[1].Evidence[0].ID != 900 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestCompleteEvidenceUploads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/verify/123/evidence/complete" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"evidence_ids":[900,901]}` {
			t.Errorf("body = %s", body)
		}
		_, _ = w.Write([]byte(`{"runbook_number":123,"uploaded_evidence_ids":[900],"missing_evidence_ids":[901],"completed_scenario_run_ids":[]}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).CompleteEvidenceUploads(context.Background(), 123, []int{900, 901})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.UploadedEvidenceIDs) != 1 || len(resp.MissingEvidenceIDs) != 1 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestUploadEvidenceFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.png")
	if err := os.WriteFile(path, []byte("\x89PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.Header.Get("Content-Type") != "image/png" {
			t.Errorf("%s %s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("bearer token sent to storage")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "\x89PNG" || r.ContentLength != 4 {
			t.Errorf("body = %q, length = %d", body, r.ContentLength)
		}
	}))
	defer srv.Close()

	err := newTestClient(srv).UploadEvidenceFile(context.Background(), UploadSlot{
		ID: 900, UploadURL: srv.URL + "/900", ContentType: "image/png",
	}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUploadEvidenceFileReportsStorageRefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.png")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("SignatureDoesNotMatch"))
	}))
	defer srv.Close()

	err := newTestClient(srv).UploadEvidenceFile(context.Background(), UploadSlot{
		ID: 900, UploadURL: srv.URL + "/900", ContentType: "image/png",
	}, path)
	if err == nil || !contains(err.Error(), "SignatureDoesNotMatch") {
		t.Errorf("err = %v", err)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
