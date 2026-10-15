package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubmitVerifySendsScenarioGuidance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if want := `"scenario_guidance":"log in as the seeded admin"`; !strings.Contains(string(body), want) {
			t.Errorf("body = %s, missing %s", body, want)
		}
		_, _ = w.Write([]byte(`{"runbook_number":7}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).SubmitVerify(context.Background(), SubmitVerifyRequest{
		Repository:         Repository{Org: "aviator-co", Name: "mergeit"},
		Intent:             "gate the banner",
		AcceptanceCriteria: []string{"banner hidden when flag off"},
		ScenarioGuidance:   "log in as the seeded admin",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubmitVerifyOmitsEmptyScenarioGuidance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "scenario_guidance") {
			t.Errorf("body = %s, should omit scenario_guidance", body)
		}
		_, _ = w.Write([]byte(`{"runbook_number":7}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).SubmitVerify(context.Background(), SubmitVerifyRequest{
		Repository:         Repository{Org: "aviator-co", Name: "mergeit"},
		Intent:             "gate the banner",
		AcceptanceCriteria: []string{"banner hidden when flag off"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
