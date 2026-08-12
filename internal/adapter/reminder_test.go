package adapter

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestIsPRCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"gh pr create", true},
		{"gh pr create --draft --title x", true},
		{"gh --repo owner/repo pr create", true},
		{"gh -R owner/repo pr create", true},
		{"av pr", true},
		{"av pr --force", true},
		{"av pr --all", true},
		{"cd sub && gh pr create", true},
		{"GH_TOKEN=x gh pr create", true},
		{"git push && av pr && echo done", true},

		// Stacks aren't av-specific, so Graphite's submit family counts too.
		{"gt submit", true},
		{"gt submit --stack", true},
		{"gt ss", true},
		{"gt stack submit", true},
		{"gt branch submit", true},

		{"gh pr list", false},
		{"gh pr view 12", false},
		{"gh pr list; echo pr create", false},
		{"av prune", false},
		{"nav prod", false},
		{"git push", false},

		// Pushes and syncs move commits without opening a PR; reminding there
		// would fire on most of a stacked workflow.
		{"av sync", false},
		{"gt sync", false},
		{"gt ssh-thing", false},
		{"gt log", false},
	}
	for _, tt := range tests {
		if got := isPRCommand(tt.cmd); got != tt.want {
			t.Errorf("isPRCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func contextOf(t *testing.T, out string) (event, text string) {
	t.Helper()
	var resp struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("parse %q: %v", out, err)
	}
	return resp.HookSpecificOutput.HookEventName, resp.HookSpecificOutput.AdditionalContext
}

func emitPre(t *testing.T, payload string) string {
	t.Helper()
	var out bytes.Buffer
	if err := emitPreToolUse(strings.NewReader(payload), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// SessionStart is the only point we reach the agent before a PR command, since
// PreToolUse context is delivered next to the tool result.
func TestSessionStartCarriesTheStandingInstruction(t *testing.T) {
	var out bytes.Buffer
	if err := emitSessionStart(&out, "run /plugin install aviator"); err != nil {
		t.Fatal(err)
	}
	event, text := contextOf(t, out.String())
	if event != "SessionStart" {
		t.Errorf("hookEventName = %q, want SessionStart", event)
	}
	if !strings.Contains(text, "/verify-submit") {
		t.Errorf("instruction does not name the command: %q", text)
	}
	if !strings.Contains(text, "run /plugin install aviator") {
		t.Errorf("instruction does not carry the agent's install hint: %q", text)
	}
}

// Agents kept submitting one session for a whole stack, which the backend
// refuses, so the standing instruction has to spell out the one-per-PR rule.
func TestSessionStartStatesOneSessionPerPR(t *testing.T) {
	var out bytes.Buffer
	if err := emitSessionStart(&out, "run /plugin install aviator"); err != nil {
		t.Fatal(err)
	}
	_, text := contextOf(t, out.String())
	for _, want := range []string{"exactly one PR", "3 submissions"} {
		if !strings.Contains(text, want) {
			t.Errorf("instruction missing %q: %q", want, text)
		}
	}
}

// Resubmitting on a branch that already has a session leaves two sessions
// claiming it, which breaks auto-linking, so the reminder points at edit.
func TestReminderPointsAtEditForAnExistingSession(t *testing.T) {
	for _, want := range []string{"aviator edit", "auto-linking"} {
		if !strings.Contains(reminderText, want) {
			t.Errorf("reminder missing %q: %q", want, reminderText)
		}
	}
}

// The whole reason the hook passes --agent: each one gets /verify-submit
// differently, so the instruction has to differ too.
func TestSessionStartIsAgentSpecific(t *testing.T) {
	texts := map[string]string{}
	for _, a := range All() {
		var out bytes.Buffer
		if err := a.EmitSessionStart(&out); err != nil {
			t.Fatal(err)
		}
		_, text := contextOf(t, out.String())
		if text == "" {
			t.Fatalf("%s emitted no instruction", a.ID())
		}
		texts[a.ID()] = text
	}
	if texts["claude"] == texts["codex"] {
		t.Error("every agent got identical instructions, so --agent buys nothing")
	}
}

func TestPRCommandsGetTheReminder(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"shell", `{"tool_name":"Bash","tool_input":{"command":"gh pr create"}}`},
		{"github mcp", `{"tool_name":"mcp__github__create_pull_request","tool_input":{}}`},
	}
	for _, tt := range tests {
		event, text := contextOf(t, emitPre(t, tt.payload))
		if event != "PreToolUse" {
			t.Errorf("%s: hookEventName = %q, want PreToolUse", tt.name, event)
		}
		if text != reminderText {
			t.Errorf("%s: additionalContext = %q, want the reminder", tt.name, text)
		}
	}
}

// Anything else must produce no output, or the hook would inject noise before
// every shell command an agent runs.
func TestPreToolUseStaysSilent(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"unrelated command", `{"tool_name":"Bash","tool_input":{"command":"go test ./..."}}`},
		{"non-shell tool", `{"tool_name":"Read","tool_input":{"command":"gh pr create"}}`},
		{"unrelated mcp tool", `{"tool_name":"mcp__github__list_issues"}`},
		{"missing tool_input", `{"tool_name":"Bash"}`},
		{"unparseable", `not json`},
		{"empty", ``},
	}
	for _, tt := range tests {
		if got := emitPre(t, tt.payload); got != "" {
			t.Errorf("%s: emitted %q, want no output", tt.name, got)
		}
	}
}
