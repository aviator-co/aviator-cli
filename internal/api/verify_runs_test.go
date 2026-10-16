package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListVerifyRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/verify/123/runs" || r.URL.Query().Get("per_page") != "20" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"runs":[{"id":900,"status":"passed","trigger_source":"manual",` +
			`"evaluator_only":false,"runbook_version":4,"commit_sha":"abc","criteria_total":12,` +
			`"criteria_passed":12,"criteria_failed":0,"criteria_skipped":0,"criteria_waived":0,` +
			`"error_message":null,"created_at":"2026-10-08T12:00:00Z","started_at":null,"completed_at":null}],` +
			`"page":1,"per_page":20,"has_more":true}`))
	}))
	defer srv.Close()

	runs, more, err := newTestClient(srv).ListVerifyRuns(context.Background(), 123, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !more || len(runs) != 1 {
		t.Fatalf("runs = %+v, more = %v", runs, more)
	}
	if r := runs[0]; r.ID != 900 || r.Status != "passed" || *r.RunbookVersion != 4 || r.CreatedAt != "2026-10-08T12:00:00Z" {
		t.Errorf("run = %+v", r)
	}
}
