// Package adapter installs and manages the Aviator Verify pre-PR reminder hook
// across AI coding agents. Each supported agent implements Adapter; the hook
// nudges the agent to submit intent + acceptance criteria to Verify before a PR
// is opened.
package adapter

import "io"

// Scope selects where an agent's hook is written.
//
// TODO(user-global): add a machine-wide scope (e.g. ~/.claude/settings.json) so
// a user can install the reminder for every repo at once. Deferred because a
// global hook fires in non-Aviator repos too and needs an "is this repo
// connected" self-check we haven't designed yet.
type Scope int

const (
	// ScopeRepoShared writes to the repo's committed config so the whole team
	// inherits the reminder (e.g. .claude/settings.json).
	ScopeRepoShared Scope = iota
	// ScopeRepoLocal writes to the repo's gitignored config so only the
	// installing user gets it (e.g. .claude/settings.local.json).
	ScopeRepoLocal
)

// Change describes what an Install/Uninstall did, so callers can report
// accurately and stay quiet when nothing moved.
type Change int

const (
	ChangeNone Change = iota
	ChangeAdded
	ChangeUpdated
	ChangeRemoved
)

// Status reports whether our hook is present for an agent at a scope, and
// whether the installed entry is stale (differs from what we'd write now).
type Status struct {
	Installed bool
	Stale     bool
	Path      string
}

// Adapter installs, removes, and serves the pre-PR reminder hook for one agent.
type Adapter interface {
	// ID is the stable key used on the command line (e.g. "claude").
	ID() string
	// DisplayName is the human-facing name (e.g. "Claude Code").
	DisplayName() string
	// Detect reports whether the agent is present for the current user.
	Detect() bool
	// Install writes or updates the reminder hook at scope. Idempotent.
	Install(scope Scope, repoRoot string) (Change, error)
	// Uninstall removes only our hook entry, leaving other config intact.
	Uninstall(scope Scope, repoRoot string) (Change, error)
	// Status reports whether our hook is installed at scope.
	Status(scope Scope, repoRoot string) (Status, error)
	// EmitReminder reads a native hook payload from stdin and writes the native
	// hook response to stdout — the callback the installed hook runs.
	EmitReminder(stdin io.Reader, stdout io.Writer) error
	// SkillsDir returns the directory this agent loads skills from at scope, and
	// whether init should install the Verify guidance skills there. It returns
	// false when another channel already provides them (e.g. the Claude plugin).
	SkillsDir(scope Scope, repoRoot string) (dir string, install bool)
}

// registry holds the built-in adapters in a stable order.
var registry = []Adapter{
	Claude{},
}

// All returns every registered adapter.
func All() []Adapter { return registry }

// Find returns the adapter with the given ID, or nil.
func Find(id string) Adapter {
	for _, a := range registry {
		if a.ID() == id {
			return a
		}
	}
	return nil
}

// Detected returns the adapters present for the current user.
func Detected() []Adapter {
	var out []Adapter
	for _, a := range registry {
		if a.Detect() {
			out = append(out, a)
		}
	}
	return out
}
