package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/config"
)

func TestParseRepo(t *testing.T) {
	repo, err := parseRepo("acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.Org != "acme" || repo.Name != "web" {
		t.Fatalf("got %+v", repo)
	}

	for _, bad := range []string{"", "noslash", "/web", "acme/", "  "} {
		if _, err := parseRepo(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestReadSpecFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(path, []byte("# spec body"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec, err := readSpecFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Filename != "spec.md" {
		t.Errorf("filename = %q, want spec.md", spec.Filename)
	}
	if spec.Content != "# spec body" {
		t.Errorf("content = %q", spec.Content)
	}

	if _, err := readSpecFile(filepath.Join(dir, "missing.md")); err == nil {
		t.Error("expected error for missing spec file")
	}
}

func TestCollectCriteria(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "criteria.txt")
	contents := "# comment\nfirst\n\n  second  \n"
	if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := collectCriteria([]string{" inline ", ""}, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"inline", "first", "second"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// TestStaticTokenDescription covers the warning that names the credential which
// would shadow a fresh sign-in, which has to tell the two sources apart.
func TestStaticTokenDescription(t *testing.T) {
	previous := config.Av.Aviator
	t.Cleanup(func() { config.Av.Aviator = previous })

	tests := []struct {
		name    string
		value   string
		fromEnv bool
		want    string
	}{
		{"no static token", "", false, ""},
		{"config file", "abc", false, "the aviator.apiToken setting in your config file"},
		{"environment", "abc", true, "the AVIATOR_API_TOKEN environment variable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.Av.Aviator.APIToken = tt.value
			config.Av.Aviator.APITokenFromEnv = tt.fromEnv
			if got := staticTokenDescription(); got != tt.want {
				t.Fatalf("staticTokenDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestParseID(t *testing.T) {
	good := map[string]int{
		"7":     7,
		"#7":    7,
		" #42 ": 42, //nolint:gocritic // the padding is what this case asserts gets trimmed
	}
	for in, want := range good {
		got, err := parseID("invariant", in)
		if err != nil {
			t.Errorf("parseID(%q) error: %v", in, err)
		} else if got != want {
			t.Errorf("parseID(%q) = %d, want %d", in, got, want)
		}
	}
	for _, in := range []string{"", "#", "abc", "0", "-3", "r/7"} {
		if _, err := parseID("invariant", in); err == nil {
			t.Errorf("parseID(%q) expected error", in)
		}
	}
	// Each command names its own kind of id.
	for noun, want := range map[string]string{
		"decision":  `invalid decision ID "0", expected a positive number`,
		"invariant": `invalid invariant ID "0", expected a positive number`,
		"artifact":  `invalid artifact ID "0", expected a positive number`,
	} {
		if _, err := parseID(noun, "0"); err == nil || err.Error() != want {
			t.Errorf("parseID(%q, \"0\") = %v, want %q", noun, err, want)
		}
	}
}
