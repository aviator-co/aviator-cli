package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateRunbookWithSpecAndCriteria(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/runbook" {
			t.Errorf("path = %q", r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"runbook_number":7,"url":"https://app/7","status":"queued"}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).CreateRunbook(context.Background(), CreateRunbookRequest{
		Repository:         Repository{Org: "acme", Name: "web"},
		Intent:             "do the thing",
		Oneshot:            true,
		SpecFile:           &SpecFile{Filename: "spec.md", Content: "# spec"},
		AcceptanceCriteria: []string{"one", "two"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RunbookNumber != 7 {
		t.Errorf("runbook_number = %d, want 7", resp.RunbookNumber)
	}

	spec, ok := body["spec_file"].(map[string]any)
	if !ok {
		t.Fatalf("spec_file not an object: %v", body["spec_file"])
	}
	if spec["filename"] != "spec.md" || spec["content"] != "# spec" {
		t.Errorf("spec_file = %v", spec)
	}
	crit, ok := body["acceptance_criteria"].([]any)
	if !ok || len(crit) != 2 || crit[0] != "one" || crit[1] != "two" {
		t.Errorf("acceptance_criteria = %v", body["acceptance_criteria"])
	}
}

func TestCreateRunbookOmitsSpecAndCriteria(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"runbook_number":1,"url":"https://app/1","status":"queued"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).CreateRunbook(context.Background(), CreateRunbookRequest{
		Repository: Repository{Org: "acme", Name: "web"},
		Intent:     "do the thing",
		Oneshot:    true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := body["spec_file"]; present {
		t.Errorf("spec_file should be omitted, got %v", body["spec_file"])
	}
	if _, present := body["acceptance_criteria"]; present {
		t.Errorf("acceptance_criteria should be omitted, got %v", body["acceptance_criteria"])
	}
}
