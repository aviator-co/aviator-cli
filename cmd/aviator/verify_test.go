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

// Callers parse this object rather than scraping "Review r/123" out of the
// human output, so the keys are the contract.
func TestVerifySubmitJSON(t *testing.T) {
	got := decodeJSON(t, newVerifySubmitJSON(&api.SubmitVerifyResponse{
		RunbookNumber:      123,
		URL:                "https://app.aviator.co/r/123",
		WorkingBranch:      "feature",
		TargetBranch:       "main",
		AcceptanceCriteria: []string{"one", "two"},
		PreviewConfigName:  ptr("docs"),
	}))
	assertJSONFields(t, got, map[string]any{
		"id":                  "r/123",
		"url":                 "https://app.aviator.co/r/123",
		"working_branch":      "feature",
		"target_branch":       "main",
		"criteria_count":      float64(2),
		"preview_config_name": "docs",
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
		"id":                  "r/7",
		"url":                 "https://app.aviator.co/r/7",
		"working_branch":      "",
		"target_branch":       "",
		"criteria_count":      float64(0),
		"preview_config_name": nil,
	})
}

// Same contract as verifySubmitJSON: callers parse these keys.
func TestVerifyRunJSON(t *testing.T) {
	got := decodeJSON(t, newVerifyRunJSON(&api.TriggerVerifyRunResponse{
		RunbookNumber:     42,
		URL:               "https://app.aviator.co/r/42",
		RunID:             1234,
		RunStatus:         "pending",
		Deduplicated:      false,
		Message:           "Verification started.",
		PreviewConfigName: ptr("docs"),
	}))
	assertJSONFields(t, got, map[string]any{
		"id":                  "r/42",
		"url":                 "https://app.aviator.co/r/42",
		"run_id":              float64(1234),
		"run_status":          "pending",
		"deduplicated":        false,
		"message":             "Verification started.",
		"preview_config_name": "docs",
	})
}

func TestVerifyRunJSONKeepsEmptyFields(t *testing.T) {
	got := decodeJSON(t, newVerifyRunJSON(&api.TriggerVerifyRunResponse{
		RunbookNumber: 7,
		Deduplicated:  true,
	}))
	assertJSONFields(t, got, map[string]any{
		"id":                  "r/7",
		"url":                 "",
		"run_id":              float64(0),
		"run_status":          "",
		"deduplicated":        true,
		"message":             "",
		"preview_config_name": nil,
	})
}

// The warning is the only place a caller learns why an unbound session may
// never attach to its PR, so it has to name both the flag and the fallback.
func TestNoWorkingBranchWarning(t *testing.T) {
	for _, want := range []string{"--working-branch", "Review: <url>"} {
		if !strings.Contains(noWorkingBranchWarning, want) {
			t.Errorf("warning missing %q: %q", want, noWorkingBranchWarning)
		}
	}
}

func TestPreviewNotAppliedWarning(t *testing.T) {
	for _, tc := range []struct {
		name string
		sent string
		got  *string
		want string
	}{
		{"no flag", "", nil, ""},
		{"saved", "docs", ptr("docs"), ""},
		{"server ignored it", "docs", nil, "didn't confirm preview config \"docs\""},
		{"server saved another", "docs", ptr("default"), "the session uses \"default\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := previewNotAppliedWarning(tc.sent, tc.got)
			if tc.want == "" && msg != "" {
				t.Errorf("want no warning, got %q", msg)
			}
			if !strings.Contains(msg, tc.want) {
				t.Errorf("warning %q missing %q", msg, tc.want)
			}
		})
	}
}
