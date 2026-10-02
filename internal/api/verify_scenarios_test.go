package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetVerifyScenarios(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/verify/123/runs/latest/scenarios" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"runbook_number":123,"run_id":900,"run_status":"failed","commit_sha":"abc",` +
			`"scenario_runs":[{"id":1,"reused":false,"status":"completed","tool_call_count":3,` +
			`"started_at":"Fri, 02 Oct 2026 20:24:50 GMT",` +
			`"scenario":{"id":7,"summary":"log in","steps":[],"criteria":[{"stable_key":"k","baseline_invariant_id":null}]},` +
			`"evidence":[{"id":4567,"type":"trace","label":"Run trace","url":"https://x/4567"}]}]}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).GetVerifyScenarios(context.Background(), 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RunStatus != "failed" || len(resp.ScenarioRuns) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	if ev := resp.ScenarioRuns[0].Evidence; len(ev) != 1 || ev[0].ID != 4567 || ev[0].Type != "trace" {
		t.Errorf("evidence = %+v", ev)
	}
}
