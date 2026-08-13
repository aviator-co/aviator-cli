// Package adapter installs and serves the Aviator Verify pre-PR reminder hook
// across AI coding agents.
package adapter

import "io"

// Scope selects where an agent's hook is written.
type Scope int

const (
	// ScopeTeam writes to the repo's committed config, shared with everyone.
	ScopeTeam Scope = iota
	// ScopeSelf writes to the user's own config, covering every repo on this
	// machine and touching nothing in the working tree.
	ScopeSelf
)

type Change int

const (
	ChangeNone Change = iota
	ChangeAdded
	ChangeUpdated
	ChangeRemoved
)

type Adapter interface {
	ID() string
	DisplayName() string
	// Detect reports whether the agent is present for the current user.
	Detect() bool
	// Install writes or updates the reminder hook at scope. Idempotent.
	Install(scope Scope, repoRoot string) (Change, error)
	// Uninstall removes only our hook entry, leaving other config intact.
	Uninstall(scope Scope, repoRoot string) (Change, error)
	// EmitSessionStart writes the standing instruction, tailored to how this
	// agent gets /verify-submit.
	EmitSessionStart(stdout io.Writer) error
	// EmitPreToolUse reads a native hook payload from stdin and writes the
	// native response to stdout when the call opens a PR.
	EmitPreToolUse(stdin io.Reader, stdout io.Writer) error
	// HookFile is the config file written at scope, so init can offer to keep a
	// self-scope file out of git.
	HookFile(scope Scope, repoRoot string) string
	// SetupNote is anything the user must still do before the hook will fire,
	// or "" when writing the file is enough.
	SetupNote() string
}

// All returns every registered adapter. Team scope installs all of them, since
// what's on this machine says nothing about what teammates run.
func All() []Adapter { return registry }

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
