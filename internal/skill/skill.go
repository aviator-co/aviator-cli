// Package skill installs Aviator Verify guidance skills into an agent's skills
// directory by fetching them from the canonical agent-plugins repo on GitHub, so
// the guidance is always the current single source rather than a vendored copy.
package skill

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/adapter"
)

// rawBase is the raw.githubusercontent base for the agent-plugins skills, always
// pulled from master (the source of truth).
const rawBase = "https://raw.githubusercontent.com/aviator-co/agent-plugins/master/aviator/skills"

// Names are the guidance skills init installs for agents that don't already get
// them from the Claude plugin: the AC quality rulebook and the submission
// mechanics.
var Names = []string{
	"acceptance-criteria",
	"spec-submission",
}

// fetch downloads a skill's SKILL.md from GitHub master.
func fetch(ctx context.Context, name string) ([]byte, error) {
	url := rawBase + "/" + name + "/SKILL.md"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch %s skill", name)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("failed to fetch %s skill: GitHub returned %d", name, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// install writes content to <dir>/<name>/SKILL.md, reporting whether it changed.
func install(dir, name string, content []byte) (adapter.Change, error) {
	path := filepath.Join(dir, name, "SKILL.md")
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == string(content) {
			return adapter.ChangeNone, nil
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			return adapter.ChangeNone, err
		}
		return adapter.ChangeUpdated, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return adapter.ChangeNone, err
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return adapter.ChangeNone, err
	}
	return adapter.ChangeAdded, nil
}

// Sync fetches every skill from GitHub and installs it into dir. On the first
// fetch failure it returns the changes made so far plus the error, so the caller
// can warn without aborting the rest of init.
func Sync(ctx context.Context, dir string) (map[string]adapter.Change, error) {
	changes := map[string]adapter.Change{}
	for _, name := range Names {
		content, err := fetch(ctx, name)
		if err != nil {
			return changes, err
		}
		change, err := install(dir, name, content)
		if err != nil {
			return changes, err
		}
		changes[name] = change
	}
	return changes, nil
}

// Uninstall removes our installed skills from dir.
func Uninstall(dir string) (adapter.Change, error) {
	changed := adapter.ChangeNone
	for _, name := range Names {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			if err := os.RemoveAll(p); err != nil {
				return changed, err
			}
			changed = adapter.ChangeRemoved
		}
	}
	return changed, nil
}
