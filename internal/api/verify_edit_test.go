package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEditVerify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/api/v1/verify/123" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		const want = `{"intent":"new intent","acceptance_criteria":["one","two"],"expected_version":4}`
		if got := strings.TrimSpace(string(body)); got != want {
			t.Errorf("body = %s, want %s", got, want)
		}
		_, _ = w.Write([]byte(`{"runbook_number":123,"url":"https://app.aviator.co/r/123",` +
			`"working_branch":"feature","target_branch":null,"acceptance_criteria":["one","two"],` +
			`"intent":"new intent","version":5}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).EditVerify(context.Background(), 123, EditVerifyRequest{
		Intent:             "new intent",
		AcceptanceCriteria: []string{"one", "two"},
		ExpectedVersion:    4,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Version == nil || *resp.Version != 5 {
		t.Errorf("version = %v", resp.Version)
	}
	if resp.URL != "https://app.aviator.co/r/123" || resp.Intent != "new intent" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestEditVerifyIntentOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if got := strings.TrimSpace(string(body)); got != `{"intent":"new intent"}` {
			t.Errorf("body = %s", got)
		}
		_, _ = w.Write([]byte(`{"runbook_number":123,"intent":"new intent","version":null}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).EditVerify(context.Background(), 123, EditVerifyRequest{Intent: "new intent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Version != nil {
		t.Errorf("version = %v, want nil", *resp.Version)
	}
}

func TestEditVerifyStaleVersion(t *testing.T) {
	const body = `{"error":"stale-runbook-version",` +
		`"message":"r/123 is currently at version 6, but expected_version=4. Re-read the review and retry.",` +
		`"current_version":6,"expected_version":4}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).EditVerify(context.Background(), 123, EditVerifyRequest{
		AcceptanceCriteria: []string{"one"},
		ExpectedVersion:    4,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "currently at version 6") ||
		!strings.Contains(got, "409") {
		t.Fatalf("error = %q, want stale-version message and status", got)
	}
}
