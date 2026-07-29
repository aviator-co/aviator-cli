package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetRunbookDetail(t *testing.T) {
	const body = `{
		"runbook_number": 123,
		"url": "https://app.aviator.co/runbook/123",
		"runbook_version": 4,
		"runbook_state": {
			"target_branch": "main",
			"working_branch": "feature",
			"steps": [
				{"step_number": "1", "title": "one", "status": "completed"},
				{"step_number": "1.1", "title": "two", "status": "in_progress"}
			]
		},
		"acceptance_criteria": [
			{"ordinal": 1, "raw_text": "does the thing", "source": "user"}
		],
		"latest_verification": {
			"status": "failed",
			"trigger_source": "commit_push",
			"runbook_version": 4,
			"commit_sha": "abcdef1234567890",
			"criteria_total": 2,
			"criteria_passed": 1,
			"criteria_failed": 1,
			"criteria_skipped": 0,
			"criteria_waived": 0,
			"error_message": null,
			"failed_results": [
				{"criterion": "does the thing", "is_invariant": false, "is_waived": false,
				 "status": "fail", "reason": "nope", "evidence": {}, "location": null}
			]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/runbook/123/detail" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("fields"); got != "runbook_state,acceptance_criteria" {
			t.Errorf("fields = %q", got)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	_, detail, err := newTestClient(srv).GetRunbookDetail(
		context.Background(), 123, []string{"runbook_state", "acceptance_criteria"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.RunbookNumber != 123 {
		t.Errorf("runbook_number = %d", detail.RunbookNumber)
	}
	if detail.RunbookVersion == nil || *detail.RunbookVersion != 4 {
		t.Errorf("runbook_version = %v", detail.RunbookVersion)
	}
	if detail.RunbookState == nil || len(detail.RunbookState.Steps) != 2 {
		t.Fatalf("runbook_state not decoded: %+v", detail.RunbookState)
	}
	if detail.RunbookState.WorkingBranch == nil || *detail.RunbookState.WorkingBranch != "feature" {
		t.Errorf("working_branch = %v", detail.RunbookState.WorkingBranch)
	}
	if len(detail.AcceptanceCriteria) != 1 || detail.AcceptanceCriteria[0].RawText != "does the thing" {
		t.Errorf("acceptance_criteria = %+v", detail.AcceptanceCriteria)
	}
	v := detail.LatestVerification
	if v == nil || v.Status != "failed" || v.CriteriaFailed != 1 {
		t.Fatalf("latest_verification = %+v", v)
	}
	if v.CommitSHA == nil || *v.CommitSHA != "abcdef1234567890" {
		t.Errorf("commit_sha = %v", v.CommitSHA)
	}
	if len(v.FailedResults) != 1 || v.FailedResults[0].Reason == nil ||
		*v.FailedResults[0].Reason != "nope" {
		t.Errorf("failed_results = %+v", v.FailedResults)
	}
}

func TestGetRunbookDetailNoFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("expected no query string, got %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"runbook_number": 7, "url": "u", "runbook_version": null}`))
	}))
	defer srv.Close()

	_, detail, err := newTestClient(srv).GetRunbookDetail(context.Background(), 7, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.RunbookNumber != 7 || detail.RunbookVersion != nil {
		t.Errorf("detail = %+v", detail)
	}
}

func TestGetRunbookDetailRawBody(t *testing.T) {
	// A field the client structs don't model must survive verbatim in the raw
	// body returned alongside the decoded struct.
	const body = `{"runbook_number": 5, "url": "u", "runbook_version": 1, "future_field": "kept"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	raw, detail, err := newTestClient(srv).GetRunbookDetail(context.Background(), 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != body {
		t.Errorf("raw = %s", raw)
	}
	if detail.LatestVerificationPresent {
		t.Error("LatestVerificationPresent = true for a response without the key")
	}
}

func TestGetRunbookDetailVerificationPresence(t *testing.T) {
	// An explicit null must register as present (no runs yet), unlike an
	// absent key.
	const body = `{"runbook_number": 5, "url": "u", "runbook_version": 1,
		"acceptance_criteria": null, "latest_verification": null}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	_, detail, err := newTestClient(srv).GetRunbookDetail(context.Background(), 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !detail.LatestVerificationPresent {
		t.Error("LatestVerificationPresent = false for an explicit null")
	}
	if detail.LatestVerification != nil {
		t.Errorf("LatestVerification = %+v, want nil", detail.LatestVerification)
	}
}
