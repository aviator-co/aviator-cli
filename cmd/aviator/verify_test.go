package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
)

// decodeJSON renders a --json payload the way a caller consuming stdout sees it.
func decodeJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	return got
}

func assertJSONFields(t *testing.T, got, want map[string]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("field set = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

// Callers parse this object rather than scraping "Runbook #123" out of the
// human output, so the keys are the contract.
func TestVerifySubmitJSON(t *testing.T) {
	got := decodeJSON(t, newVerifySubmitJSON(&api.SubmitVerifyResponse{
		RunbookNumber:      123,
		URL:                "https://app.aviator.co/r/123",
		WorkingBranch:      "feature",
		TargetBranch:       "main",
		AcceptanceCriteria: []string{"one", "two"},
	}))
	assertJSONFields(t, got, map[string]any{
		"runbook_number": float64(123),
		"runbook_id":     "r/123",
		"url":            "https://app.aviator.co/r/123",
		"working_branch": "feature",
		"target_branch":  "main",
		"criteria_count": float64(2),
	})
}

// A submission without a working branch still has to carry every key, so a
// caller can read the field instead of discovering it went missing.
func TestVerifySubmitJSONKeepsEmptyFields(t *testing.T) {
	got := decodeJSON(t, newVerifySubmitJSON(&api.SubmitVerifyResponse{
		RunbookNumber: 7,
		URL:           "https://app.aviator.co/r/7",
	}))
	assertJSONFields(t, got, map[string]any{
		"runbook_number": float64(7),
		"runbook_id":     "r/7",
		"url":            "https://app.aviator.co/r/7",
		"working_branch": "",
		"target_branch":  "",
		"criteria_count": float64(0),
	})
}

// The warning is the only place a caller learns why an unbound session may
// never attach to its PR, so it has to name both the flag and the fallback.
func TestNoWorkingBranchWarning(t *testing.T) {
	for _, want := range []string{"--working-branch", "Runbook: <url>"} {
		if !strings.Contains(noWorkingBranchWarning, want) {
			t.Errorf("warning missing %q: %q", want, noWorkingBranchWarning)
		}
	}
}
