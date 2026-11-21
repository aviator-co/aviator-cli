package adapter

import (
	"encoding/json"
	"io"
	"regexp"
)

// readOnlyCommand matches a lone read of a session or its decisions, optionally
// after a cd and before a head. Separators are spaces and tabs only, since a
// newline would let a second command ride along as arguments.
var readOnlyCommand = func() *regexp.Regexp {
	const (
		sp   = `[ \t]+`
		word = `[\w./:@%+=,~-]+`
	)
	return regexp.MustCompile(`^[ \t]*` +
		`(cd` + sp + word + `[ \t]*&&[ \t]*)?` +
		`aviator` + sp + `(sessions|decisions` + sp + `(list|show))` +
		`(` + sp + word + `)*` +
		`(` + sp + `2>&1)?` +
		`([ \t]*\|[ \t]*head(` + sp + `-n)?` + sp + `-?\d+)?` +
		`[ \t]*$`)
}()

func isReadOnlyCommand(cmd string) bool {
	return readOnlyCommand.MatchString(cmd)
}

func emitAllow(stdout io.Writer) error {
	return json.NewEncoder(stdout).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": "read-only aviator command",
		},
	})
}
