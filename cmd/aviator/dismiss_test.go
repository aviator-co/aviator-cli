package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
)

func TestBuildDismissals(t *testing.T) {
	got, err := buildDismissals([]string{"def"},
		`[{"stable_key":"abc"},{"baseline_invariant_id":42,"category":"accepted_risk","justification":"known"}]`, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []api.Dismissal{
		{StableKey: "abc"},
		{BaselineInvariantID: 42, Category: "accepted_risk", Justification: "known"},
		{StableKey: "def"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dismissals = %+v, want %+v", got, want)
	}
}

func TestBuildDismissalsFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dismiss.json")
	if err := os.WriteFile(path, []byte(`[{"stable_key":"abc"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := buildDismissals(nil, "", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []api.Dismissal{{StableKey: "abc"}}) {
		t.Errorf("dismissals = %+v", got)
	}
}

func TestBuildDismissalsRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		json string
		want string
	}{
		"nothing":       {want: "at least one"},
		"empty array":   {json: "[]", want: "at least one"},
		"unknown field": {json: `[{"invariant_id":42}]`, want: "invalid --criteria-json"},
		"not an array":  {json: `{"stable_key":"abc"}`, want: "invalid --criteria-json"},
	} {
		_, err := buildDismissals(nil, tc.json, "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestDismissJSON(t *testing.T) {
	version := 6
	got := decodeJSON(t, newDismissJSON(&api.DismissCriteriaResponse{
		RunbookNumber: 123, NewVersion: &version, Deleted: 2, Waived: 1, CriteriaCount: 3,
	}))
	assertJSONFields(t, got, map[string]any{
		"id":              "r/123",
		"version":         float64(6),
		"deleted":         float64(2),
		"already_deleted": float64(0),
		"waived":          float64(1),
		"criteria_count":  float64(3),
	})
}
