// Package git wraps the git commands aviator-cli needs. It shells out to the
// git binary, mirroring the av CLI's internal/git so the two can converge on a
// shared library later.
package git

import (
	"context"
	"os/exec"
	"strings"

	"emperror.dev/errors"
)

// RepoRoot returns the absolute path to the working tree root
// (git rev-parse --show-toplevel). It errors when not inside a work tree.
func RepoRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", errors.Wrap(err, "not inside a git repository")
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", errors.New("could not determine repository root")
	}
	return root, nil
}
