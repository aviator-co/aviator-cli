package adapter

import "regexp"

// prCommand matches a shell command that opens a pull request — gh pr create or
// any av pr invocation. It runs against the whole command string, so it still
// fires through cd/env prefixes and && chains. Missing a match only means no
// reminder (fail-open), never a broken command.
var prCommand = regexp.MustCompile(`\b(gh\s+pr\s+create|av\s+pr)\b`)

// isPRCommand reports whether cmd opens a PR.
func isPRCommand(cmd string) bool {
	return prCommand.MatchString(cmd)
}

// reminderText is the nudge injected when a PR command is about to run. It
// points at the shared skills for the quality bar and the command to submit.
const reminderText = "Before finalizing this PR, capture it for Aviator Verify: " +
	"compose the change's intent and acceptance criteria (use the acceptance-criteria " +
	"and spec-submission skills for the quality bar), then submit them — run /verify-submit " +
	"if available, otherwise `aviator verify --working-branch <branch> --intent <...> --criteria <...>`. " +
	"This binds the PR to Verify so it is checked against those criteria."
