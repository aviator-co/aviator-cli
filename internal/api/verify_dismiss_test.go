package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDismissCriteria(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/verify/123/dismissals" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		const want = `{"criteria":[{"stable_key":"abc123"},` +
			`{"baseline_invariant_id":42,"category":"accepted_risk","justification":"known"}]}`
		if got := strings.TrimSpace(string(body)); got != want {
			t.Errorf("body = %s, want %s", got, want)
		}
		_, _ = w.Write([]byte(`{"runbook_number":123,"new_version":6,"waived":1,` +
			`"deleted":1,"already_deleted":0,"criteria_count":2}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).DismissCriteria(context.Background(), 123, DismissCriteriaRequest{
		Criteria: []Dismissal{
			{StableKey: "abc123"},
			{BaselineInvariantID: 42, Category: "accepted_risk", Justification: "known"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.NewVersion == nil || *resp.NewVersion != 6 || resp.Waived != 1 ||
		resp.Deleted != 1 || resp.CriteriaCount != 2 {
		t.Errorf("resp = %+v", resp)
	}
}
