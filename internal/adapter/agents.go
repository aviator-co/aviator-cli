package adapter

import (
	"io"
	"os"
	"path/filepath"
)

// settingsAgent is an agent using Claude Code's hook schema, which Codex shares.
// Agents whose hooks differ in event name or response shape (Cursor,
// Antigravity) implement Adapter themselves instead.
type settingsAgent struct {
	id, name string
	// note is anything the user must do before the hook fires.
	note string
	// userEnv overrides userDir, which is relative to the user's home.
	userEnv, userDir string
	// repoFile is relative to the repo root, userFile to the agent's user dir.
	repoFile, userFile string
	// install tells the agent how to get /verify-submit, which differs per agent.
	install string
}

var registry = []Adapter{
	settingsAgent{
		id: "claude", name: "Claude Code",
		userEnv: "CLAUDE_CONFIG_DIR", userDir: ".claude",
		repoFile: ".claude/settings.json",
		userFile: "settings.json",
		install: "run `/plugin marketplace add aviator-co/agent-plugins` then " +
			"`/plugin install aviator@aviator-plugins`.",
	},
	settingsAgent{
		id: "codex", name: "Codex",
		userEnv: "CODEX_HOME", userDir: ".codex",
		repoFile: ".codex/hooks.json",
		userFile: "hooks.json",
		note:     "run /hooks in Codex and trust it — Codex won't fire an untrusted hook",
		install: "install the verify-submit skill from " +
			"https://github.com/aviator-co/agent-plugins into your Codex skills directory.",
	},
}

func (a settingsAgent) ID() string          { return a.id }
func (a settingsAgent) DisplayName() string { return a.name }
func (a settingsAgent) SetupNote() string   { return a.note }

func (a settingsAgent) Detect() bool {
	dir := a.configDir()
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func (a settingsAgent) configDir() string {
	if v := os.Getenv(a.userEnv); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, a.userDir)
}

// HookFile is the repo's config for team scope and the user's own for self,
// which is why self applies to every repository on the machine.
func (a settingsAgent) HookFile(scope Scope, repoRoot string) string {
	if scope == ScopeSelf {
		dir := a.configDir()
		if dir == "" {
			return ""
		}
		return filepath.Join(dir, filepath.FromSlash(a.userFile))
	}
	return filepath.Join(repoRoot, filepath.FromSlash(a.repoFile))
}

func (a settingsAgent) Install(scope Scope, repoRoot string) (Change, error) {
	return installSettingsHook(a.HookFile(scope, repoRoot), a.id)
}

func (a settingsAgent) Uninstall(scope Scope, repoRoot string) (Change, error) {
	return uninstallSettingsHook(a.HookFile(scope, repoRoot), a.id)
}

func (a settingsAgent) EmitSessionStart(stdout io.Writer) error {
	return emitSessionStart(stdout, a.install)
}

func (a settingsAgent) EmitPreToolUse(stdin io.Reader, stdout io.Writer) error {
	return emitPreToolUse(stdin, stdout)
}
