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
		RunbookNumber:     123,
		URL:               "https://app.aviator.co/runbook/123",
		RunbookVersion:    ptr(4),
		Intent:            ptr("make the thing doable"),
		PreviewConfigName: ptr("docs"),
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
		LatestVerification: &api.VerificationRun{
			ID:             900,
			Status:         "failed",
			CommitSHA:      ptr("abcdef1234567890"),
			CriteriaTotal:  2,
			CriteriaPassed: 1,
			CriteriaFailed: 1,
			FailedResults: []api.FailedResult{
				{Criterion: "does the thing", Reason: ptr("nope")},
			},
			PreviewConfigName: ptr("default"),
			PreviewMethod:     ptr("sandbox"),
		},
	}

	out := formatRunbookDetail(detail, nil, false)
	for _, want := range []string{
		"✓ r/123 (version 4)",
		"Intent: make the thing doable",
		"Branch: feature -> main",
		"Steps: 1/2 completed",
		"1. does the thing",
		"Latest verification: run 900, failed (1/2 passed, 1 failed, abcdef1)",
		"does the thing: nope",
		"  Preview config: docs",
		"Ran on preview config: default (sandbox)",
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
	out := formatRunbookDetail(detail, nil, false)
	if !strings.Contains(out, "Latest verification: none yet") {
		t.Errorf("expected 'none yet', got:\n%s", out)
	}
}

func TestFormatVerificationError(t *testing.T) {
	msg := "sandbox timed out"
	out := formatVerification("Latest verification", &api.VerificationRun{
		Status:       "error",
		ErrorMessage: &msg,
	})
	if !strings.Contains(out, "Error: sandbox timed out") {
		t.Errorf("expected error message in output, got:\n%s", out)
	}
}

func TestShowJSON(t *testing.T) {
	got := decodeJSON(t, newShowJSON(&api.RunbookDetail{
		RunbookNumber:     123,
		URL:               "https://app.aviator.co/r/123",
		RunbookVersion:    ptr(4),
		Intent:            ptr("gate the banner"),
		PreviewConfigName: ptr("docs"),
		RunbookState:      &api.RunbookState{WorkingBranch: ptr("feature"), TargetBranch: ptr("main")},
		PullRequests:      []api.LinkedPullRequest{{Number: 1201, URL: "https://github.com/acme/web/pull/1201"}},
		SpecFiles:         []api.DetailSpecFile{{Filename: "spec.md", Content: "..."}},
		AcceptanceCriteria: []api.DetailCriterion{
			{Ordinal: 1, RawText: "does the thing", StableKey: ptr("abc123")},
		},
	}, nil, false))
	for key, want := range map[string]any{
		"id":                  "r/123",
		"url":                 "https://app.aviator.co/r/123",
		"version":             float64(4),
		"runbook_version":     float64(4),
		"intent":              "gate the banner",
		"working_branch":      "feature",
		"target_branch":       "main",
		"latest_verification": nil,
		"runs_has_more":       false,
		"preview_config_name": "docs",
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	if specs := got["spec_files"].([]any); len(specs) != 1 || specs[0] != "spec.md" {
		t.Errorf("spec_files = %v", specs)
	}
	if c := got["criteria"].([]any); len(c) != 1 || c[0].(map[string]any)["stable_key"] != "abc123" {
		t.Errorf("criteria = %v", c)
	}
	if pr := got["pull_requests"].([]any); len(pr) != 1 || pr[0].(map[string]any)["number"] != float64(1201) {
		t.Errorf("pull_requests = %v", pr)
	}
}

func TestShowJSONRuns(t *testing.T) {
	got := decodeJSON(t, newShowJSON(&api.RunbookDetail{RunbookNumber: 123}, []api.VerificationRun{{
		ID:             900,
		Status:         "passed",
		CommitSHA:      ptr("abc123"),
		RunbookVersion: ptr(4),
		CriteriaTotal:  12,
		CriteriaPassed: 12,
		CreatedAt:      "2026-10-08T12:00:00Z",
		FailedResults:  []api.FailedResult{{Criterion: "ignored"}},
	}}, true))
	if got["runs_has_more"] != true {
		t.Errorf("runs_has_more = %v", got["runs_has_more"])
	}
	run := got["runs"].([]any)[0].(map[string]any)
	for key, want := range map[string]any{
		"run_id":     float64(900),
		"status":     "passed",
		"commit_sha": "abc123",
		"version":    float64(4),
		"total":      float64(12),
		"passed":     float64(12),
		"created_at": "2026-10-08T12:00:00Z",
	} {
		if run[key] != want {
			t.Errorf("%s = %v, want %v", key, run[key], want)
		}
	}
	if _, ok := run["failures"]; ok {
		t.Errorf("a listed run should not carry failures: %v", run)
	}
}

func TestShowJSONNoRuns(t *testing.T) {
	got := decodeJSON(t, newShowJSON(&api.RunbookDetail{RunbookNumber: 123}, nil, false))
	if runs, ok := got["runs"].([]any); !ok || len(runs) != 0 {
		t.Errorf("runs = %v, want []", got["runs"])
	}
}

func TestFormatRunbookDetailRuns(t *testing.T) {
	detail := &api.RunbookDetail{RunbookNumber: 123, URL: "https://app.aviator.co/r/123"}
	runs := []api.VerificationRun{
		{ID: 900, Status: "passed", CommitSHA: ptr("abcdef1234"), RunbookVersion: ptr(4), CriteriaTotal: 12, CriteriaPassed: 12},
		{ID: 887, Status: "error"},
	}
	out := formatRunbookDetail(detail, runs, true)
	for _, want := range []string{
		"  Runs:\n",
		"900  passed  abcdef1  12/12 passed  version 4",
		"887  error   -        0/0 passed    -",
		"More runs: https://app.aviator.co/r/123/verify/history",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(formatRunbookDetail(detail, nil, false), "Runs:") {
		t.Error("a review with no runs should not print a Runs section")
	}
}

func TestResultsJSON(t *testing.T) {
	got := decodeJSON(t, newResultsJSON(&api.RunbookDetail{
		RunbookNumber:  123,
		URL:            "https://app.aviator.co/r/123",
		RunbookVersion: ptr(4),
		LatestVerification: &api.VerificationRun{
			Status:         "failed",
			RunbookVersion: ptr(4),
			CriteriaTotal:  2,
			CriteriaPassed: 1,
			CriteriaFailed: 1,
			FailedResults: []api.FailedResult{
				{Criterion: "no secrets", Status: "fail", Reason: ptr("nope"), IsInvariant: true, BaselineInvariantID: ptr(42)},
			},
		},
	}))
	if got["id"] != "r/123" || got["version"] != float64(4) {
		t.Errorf("top level = %v", got)
	}
	if _, ok := got["run"]; ok {
		t.Errorf("run should only appear with --run: %v", got)
	}
	v := got["latest_verification"].(map[string]any)
	if v["status"] != "failed" || v["total"] != float64(2) || v["failed"] != float64(1) {
		t.Errorf("latest_verification = %v", v)
	}
	f := v["failures"].([]any)[0].(map[string]any)
	if f["criterion"] != "no secrets" || f["invariant"] != true || f["baseline_invariant_id"] != float64(42) || f["stable_key"] != nil {
		t.Errorf("failure = %v", f)
	}
}

func TestResultsJSONForRun(t *testing.T) {
	got := decodeJSON(t, newResultsRunJSON(
		&api.RunbookDetail{RunbookNumber: 123, LatestVerification: &api.VerificationRun{ID: 900, Status: "passed"}},
		&api.VerificationRun{ID: 887, Status: "failed", FailedResults: []api.FailedResult{{Criterion: "no secrets"}}},
	))
	if _, ok := got["latest_verification"]; ok {
		t.Errorf("latest_verification should be absent with --run: %v", got)
	}
	run := got["run"].(map[string]any)
	if run["run_id"] != float64(887) || run["status"] != "failed" {
		t.Errorf("run = %v", run)
	}
	if f := run["failures"].([]any); len(f) != 1 || f[0].(map[string]any)["criterion"] != "no secrets" {
		t.Errorf("failures = %v", f)
	}
}

func TestFormatRunbookDetailHandles(t *testing.T) {
	detail := &api.RunbookDetail{
		RunbookNumber: 5,
		URL:           "https://app.aviator.co/r/5",
		AcceptanceCriteria: []api.DetailCriterion{
			{Ordinal: 1, RawText: "does the thing", StableKey: ptr("abc123")},
		},
		LatestVerification: &api.VerificationRun{
			Status: "failed",
			FailedResults: []api.FailedResult{
				{Criterion: "no secrets", BaselineInvariantID: ptr(42), Reason: ptr("leaked")},
			},
		},
	}
	out := formatRunbookDetail(detail, nil, false)
	for _, want := range []string{
		"1. does the thing [key abc123]",
		"no secrets [invariant 42]: leaked",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}
