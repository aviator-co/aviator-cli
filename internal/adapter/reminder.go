package adapter

import (
	"encoding/json"
	"io"
	"regexp"
)

const shellToolName = "Bash"

// prCommand matches PR-opening shell commands. The gh branch tolerates global
// flags (gh --repo x pr create) but won't cross a command separator.
var prCommand = regexp.MustCompile(`\bgh\b[^;&|]*\bpr\s+create\b|\bav\s+pr\b`)

// mcpPRTool matches the GitHub MCP server's PR call, whatever the server is named.
var mcpPRTool = regexp.MustCompile(`^mcp__.*__create_pull_request$`)

// reminderText backs up the session instruction at the PR call. Claude and Codex
// deliver PreToolUse context next to the tool result, so by the time this is read
// the PR command has run; the wording holds either way.
const reminderText = "This PR needs its intent and acceptance criteria in Aviator Verify. " +
	"Run /verify-submit now, while the context of the change is still live."

// sessionText is the standing instruction. SessionStart delivers it before the
// first prompt, which is the only point we can reach the agent ahead of a PR.
// howToInstall is agent-specific, since /verify-submit arrives differently.
func sessionText(howToInstall string) string {
	return "This repository uses Aviator Verify. Before opening a pull request, capture the " +
		"change's intent and acceptance criteria by running /verify-submit — do it while the " +
		"reasoning behind the change is still in context, since it can't be reconstructed " +
		"afterwards. If /verify-submit isn't available: " + howToInstall
}

func isPRCommand(cmd string) bool {
	return prCommand.MatchString(cmd)
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
