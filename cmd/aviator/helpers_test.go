package main

import (
	"os"
	"path/filepath"
	"testing"
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
