package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/spf13/pflag"
)

func TestParseInvariantID(t *testing.T) {
	good := map[string]int{
		"7":     7,
		"#7":    7,
		" #42 ": 42, //nolint:gocritic // the padding is what this case asserts gets trimmed
	}
	for in, want := range good {
		got, err := parseInvariantID(in)
		if err != nil {
			t.Errorf("parseInvariantID(%q) error: %v", in, err)
		} else if got != want {
			t.Errorf("parseInvariantID(%q) = %d, want %d", in, got, want)
		}
	}
	for _, in := range []string{"", "#", "abc", "0", "-3", "r/7"} {
		if _, err := parseInvariantID(in); err == nil {
			t.Errorf("parseInvariantID(%q) expected error", in)
		}
	}
}

func TestParseCondition(t *testing.T) {
	good := map[string]api.InvariantCondition{
		"file_path_glob=src/**/*.py": {Type: "file_path_glob", Value: "src/**/*.py"},
		"language!=python":           {Type: "language", Value: "python", Negate: true},
		" language = go ":            {Type: "language", Value: "go"}, //nolint:gocritic // the padding is what this case asserts gets trimmed
		"file_path_glob=a=b":         {Type: "file_path_glob", Value: "a=b"},
	}
	for in, want := range good {
		got, err := parseCondition(in)
		if err != nil {
			t.Errorf("parseCondition(%q) error: %v", in, err)
		} else if got != want {
			t.Errorf("parseCondition(%q) = %+v, want %+v", in, got, want)
		}
	}
	for _, in := range []string{"", "language", "=python", "language=", "!=python", "language!="} {
		if _, err := parseCondition(in); err == nil {
			t.Errorf("parseCondition(%q) expected error", in)
		}
	}
}

// buildEditRequest parses args the way `edit` would and builds the PATCH body.
func buildEditRequest(t *testing.T, args ...string) (api.UpdateInvariantRequest, error) {
	t.Helper()
	var f invariantEditFlags
	fs := pflag.NewFlagSet("edit", pflag.ContinueOnError)
	registerEditFlags(fs, &f)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return buildUpdateRequest(fs, &f)
}

func TestBuildUpdateRequestLeavesUnpassedFieldsAlone(t *testing.T) {
	req, err := buildEditRequest(t, "--title", "New")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Title == nil || *req.Title != "New" {
		t.Errorf("title = %v", req.Title)
	}
	if req.Body != nil || req.Category != nil || req.Repositories != nil ||
		req.Conditions != nil || req.Enabled != nil {
		t.Errorf("unpassed fields should be nil: %+v", req)
	}
}

func TestBuildUpdateRequestReplacesAndClearsSets(t *testing.T) {
	req, err := buildEditRequest(t,
		"--repo", "acme/web", "--repo", "acme/api",
		"--condition", "language!=go", "--disable")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Repositories == nil || len(*req.Repositories) != 2 || (*req.Repositories)[1].Name != "api" {
		t.Errorf("repositories = %v", req.Repositories)
	}
	if req.Conditions == nil || len(*req.Conditions) != 1 || !(*req.Conditions)[0].Negate {
		t.Errorf("conditions = %v", req.Conditions)
	}
	if req.Enabled == nil || *req.Enabled {
		t.Errorf("enabled = %v, want false", req.Enabled)
	}

	req, err = buildEditRequest(t, "--account-scoped", "--no-conditions", "--enable")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Repositories == nil || len(*req.Repositories) != 0 {
		t.Errorf("--account-scoped should send an empty list, got %v", req.Repositories)
	}
	if req.Conditions == nil || len(*req.Conditions) != 0 {
		t.Errorf("--no-conditions should send an empty list, got %v", req.Conditions)
	}
	if req.Enabled == nil || !*req.Enabled {
		t.Errorf("enabled = %v, want true", req.Enabled)
	}
}

// TestBuildUpdateRequestExplicitFalseBools covers --flag=false: an explicit
// false toggle is not a clear, and --enable=false means disable.
func TestBuildUpdateRequestExplicitFalseBools(t *testing.T) {
	req, err := buildEditRequest(t, "--title", "X", "--account-scoped=false", "--no-conditions=false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Repositories != nil || req.Conditions != nil {
		t.Errorf("=false toggles should not clear: %+v", req)
	}
	req, err = buildEditRequest(t, "--enable=false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Enabled == nil || *req.Enabled {
		t.Errorf("--enable=false should disable, got %v", req.Enabled)
	}
	req, err = buildEditRequest(t, "--disable=false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Enabled == nil || !*req.Enabled {
		t.Errorf("--disable=false should enable, got %v", req.Enabled)
	}
}

func TestBuildUpdateRequestBodyFileAndErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("From a file.\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	req, err := buildEditRequest(t, "--body-file", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Body == nil || *req.Body != "From a file." {
		t.Errorf("body = %v", req.Body)
	}

	if _, err := buildEditRequest(t); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Errorf("no flags: err = %v", err)
	}
	if _, err := buildEditRequest(t, "--repo", "bad"); err == nil {
		t.Error("bad repo: expected error")
	}
	if _, err := buildEditRequest(t, "--condition", "bad"); err == nil {
		t.Error("bad condition: expected error")
	}
}

func TestFormatInvariantList(t *testing.T) {
	resp := &api.ListInvariantsResponse{
		Page:    1,
		HasMore: true,
		Invariants: []api.Invariant{
			{
				ID: 1, Title: "Account rule", Category: "security", Status: "active", Enabled: true,
				Source: "manual",
			},
			{
				ID: 2, Title: "Repo rule", Category: "test_coverage", Status: "pending",
				Source: "ai_generated_docs", Reason: "CONTRIBUTING.md asks for tests on every module.",
				Repositories: []api.Repository{{Org: "acme", Name: "web"}, {Org: "acme", Name: "api"}},
				Conditions: []api.InvariantCondition{
					{Type: "file_path_glob", Value: "src/**"},
					{Type: "language", Value: "markdown", Negate: true},
				},
			},
			{
				ID: 3, Title: "Paused rule", Category: "code_style", Status: "active", Enabled: false,
				Source: "manual",
			},
		},
	}
	out := formatInvariantList(resp)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("want header, 3 rows, 1 reason row, blank, footer; got %d lines:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "ID  STATUS") || !strings.HasSuffix(lines[0], "TITLE") {
		t.Errorf("header = %q", lines[0])
	}
	// Every column starts at the same offset on every row: the title column is
	// where the header says it is, on the header, both rules, and the reason.
	titleCol := strings.Index(lines[0], "TITLE")
	for i, want := range map[int]string{1: "Account rule", 2: "Repo rule", 3: "reason: CONTRIBUTING.md", 4: "Paused rule"} {
		if got := strings.Index(lines[i], want); got != titleCol {
			t.Errorf("line %d: %q starts at column %d, want %d\n%s", i, want, got, titleCol, out)
		}
	}
	for _, want := range []string{
		"#2  pending", "ai_generated_docs", "2 repos +conditions",
		"#3  active (disabled)  code_style",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Count(out, "reason:") != 1 {
		t.Errorf("only the pending rule has a reason, got:\n%s", out)
	}
	if lines[6] != "See more invariants --page 2" {
		t.Errorf("footer = %q", lines[6])
	}
	// The full scope stays out of the table; it is in the detail and --json.
	if strings.Contains(out, "acme/web, acme/api") || strings.Contains(out, "file_path_glob") {
		t.Errorf("table should carry the short scope only:\n%s", out)
	}
}

func TestFormatInvariantScopeShort(t *testing.T) {
	one := []api.Repository{{Org: "acme", Name: "web"}}
	cond := []api.InvariantCondition{{Type: "language", Value: "go"}}
	for want, inv := range map[string]api.Invariant{
		"all repos":             {},
		"all repos +conditions": {Conditions: cond},
		"acme/web":              {Repositories: one},
		"acme/web +conditions":  {Repositories: one, Conditions: cond},
		"3 repos":               {Repositories: []api.Repository{{}, {}, {}}},
	} {
		if got := formatInvariantScopeShort(&inv); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestFormatInvariantListEmptyAndLastPage(t *testing.T) {
	if out := formatInvariantList(&api.ListInvariantsResponse{Page: 1}); out != "No invariants match.\n" {
		t.Errorf("empty first page = %q", out)
	}
	if out := formatInvariantList(&api.ListInvariantsResponse{Page: 4}); !strings.Contains(out, "Nothing on page 4") {
		t.Errorf("empty later page = %q", out)
	}
	out := formatInvariantList(&api.ListInvariantsResponse{Page: 3, Invariants: []api.Invariant{{ID: 9}}})
	if strings.Contains(out, "See more") {
		t.Errorf("last page should have no footer, got %q", out)
	}
}

func TestFormatInvariantCategories(t *testing.T) {
	out := formatInvariantCategories(&api.ListInvariantCategoriesResponse{
		Categories: []api.InvariantCategory{
			{Slug: "security", Name: "Security", Description: "Keeps secrets secret."},
			{Slug: "test_coverage", Name: "Test coverage", Description: "Has tests."},
		},
	})
	for _, want := range []string{
		"2 categories",
		"SLUG           NAME           DESCRIPTION",
		"security       Security       Keeps secrets secret.",
		"test_coverage  Test coverage  Has tests.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	if out := formatInvariantCategories(&api.ListInvariantCategoriesResponse{}); !strings.Contains(out, "no categories yet") {
		t.Errorf("output = %q", out)
	}
}

func TestFormatInvariantDetail(t *testing.T) {
	out := formatInvariantDetail(&api.Invariant{
		ID: 4, Title: "T", Body: "Do the thing.", Category: "security", Status: "rejected",
		Source: "ai_generated_pr_comments", Reason: "Reviewers keep asking for it.",
		SourceRefs:   []string{"https://github.com/acme/web/pull/1", "https://github.com/acme/web/pull/2"},
		Repositories: []api.Repository{{Org: "acme", Name: "web"}},
	})
	for _, want := range []string{
		"Title: T", "Category: security", "Status: rejected (enabled: false)",
		"Source: ai_generated_pr_comments", "Scope: acme/web", "Body: Do the thing.",
		"Reason: Reviewers keep asking for it.",
		"Source refs: https://github.com/acme/web/pull/1, https://github.com/acme/web/pull/2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	manual := formatInvariantDetail(&api.Invariant{ID: 5, Title: "M", Source: "manual"})
	if strings.Contains(manual, "Reason:") || strings.Contains(manual, "Source refs:") {
		t.Errorf("manual rule should omit empty reason/refs:\n%s", manual)
	}
}

// TestFormatInvariantDetailMultilineBody covers a body from --body-file: its
// continuation lines sit under the first line, not at column zero.
func TestFormatInvariantDetailMultilineBody(t *testing.T) {
	out := formatInvariantDetail(&api.Invariant{
		Title: "T", Body: "First line.\nSecond line.\n\nFourth line.\n",
	})
	want := "  Body: First line.\n        Second line.\n        \n        Fourth line.\n"
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q\n---\n%s", want, out)
	}
}
