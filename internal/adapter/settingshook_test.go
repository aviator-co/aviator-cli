package adapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func groupsFor(t *testing.T, path, event string) []any {
	t.Helper()
	hooks, ok := readJSON(t, path)["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no hooks object", path)
	}
	groups, ok := hooks[event].([]any)
	if !ok {
		t.Fatalf("%s has no %s groups", path, event)
	}
	return groups
}

func hooksOf(group any) []any {
	return group.(map[string]any)["hooks"].([]any)
}

func commandOf(hook any) string {
	c, _ := hook.(map[string]any)["command"].(string)
	return c
}

func TestInstallWritesEveryEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	change, err := installSettingsHook(path, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if change != ChangeAdded {
		t.Fatalf("change = %v, want ChangeAdded", change)
	}

	for _, ev := range hookEvents {
		groups := groupsFor(t, path, ev.name)
		if len(groups) != 1 {
			t.Fatalf("%s: got %d groups, want 1", ev.name, len(groups))
		}
		if m, _ := groups[0].(map[string]any)["matcher"].(string); m != ev.matcher {
			t.Errorf("%s: matcher = %q, want %q", ev.name, m, ev.matcher)
		}
		if cmd := commandOf(hooksOf(groups[0])[0]); cmd != callbackCommand("claude", ev.subcommand) {
			t.Errorf("%s: command = %q", ev.name, cmd)
		}
	}
}

// The command lands in the file verbatim: Codex and Gemini re-prompt for trust
// when it changes, so escaping the shell operators would be a silent break.
func TestInstallWritesCommandUnescaped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := installSettingsHook(path, "claude"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), callbackCommand("claude", "pre-tool-use")) {
		t.Errorf("command not written verbatim:\n%s", data)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := installSettingsHook(path, "claude"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	change, err := installSettingsHook(path, "claude")
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
	if _, err := installSettingsHook(path, "claude"); err != nil {
		t.Fatal(err)
	}

	got := readJSON(t, path)
	if got["model"] != "opus" {
		t.Errorf("lost top-level key: model = %v", got["model"])
	}
	if _, ok := got["permissions"]; !ok {
		t.Error("lost permissions key")
	}
	if _, ok := got["hooks"].(map[string]any)["Stop"]; !ok {
		t.Error("lost Stop event")
	}
	groups := groupsFor(t, path, "PreToolUse")
	if len(groups) != 2 {
		t.Fatalf("PreToolUse has %d groups, want 2 (user's mylint + ours)", len(groups))
	}
	if commandOf(hooksOf(groups[0])[0]) != "mylint" {
		t.Error("clobbered the user's existing PreToolUse hook")
	}
}

// We own one hook, not the group around it. A user consolidating their hooks
// under one matcher must not lose them to install or uninstall.
func TestSharedGroupKeepsTheUsersHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	shared := `{"hooks": {"PreToolUse": [{"matcher": "` + toolMatcher + `", "hooks": [
	  {"type": "command", "command": "my-audit-log", "timeout": 30},
	  {"type": "command", "command": "aviator hooks pre-tool-use --agent=claude"}
	]}]}}`
	if err := os.WriteFile(path, []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := installSettingsHook(path, "claude"); err != nil {
		t.Fatal(err)
	}
	hooks := hooksOf(groupsFor(t, path, "PreToolUse")[0])
	if len(hooks) != 2 {
		t.Fatalf("after install the group has %d hooks, want 2", len(hooks))
	}
	user := hooks[0].(map[string]any)
	if user["command"] != "my-audit-log" {
		t.Errorf("install clobbered the user's hook: %v", user["command"])
	}
	if user["timeout"] == nil {
		t.Error("install dropped a field it doesn't model on the user's hook")
	}

	if _, err := uninstallSettingsHook(path, "claude"); err != nil {
		t.Fatal(err)
	}
	hooks = hooksOf(groupsFor(t, path, "PreToolUse")[0])
	if len(hooks) != 1 || commandOf(hooks[0]) != "my-audit-log" {
		t.Errorf("uninstall removed the wrong hook: %v", hooks)
	}
}

// A hook under the wrong matcher can never fire, so install must move it rather
// than report it as current.
func TestInstallCorrectsTheMatcher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	wrong := `{"hooks": {"PreToolUse": [{"matcher": "Edit", "hooks": [{"type": "command",
	  "command": "command -v aviator >/dev/null 2>&1 && aviator hooks pre-tool-use --agent=claude || true"}]}]}}`
	if err := os.WriteFile(path, []byte(wrong), 0o600); err != nil {
		t.Fatal(err)
	}

	change, err := installSettingsHook(path, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if change != ChangeUpdated {
		t.Fatalf("change = %v, want ChangeUpdated", change)
	}
	groups := groupsFor(t, path, "PreToolUse")
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1 (the Edit group emptied and went)", len(groups))
	}
	if m := groups[0].(map[string]any)["matcher"]; m != toolMatcher {
		t.Errorf("matcher = %v, want %q", m, toolMatcher)
	}
}

// --agent=claude is a prefix of --agent=claude-custom.
func TestOwnershipStopsAtTheAgentID(t *testing.T) {
	if ownsCommand("aviator hooks pre-tool-use --agent=claude-custom", "claude") {
		t.Error("claude claimed claude-custom's hook")
	}
	if !ownsCommand("aviator hooks pre-tool-use --agent=claude", "claude") {
		t.Error("did not recognise our own bare command")
	}
	if !ownsCommand(callbackCommand("claude", "pre-tool-use"), "claude") {
		t.Error("did not recognise our own current command")
	}
}

func TestUninstallRemovesAFileItEmptied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := installSettingsHook(path, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstallSettingsHook(path, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		data, _ := os.ReadFile(path)
		t.Errorf("settings file still present after uninstall: %s", data)
	}
}

func TestUninstallKeepsAFileWithOtherContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"model": "opus"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := installSettingsHook(path, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstallSettingsHook(path, "claude"); err != nil {
		t.Fatal(err)
	}
	if readJSON(t, path)["model"] != "opus" {
		t.Error("uninstall removed a file that still had the user's settings")
	}
}

func TestUninstallMissingIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	change, err := uninstallSettingsHook(path, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if change != ChangeNone {
		t.Fatalf("change = %v, want ChangeNone", change)
	}
}

// JSON null unmarshals into a nil map, which we would then assign into.
func TestNullSettingsErrorRatherThanPanic(t *testing.T) {
	for _, content := range []string{`null`, `{"hooks": null}`, `[]`, `"a string"`} {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := installSettingsHook(path, "claude"); err == nil {
			t.Errorf("install on %s returned no error, want one", content)
		}
	}
}
