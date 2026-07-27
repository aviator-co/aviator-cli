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
		" r/45 ":                        45,
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

func TestCleanDetailFields(t *testing.T) {
	got := cleanDetailFields([]string{"steps_markdown", " spec_files ", ""})
	want := []string{"steps_markdown", "spec_files"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFormatRunbookDetail(t *testing.T) {
	ptrInt := func(i int) *int { return &i }
	ptrStr := func(s string) *string { return &s }

	detail := &api.RunbookDetail{
		RunbookNumber:  123,
		URL:            "https://app.aviator.co/runbook/123",
		RunbookVersion: ptrInt(4),
		Intent:         ptrStr("make the thing doable"),
		RunbookState: &api.RunbookState{
			WorkingBranch: ptrStr("feature"),
			TargetBranch:  ptrStr("main"),
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
			CommitSHA:      ptrStr("abcdef1234567890"),
			CriteriaTotal:  2,
			CriteriaPassed: 1,
			CriteriaFailed: 1,
			FailedResults: []api.FailedResult{
				{Criterion: "does the thing", Reason: ptrStr("nope")},
			},
		},
	}

	out := formatRunbookDetail(detail, false)
	for _, want := range []string{
		"Runbook r/123 (version 4)",
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

func TestFormatRunbookDetailStepsMarkdown(t *testing.T) {
	md := "## Step 1\ndo the thing"
	detail := &api.RunbookDetail{
		RunbookNumber: 9,
		URL:           "https://app.aviator.co/runbook/9",
		StepsMarkdown: &md,
	}
	if out := formatRunbookDetail(detail, true); !strings.Contains(out, "## Step 1") {
		t.Errorf("expected steps markdown in output, got:\n%s", out)
	}
	if out := formatRunbookDetail(detail, false); strings.Contains(out, "## Step 1") {
		t.Errorf("expected steps markdown omitted, got:\n%s", out)
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
	out := formatRunbookDetail(detail, false)
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
