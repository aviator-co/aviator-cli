package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTriggerVerifyRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/verify/42/run" {
			t.Errorf("path = %s, want /api/v1/verify/42/run", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		// Both fields are false, so the omitempty body is an empty object.
		if got := strings.TrimSpace(string(body)); got != "{}" {
			t.Errorf("body = %s, want {}", got)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{
			"runbook_number": 42,
			"url": "https://app.aviator.co/r/42",
			"run_id": 1234,
			"run_status": "pending",
			"deduplicated": false,
			"message": "Verification started."
		}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).TriggerVerifyRun(
		context.Background(), 42, TriggerVerifyRunRequest{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RunbookNumber != 42 || resp.RunID != 1234 ||
		resp.RunStatus != "pending" || resp.Deduplicated {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.URL != "https://app.aviator.co/r/42" || resp.Message != "Verification started." {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestTriggerVerifyRunSendsFlags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		for _, want := range []string{`"evaluator_only":true`, `"force":true`} {
			if !strings.Contains(string(body), want) {
				t.Errorf("body = %s, missing %s", body, want)
			}
		}
		_, _ = w.Write([]byte(`{"runbook_number":7,"run_status":"pending"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).TriggerVerifyRun(
		context.Background(), 7, TriggerVerifyRunRequest{EvaluatorOnly: true, Force: true},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTriggerVerifyRunDeduplicated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"runbook_number": 42,
			"url": "https://app.aviator.co/r/42",
			"run_id": 900,
			"run_status": "passed",
			"deduplicated": true,
			"message": "An equivalent run already exists; pass --force to run again."
		}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).TriggerVerifyRun(
		context.Background(), 42, TriggerVerifyRunRequest{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Deduplicated || resp.RunStatus != "passed" || resp.RunID != 900 {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestTriggerVerifyRunNotRunnable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"verify-not-runnable","message":"a run is already in progress"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).TriggerVerifyRun(
		context.Background(), 42, TriggerVerifyRunRequest{},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "a run is already in progress") ||
		!strings.Contains(got, "409") {
		t.Fatalf("error = %q, want to mention message and status", got)
	}
}
