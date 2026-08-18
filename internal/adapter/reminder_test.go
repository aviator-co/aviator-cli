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

func TestIsCommitCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git commit -m 'x'", true},
		{"git commit --amend --no-edit", true},
		{"git -C /repo commit -m x", true},
		{"git add -A && git commit -m x", true},
		{"av commit -m x", true},
		{"av commit --amend", true},
		{"gt create -m x", true},
		{"gt modify", true},
		{"gt c", true},
		{"gt m", true},
		{"gt commit create -m x", true},
		{"gt commit amend", true},
		{"gt branch create x", true},
		{"gt cc -m x", true},
		{"gt ca", true},
		{"gt bc x", true},
		{"gt absorb", true},
		{"gt ab", true},

		{"git log --oneline", false},
		{"git push", false},
		{"gh pr create", false},
		{"av sync", false},
		{"git status; echo commit", false},
		{"gt config", false},
		{"gt checkout main", false},
		{"gt branch delete x", false},
	}
	for _, tt := range tests {
		if got := isCommitCommand(tt.cmd); got != tt.want {
			t.Errorf("isCommitCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
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

func emitPost(t *testing.T, payload string) string {
	t.Helper()
	var out bytes.Buffer
	if err := emitPostToolUse(strings.NewReader(payload), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func commitPayload(command string) string {
	return `{"tool_name":"Bash","tool_input":{"command":"` + command + `"}}`
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

// Agents kept opening the PR first, so the ask moved to the commit, and asks
// for a task item because that outlives the turn it is read in.
func TestCommitAsksForATaskItem(t *testing.T) {
	event, text := contextOf(t, emitPost(t, commitPayload("git commit -m x")))
	if event != "PostToolUse" {
		t.Errorf("hookEventName = %q, want PostToolUse", event)
	}
	for _, want := range []string{
		"task list",
		`"run /verify-submit before opening a PR"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("directive missing %q: %q", want, text)
		}
	}
}

// It follows every commit, so a branch that already has a session has to be
// able to stop at the first sentence rather than submit a second time.
func TestCommitTextExcusesAnExistingSession(t *testing.T) {
	first, _, _ := strings.Cut(commitText, ".")
	for _, want := range []string{"already has a Verify session", "nothing to do"} {
		if !strings.Contains(first, want) {
			t.Errorf("first sentence missing %q: %q", want, first)
		}
	}
}

// Anything else must produce no output, or the hook would inject noise after
// every shell command an agent runs.
func TestPostToolUseStaysSilent(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"unrelated command", commitPayload("go test ./...")},
		{"non-shell tool", `{"tool_name":"Read","tool_input":{"command":"git commit"}}`},
		{"missing tool_input", `{"tool_name":"Bash"}`},
		{"unparseable", `not json`},
		{"empty", ``},
	}
	for _, tt := range tests {
		if got := emitPost(t, tt.payload); got != "" {
			t.Errorf("%s: emitted %q, want no output", tt.name, got)
		}
	}
}

// By the PR call the agent has usually submitted, so an unconditional "submit
// now" here is what puts two sessions on one branch.
func TestReminderIsConditionalRemediation(t *testing.T) {
	for _, want := range []string{"Runbook:", "/verify-submit", "refreshes its criteria"} {
		if !strings.Contains(reminderText, want) {
			t.Errorf("reminder missing %q: %q", want, reminderText)
		}
	}
	if strings.Contains(reminderText, "aviator edit") {
		t.Errorf("reminder points at an edit the agent can't perform: %q", reminderText)
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
