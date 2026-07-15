package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEditRunbookCriteria(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/api/v1/runbook/123/acceptance-criteria" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var req EditRunbookCriteriaRequest
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ExpectedVersion != 4 || len(req.AcceptanceCriteria) != 2 {
			t.Errorf("request = %+v", req)
		}
		_, _ = w.Write([]byte(`{"runbook_number":123,"new_version":5,"criteria_count":2,"message":"updated"}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).EditRunbookCriteria(context.Background(), 123, EditRunbookCriteriaRequest{
		ExpectedVersion:    4,
		AcceptanceCriteria: []string{"one", "two"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.NewVersion == nil || *resp.NewVersion != 5 {
		t.Errorf("new_version = %v", resp.NewVersion)
	}
	if resp.CriteriaCount != 2 {
		t.Errorf("criteria_count = %d", resp.CriteriaCount)
	}
}

func TestEditRunbookCriteriaStaleVersion(t *testing.T) {
	// Mirrors the real backend 409 envelope, including the extra version
	// fields the client ignores.
	const body = `{"error":"stale-runbook-version",` +
		`"message":"Runbook is currently at version 6, but expected_version=4. Re-read the runbook and retry.",` +
		`"current_version":6,"expected_version":4}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).EditRunbookCriteria(context.Background(), 123, EditRunbookCriteriaRequest{
		ExpectedVersion:    4,
		AcceptanceCriteria: []string{"one"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "currently at version 6") ||
		!strings.Contains(got, "409") {
		t.Fatalf("error = %q, want stale-version message and status", got)
	}
}

func TestEditRunbookCriteriaNoOp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"runbook_number":123,"new_version":null,"criteria_count":2,"message":"Criteria unchanged."}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).EditRunbookCriteria(context.Background(), 123, EditRunbookCriteriaRequest{
		ExpectedVersion:    4,
		AcceptanceCriteria: []string{"one", "two"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.NewVersion != nil {
		t.Errorf("new_version = %v, want nil", resp.NewVersion)
	}
	if resp.Message != "Criteria unchanged." {
		t.Errorf("message = %q", resp.Message)
	}
}
