package main

import (
	"strings"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
)

func TestParseRunbookID(t *testing.T) {
	good := map[string]int{
		"123":                           123,
		"r/123":                         123,
		" r/45 ":                        45, //nolint:gocritic // the padding is what this case asserts gets trimmed
		"https://app.aviator.co/r/123":  123,
		"https://app.aviator.co/r/123/": 123,
		"https://aviator.co/org/r/9":    9,
	}
	for in, want := range good {
		got, err := parseRunbookID(in)
		if err != nil {
			t.Errorf("parseRunbookID(%q) error: %v", in, err)
		} else if got != want {
			t.Errorf("parseRunbookID(%q) = %d, want %d", in, got, want)
		}
	}
	for _, in := range []string{"", "abc", "r/", "r/abc", "-4", "r/-4", "https://app.aviator.co/x/123"} {
		if _, err := parseRunbookID(in); err == nil {
			t.Errorf("parseRunbookID(%q) expected error", in)
		}
	}
}

func TestFormatRunbookID(t *testing.T) {
	if got := formatRunbookID(123); got != "r/123" {
		t.Errorf("formatRunbookID(123) = %q", got)
	}
}

func TestFormatRunbookDetail(t *testing.T) {
	detail := &api.RunbookDetail{
		RunbookNumber:  123,
		URL:            "https://app.aviator.co/runbook/123",
		RunbookVersion: ptr(4),
		Intent:         ptr("make the thing doable"),
		RunbookState: &api.RunbookState{
			WorkingBranch: ptr("feature"),
			TargetBranch:  ptr("main"),
			Steps: []api.RunbookStep{
				{StepNumber: "1", Title: "one", Status: "completed"},
				{StepNumber: "1.1", Title: "two", Status: "in_progress"},
			},
		},
		AcceptanceCriteria: []api.DetailCriterion{
			{Ordinal: 1, RawText: "does the thing"},
		},
		LatestVerification: &api.LatestVerification{
			Status:         "failed",
			CommitSHA:      ptr("abcdef1234567890"),
			CriteriaTotal:  2,
			CriteriaPassed: 1,
			CriteriaFailed: 1,
			FailedResults: []api.FailedResult{
				{Criterion: "does the thing", Reason: ptr("nope")},
			},
		},
	}

	out := formatRunbookDetail(detail)
	for _, want := range []string{
		"✓ r/123 (version 4)",
		"Intent: make the thing doable",
		"Branch: feature -> main",
		"Steps: 1/2 completed",
		"1. does the thing",
		"Latest verification: failed (1/2 passed, 1 failed, abcdef1)",
		"does the thing: nope",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatRunbookDetailNoVerificationYet(t *testing.T) {
	detail := &api.RunbookDetail{
		RunbookNumber: 7,
		URL:           "https://app.aviator.co/runbook/7",
		AcceptanceCriteria: []api.DetailCriterion{
			{Ordinal: 1, RawText: "criterion"},
		},
	}
	out := formatRunbookDetail(detail)
	if !strings.Contains(out, "Latest verification: none yet") {
		t.Errorf("expected 'none yet', got:\n%s", out)
	}
}

func TestFormatVerificationError(t *testing.T) {
	msg := "sandbox timed out"
	out := formatVerification(&api.LatestVerification{
		Status:       "error",
		ErrorMessage: &msg,
	})
	if !strings.Contains(out, "Error: sandbox timed out") {
		t.Errorf("expected error message in output, got:\n%s", out)
	}
}

func TestShowJSON(t *testing.T) {
	got := decodeJSON(t, newShowJSON(&api.RunbookDetail{
		RunbookNumber:  123,
		URL:            "https://app.aviator.co/r/123",
		RunbookVersion: ptr(4),
		Intent:         ptr("gate the banner"),
		RunbookState:   &api.RunbookState{WorkingBranch: ptr("feature"), TargetBranch: ptr("main")},
		PullRequests:   []api.LinkedPullRequest{{Number: 1201, URL: "https://github.com/acme/web/pull/1201"}},
		SpecFiles:      []api.DetailSpecFile{{Filename: "spec.md", Content: "..."}},
		AcceptanceCriteria: []api.DetailCriterion{
			{Ordinal: 1, RawText: "does the thing"},
		},
	}))
	for key, want := range map[string]any{
		"id":                  "r/123",
		"url":                 "https://app.aviator.co/r/123",
		"version":             float64(4),
		"runbook_version":     float64(4),
		"intent":              "gate the banner",
		"working_branch":      "feature",
		"target_branch":       "main",
		"latest_verification": nil,
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	if specs := got["spec_files"].([]any); len(specs) != 1 || specs[0] != "spec.md" {
		t.Errorf("spec_files = %v", specs)
	}
	if c := got["criteria"].([]any); len(c) != 1 || c[0].(map[string]any)["text"] != "does the thing" {
		t.Errorf("criteria = %v", c)
	}
	if pr := got["pull_requests"].([]any); len(pr) != 1 || pr[0].(map[string]any)["number"] != float64(1201) {
		t.Errorf("pull_requests = %v", pr)
	}
}

func TestResultsJSON(t *testing.T) {
	got := decodeJSON(t, newResultsJSON(&api.RunbookDetail{
		RunbookNumber:  123,
		URL:            "https://app.aviator.co/r/123",
		RunbookVersion: ptr(4),
		LatestVerification: &api.LatestVerification{
			Status:         "failed",
			RunbookVersion: ptr(4),
			CriteriaTotal:  2,
			CriteriaPassed: 1,
			CriteriaFailed: 1,
			FailedResults:  []api.FailedResult{{Criterion: "no secrets", Status: "fail", Reason: ptr("nope"), IsInvariant: true}},
		},
	}))
	if got["id"] != "r/123" || got["version"] != float64(4) {
		t.Errorf("top level = %v", got)
	}
	v := got["latest_verification"].(map[string]any)
	if v["status"] != "failed" || v["total"] != float64(2) || v["failed"] != float64(1) {
		t.Errorf("latest_verification = %v", v)
	}
	f := v["failures"].([]any)[0].(map[string]any)
	if f["criterion"] != "no secrets" || f["reason"] != "nope" || f["invariant"] != true || f["evidence"] != nil {
		t.Errorf("failure = %v", f)
	}
}
