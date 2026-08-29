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
// ignores; a missed PR is the failure the hook exists to prevent. Pushes stay
// out: they precede a PR without opening one, so they belong to the criteria
// reminder below rather than to the one about the PR body.
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

// commitOrPushCommands matches commits and pushes, after which criteria
// written earlier may no longer match the code.
var commitOrPushCommands = []*regexp.Regexp{
	regexp.MustCompile(`\bgit\b[^;&|]*\bcommit\b`),
	regexp.MustCompile(`\bgit\b[^;&|]*\bpush\b`),
	regexp.MustCompile(`\bav\s+(commit|sync)\b`),
	regexp.MustCompile(`\bgt\s+(create|modify|absorb|cc|ca|bc|ab|c|m)\b`),
	regexp.MustCompile(`\bgt\s+(commit|branch)\s+(create|amend)\b`),
}

// commitOrPushText follows every commit and push, so it stays short and has
// the agent look the session up instead of guessing at one.
const commitOrPushText = "Verify: does this branch have a session? Check with " +
	"`aviator sessions --repo <owner/repo> --branch <branch>`. If it has one, update its " +
	"criteria to match the code. If it doesn't, add \"run /verify-submit before opening a " +
	"PR\" to your task list."

// reminderText is delivered beside the tool result, once the PR command has
// already run, so it asks for a repair rather than for the submission.
const reminderText = "Verify: a PR links to its session by a `Runbook: <url>` line at the top " +
	"of the body, not by pushing the branch. Find the session with " +
	"`aviator sessions --repo <owner/repo> --branch <branch>`, or run /verify-submit if the " +
	"branch doesn't have one yet."

// signInText is appended when the machine has no Aviator credentials, since an
// agent that reaches /verify-submit without them only finds out when the
// submission fails.
const signInText = "No Aviator credentials were found, so this session can't submit " +
	"anything to Aviator. Tell the user to sign in with `aviator login`."

// missingCLIText is delivered by the hook itself when aviator is not on PATH.
// Unlike the texts the CLI emits, it is frozen into every hook file already
// installed, so it stays clear of anything we might rename: the CLI and its
// install are safe, a skill name is not.
const missingCLIText = "This repository uses Aviator Verify, but the aviator CLI isn't " +
	"installed, so this session can't submit anything to it. Tell the user to install " +
	"it with `brew install aviator-co/tap/aviator`, then sign in with `aviator login`."

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
	return matchesAny(prCommands, cmd)
}

func isCommitOrPushCommand(cmd string) bool {
	return matchesAny(commitOrPushCommands, cmd)
}

func matchesAny(res []*regexp.Regexp, cmd string) bool {
	for _, re := range res {
		if re.MatchString(cmd) {
			return true
		}
	}
	return false
}

// emitSessionStart writes the standing instruction. It takes no input — the
// event carries nothing we need.
func emitSessionStart(stdout io.Writer, howToInstall string, signedIn bool) error {
	text := sessionText(howToInstall)
	if !signedIn {
		text += " " + signInText
	}
	return emitContext(stdout, "SessionStart", text)
}

type hookInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

// readPayload returns the zero value for an unparseable payload, whose empty
// tool name matches nothing, so callers turn it into silence themselves.
func readPayload(stdin io.Reader) (hookInput, error) {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return hookInput{}, err
	}
	var in hookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return hookInput{}, nil
	}
	return in, nil
}

// emitPreToolUse injects the reminder when the call opens a PR. It fails open:
// anything unparseable or unrelated produces no output, so the tool proceeds.
func emitPreToolUse(stdin io.Reader, stdout io.Writer) error {
	in, err := readPayload(stdin)
	if err != nil {
		return err
	}
	shellPR := in.ToolName == shellToolName && isPRCommand(in.ToolInput.Command)
	if !shellPR && !mcpPRTool.MatchString(in.ToolName) {
		return nil
	}
	return emitContext(stdout, "PreToolUse", reminderText)
}

func emitPostToolUse(stdin io.Reader, stdout io.Writer) error {
	in, err := readPayload(stdin)
	if err != nil {
		return err
	}
	if in.ToolName != shellToolName || !isCommitOrPushCommand(in.ToolInput.Command) {
		return nil
	}
	return emitContext(stdout, "PostToolUse", commitOrPushText)
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
