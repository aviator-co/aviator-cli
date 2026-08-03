package adapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCommand = "aviator hooks run --agent=claude"

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return v
}

func TestInstallAddsHookToNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	change, err := installSettingsHook(path, "Bash", testCommand)
	if err != nil {
		t.Fatal(err)
	}
	if change != ChangeAdded {
		t.Fatalf("change = %v, want ChangeAdded", change)
	}

	st, err := statusSettingsHook(path, "Bash", testCommand)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed || st.Stale {
		t.Fatalf("status = %+v, want installed & current", st)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := installSettingsHook(path, "Bash", testCommand); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	change, err := installSettingsHook(path, "Bash", testCommand)
	if err != nil {
		t.Fatal(err)
	}
	if change != ChangeNone {
		t.Fatalf("second install change = %v, want ChangeNone", change)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("idempotent install rewrote the file")
	}
}

func TestInstallPreservesUnknownKeysAndOtherHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	existing := `{
  "model": "opus",
  "permissions": {"allow": ["Read"]},
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "echo done"}]}],
    "PreToolUse": [{"matcher": "Edit", "hooks": [{"type": "command", "command": "mylint"}]}]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := installSettingsHook(path, "Bash", testCommand); err != nil {
		t.Fatal(err)
	}

	got := readJSON(t, path)
	if got["model"] != "opus" {
		t.Errorf("lost top-level key: model = %v", got["model"])
	}
	if _, ok := got["permissions"]; !ok {
		t.Error("lost permissions key")
	}
	hooks := got["hooks"].(map[string]any)
	if _, ok := hooks["Stop"]; !ok {
		t.Error("lost Stop event")
	}
	groups := hooks["PreToolUse"].([]any)
	if len(groups) != 2 {
		t.Fatalf("PreToolUse has %d groups, want 2 (user's mylint + ours)", len(groups))
	}
	// The user's Edit/mylint group must survive untouched.
	var foundMyLint bool
	for _, g := range groups {
		b, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "mylint") {
			foundMyLint = true
		}
	}
	if !foundMyLint {
		t.Error("clobbered the user's existing PreToolUse hook")
	}
}

func TestUpdateReplacesStaleEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	// Install an older-shaped entry owned by us (different matcher).
	if _, err := installSettingsHook(path, "OldMatcher", testCommand); err != nil {
		t.Fatal(err)
	}
	st, _ := statusSettingsHook(path, "Bash", testCommand)
	if !st.Stale {
		t.Fatal("expected the OldMatcher entry to read as stale vs the current matcher")
	}

	change, err := installSettingsHook(path, "Bash", testCommand)
	if err != nil {
		t.Fatal(err)
	}
	if change != ChangeUpdated {
		t.Fatalf("change = %v, want ChangeUpdated", change)
	}
	st, _ = statusSettingsHook(path, "Bash", testCommand)
	if st.Stale {
		t.Fatal("entry still stale after update")
	}
}

func TestUninstallRemovesOnlyOurs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	existing := `{"hooks": {"PreToolUse": [{"matcher": "Edit", "hooks": [{"type": "command", "command": "mylint"}]}]}}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := installSettingsHook(path, "Bash", testCommand); err != nil {
		t.Fatal(err)
	}

	change, err := uninstallSettingsHook(path)
	if err != nil {
		t.Fatal(err)
	}
	if change != ChangeRemoved {
		t.Fatalf("change = %v, want ChangeRemoved", change)
	}

	got := readJSON(t, path)
	groups := got["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(groups) != 1 {
		t.Fatalf("PreToolUse has %d groups after uninstall, want 1 (the user's)", len(groups))
	}
	b, err := json.Marshal(groups[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "mylint") {
		t.Error("uninstall removed the wrong entry")
	}
}

func TestUninstallMissingIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	change, err := uninstallSettingsHook(path)
	if err != nil {
		t.Fatal(err)
	}
	if change != ChangeNone {
		t.Fatalf("change = %v, want ChangeNone", change)
	}
}
