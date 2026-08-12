package adapter

import (
	"encoding/json"
	"io"
	"regexp"
)

const shellToolName = "Bash"

// prCommands matches PR-opening shell commands, one pattern per tool so each
// stays readable and a new tool is one line rather than another alternation.
//
// Matching is deliberately loose. A false positive costs one reminder the agent
// ignores; a missed PR is the failure the hook exists to prevent. What it must
// not do is fire often: pushes and syncs (git push, av sync, gt sync) precede a
// PR but recur throughout a stacked workflow, and a reminder on every one of
// them burns tokens and trains the agent to skim past the text.
//
// The word boundaries carry the weight: they are what keeps `av prune` and
// `gt ssh` out, which plain substring search would let through.
var prCommands = []*regexp.Regexp{
	// gh tolerates global flags (gh --repo x pr create), but must not reach
	// across a separator into an unrelated command.
	regexp.MustCompile(`\bgh\b[^;&|]*\bpr\s+create\b`),
	regexp.MustCompile(`\bav\s+pr\b`),
	// Graphite's submit family: stacks aren't av-specific.
	regexp.MustCompile(`\bgt\s+(submit|ss)\b`),
	regexp.MustCompile(`\bgt\s+(stack|branch|downstack|upstack)\s+submit\b`),
}

// mcpPRTool matches the GitHub MCP server's PR call, whatever the server is named.
var mcpPRTool = regexp.MustCompile(`^mcp__.*__create_pull_request$`)

// reminderText backs up the session instruction at the PR call. Claude and Codex
// deliver PreToolUse context next to the tool result, so by the time this is read
// the PR command has run; the wording holds either way.
const reminderText = "This PR needs its intent and acceptance criteria in Aviator Verify. " +
	"Run /verify-submit now, while the context of the change is still live. " +
	"One verify session tracks exactly one PR. If this branch already has a session, refresh " +
	"its acceptance criteria with `aviator edit` instead of submitting again: a second " +
	"submission for the same branch creates a duplicate session and breaks PR auto-linking " +
	"for that branch."

// sessionText is the standing instruction. SessionStart delivers it before the
// first prompt, which is the only point we can reach the agent ahead of a PR.
// howToInstall is agent-specific, since /verify-submit arrives differently.
func sessionText(howToInstall string) string {
	return "This repository uses Aviator Verify. Before opening a pull request, capture the " +
		"change's intent and acceptance criteria by running /verify-submit — do it while the " +
		"reasoning behind the change is still in context, since it can't be reconstructed " +
		"afterwards. One verify session tracks exactly one PR: every PR gets its own " +
		"submission with its own intent and acceptance criteria, so a stack of 3 PRs means 3 " +
		"submissions, not one covering the stack. " +
		"If /verify-submit isn't available: " + howToInstall
}

func isPRCommand(cmd string) bool {
	for _, re := range prCommands {
		if re.MatchString(cmd) {
			return true
		}
	}
	return false
}

// emitSessionStart writes the standing instruction. It takes no input — the
// event carries nothing we need.
func emitSessionStart(stdout io.Writer, howToInstall string) error {
	return emitContext(stdout, "SessionStart", sessionText(howToInstall))
}

// emitPreToolUse injects the reminder when the call opens a PR. It fails open:
// anything unparseable or unrelated produces no output, so the tool proceeds.
func emitPreToolUse(stdin io.Reader, stdout io.Writer) error {
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
	if err := json.Unmarshal(data, &in); err != nil {
		return nil
	}
	shellPR := in.ToolName == shellToolName && isPRCommand(in.ToolInput.Command)
	if !shellPR && !mcpPRTool.MatchString(in.ToolName) {
		return nil
	}
	return emitContext(stdout, "PreToolUse", reminderText)
}

func emitContext(stdout io.Writer, event, text string) error {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     event,
			"additionalContext": text,
		},
	})
}
