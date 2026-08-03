package adapter

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// Claude is the adapter for Claude Code. Its hooks live in .claude/settings.json
// (committed) or .claude/settings.local.json (gitignored), using the PreToolUse
// schema shared with Codex.
type Claude struct{}

func (Claude) ID() string          { return "claude" }
func (Claude) DisplayName() string { return "Claude Code" }

// claudeCommand is the callback the installed hook runs. Its ownerPrefix marks
// the entry as ours for updates and removal.
const claudeCommand = "aviator hooks run --agent=claude"

// claudeMatcher matches the Bash tool; the callback self-checks whether the
// specific command opens a PR.
const claudeMatcher = "Bash"

// Detect reports whether Claude Code is set up for the current user.
func (Claude) Detect() bool {
	dir := claudeUserDir()
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func claudeUserDir() string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func claudeSettingsPath(scope Scope, repoRoot string) string {
	if scope == ScopeRepoLocal {
		return filepath.Join(repoRoot, ".claude", "settings.local.json")
	}
	return filepath.Join(repoRoot, ".claude", "settings.json")
}

// SkillsDir returns where Claude Code loads skills for the given scope. Team
// scope commits them with the repo; self scope keeps them in the user's home so
// nothing new lands in the repo. It returns install=false when the aviator
// plugin already ships the guidance skills, to avoid a redundant second copy.
func (Claude) SkillsDir(scope Scope, repoRoot string) (string, bool) {
	if claudePluginProvidesSkills() {
		return "", false
	}
	if scope == ScopeRepoLocal {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		return filepath.Join(home, ".claude", "skills"), true
	}
	return filepath.Join(repoRoot, ".claude", "skills"), true
}

// claudePluginProvidesSkills best-effort reports whether the aviator Claude
// plugin already supplies the guidance skills, so init can skip installing a
// duplicate. It looks for the plugin's acceptance-criteria skill in the known
// marketplace/cache locations.
func claudePluginProvidesSkills() bool {
	dir := claudeUserDir()
	if dir == "" {
		return false
	}
	patterns := []string{
		filepath.Join(dir, "plugins", "marketplaces", "*", "aviator", "skills", "acceptance-criteria", "SKILL.md"),
		filepath.Join(dir, "plugins", "cache", "*", "aviator", "skills", "acceptance-criteria", "SKILL.md"),
	}
	for _, p := range patterns {
		if m, _ := filepath.Glob(p); len(m) > 0 {
			return true
		}
	}
	return false
}

func (Claude) Install(scope Scope, repoRoot string) (Change, error) {
	return installSettingsHook(claudeSettingsPath(scope, repoRoot), claudeMatcher, claudeCommand)
}

func (Claude) Uninstall(scope Scope, repoRoot string) (Change, error) {
	return uninstallSettingsHook(claudeSettingsPath(scope, repoRoot))
}

func (Claude) Status(scope Scope, repoRoot string) (Status, error) {
	return statusSettingsHook(claudeSettingsPath(scope, repoRoot), claudeMatcher, claudeCommand)
}

// EmitReminder reads Claude's PreToolUse payload and, when the command opens a
// PR, injects the Verify reminder as additional context without blocking.
func (Claude) EmitReminder(stdin io.Reader, stdout io.Writer) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	var in struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	// Fail open: unparseable or unrelated input means no reminder — the tool
	// proceeds normally.
	if err := json.Unmarshal(data, &in); err != nil {
		return nil
	}
	if in.ToolName != "Bash" || !isPRCommand(in.ToolInput.Command) {
		return nil
	}
	out := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "PreToolUse",
			"additionalContext": reminderText,
		},
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}
