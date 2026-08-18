package adapter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
)

// toolMatcher covers the shell tool and the GitHub MCP server's PR call. The
// callback decides which commands actually open a PR.
const toolMatcher = `Bash|mcp__.*__create_pull_request`

// hookEvents are the events we install into, in the order an agent meets them:
// the standing instruction, then a commit, then the PR call itself. Each gets
// its own subcommand so the settings file says what it does.
var hookEvents = []hookEvent{
	{name: "SessionStart", subcommand: "session-start"},
	{name: "PostToolUse", matcher: shellToolName, subcommand: "post-tool-use"},
	{name: "PreToolUse", matcher: toolMatcher, subcommand: "pre-tool-use"},
}

type hookEvent struct {
	name       string
	matcher    string
	subcommand string
}

type cmdHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type matcherGroup struct {
	Matcher string    `json:"matcher,omitempty"`
	Hooks   []cmdHook `json:"hooks"`
}

// callbackCommand guards on aviator being installed so a teammate who pulled a
// committed hook without the CLI gets a no-op, and always exits 0. Its output
// must stay byte-stable — Codex and Gemini re-prompt for trust when it changes.
func callbackCommand(agent, subcommand string) string {
	return "command -v aviator >/dev/null 2>&1 && aviator hooks " + subcommand +
		" --agent=" + agent + " || true"
}

// ownsCommand reports whether cmd is one of our callbacks for agentID. The id
// must end at a boundary, or --agent=claude would claim --agent=claude-custom.
func ownsCommand(cmd, agentID string) bool {
	if !strings.Contains(cmd, "aviator hooks ") {
		return false
	}
	marker := "--agent=" + agentID
	_, rest, found := strings.Cut(cmd, marker)
	if !found {
		return false
	}
	return rest == "" || !isAgentIDChar(rest[0])
}

func isAgentIDChar(b byte) bool {
	return b == '-' || b == '_' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func desiredGroup(ev hookEvent, agentID string) matcherGroup {
	return matcherGroup{
		Matcher: ev.matcher,
		Hooks:   []cmdHook{{Type: "command", Command: callbackCommand(agentID, ev.subcommand)}},
	}
}

// marshalNoHTML keeps shell operators (>, &, |) readable instead of escaped.
func marshalNoHTML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// installSettingsHook writes our hook into every event in hookEvents, leaving
// every other key, event, group, and hook untouched.
func installSettingsHook(path, agentID string) (Change, error) {
	top, events, err := readSettings(path)
	if err != nil {
		return ChangeNone, err
	}

	had, changed := false, false
	for _, ev := range hookEvents {
		groups, err := eventGroups(events, ev.name)
		if err != nil {
			return ChangeNone, err
		}
		gi, hi, err := ownedHook(groups, agentID)
		if err != nil {
			return ChangeNone, err
		}
		if gi >= 0 {
			had = true
		}
		next, c, err := reconcile(groups, gi, hi, ev, agentID)
		if err != nil {
			return ChangeNone, err
		}
		if c == ChangeNone {
			continue
		}
		if err := setEventGroups(events, ev.name, next); err != nil {
			return ChangeNone, err
		}
		changed = true
	}

	switch {
	case !changed:
		return ChangeNone, nil
	case had:
		return ChangeUpdated, writeSettings(path, top, events)
	default:
		return ChangeAdded, writeSettings(path, top, events)
	}
}

// reconcile brings one event's groups in line with what we'd write now.
func reconcile(groups []json.RawMessage, gi, hi int, ev hookEvent, agentID string) ([]json.RawMessage, Change, error) {
	// A hook under the wrong matcher can never fire, so lift it out and let the
	// append below put it where it does.
	moved := false
	if gi >= 0 && groupMatcher(groups[gi]) != ev.matcher {
		var err error
		if groups, err = removeHook(groups, gi, hi); err != nil {
			return nil, ChangeNone, err
		}
		gi, moved = -1, true
	}

	if gi < 0 {
		group, err := marshalNoHTML(desiredGroup(ev, agentID))
		if err != nil {
			return nil, ChangeNone, err
		}
		change := ChangeAdded
		if moved {
			change = ChangeUpdated
		}
		return append(groups, group), change, nil
	}

	hooks, err := groupHooks(groups[gi])
	if err != nil {
		return nil, ChangeNone, err
	}
	desired, err := marshalNoHTML(cmdHook{Type: "command", Command: callbackCommand(agentID, ev.subcommand)})
	if err != nil {
		return nil, ChangeNone, err
	}
	if bytes.Equal(canonical(hooks[hi]), canonical(desired)) {
		return groups, ChangeNone, nil
	}
	hooks[hi] = desired
	if groups[gi], err = setGroupHooks(groups[gi], hooks); err != nil {
		return nil, ChangeNone, err
	}
	return groups, ChangeUpdated, nil
}

func uninstallSettingsHook(path, agentID string) (Change, error) {
	top, events, err := readSettings(path)
	if err != nil {
		return ChangeNone, err
	}

	removed := false
	for _, ev := range hookEvents {
		groups, err := eventGroups(events, ev.name)
		if err != nil {
			return ChangeNone, err
		}
		gi, hi, err := ownedHook(groups, agentID)
		if err != nil {
			return ChangeNone, err
		}
		if gi < 0 {
			continue
		}
		if groups, err = removeHook(groups, gi, hi); err != nil {
			return ChangeNone, err
		}
		if err := setEventGroups(events, ev.name, groups); err != nil {
			return ChangeNone, err
		}
		removed = true
	}
	if !removed {
		return ChangeNone, nil
	}
	return ChangeRemoved, writeSettings(path, top, events)
}

// ownedHook locates our entry as (group index, index within that group's hooks),
// or -1, -1. We own a single cmdHook, never the group it sits in — a user may
// have put their own hooks alongside ours.
func ownedHook(groups []json.RawMessage, agentID string) (int, int, error) {
	for gi, raw := range groups {
		hooks, err := groupHooks(raw)
		if err != nil {
			return 0, 0, err
		}
		for hi, raw := range hooks {
			var h cmdHook
			if err := json.Unmarshal(raw, &h); err != nil {
				return 0, 0, errors.Wrap(err, "malformed hook entry")
			}
			if ownsCommand(h.Command, agentID) {
				return gi, hi, nil
			}
		}
	}
	return -1, -1, nil
}

// removeHook drops one hook from a group, dropping the group once it empties.
func removeHook(groups []json.RawMessage, gi, hi int) ([]json.RawMessage, error) {
	hooks, err := groupHooks(groups[gi])
	if err != nil {
		return nil, err
	}
	hooks = append(hooks[:hi], hooks[hi+1:]...)
	if len(hooks) == 0 {
		return append(groups[:gi], groups[gi+1:]...), nil
	}
	groups[gi], err = setGroupHooks(groups[gi], hooks)
	return groups, err
}

// groupMatcher returns a group's matcher, or "" when it has none.
func groupMatcher(group json.RawMessage) string {
	obj, err := decodeObject(group, "hook group")
	if err != nil {
		return ""
	}
	var m string
	if err := json.Unmarshal(obj["matcher"], &m); err != nil {
		return ""
	}
	return m
}

// groupHooks returns a group's hooks as raw JSON, so sibling hooks keep fields
// we don't model (timeout, statusMessage) when we rewrite the list.
func groupHooks(group json.RawMessage) ([]json.RawMessage, error) {
	obj, err := decodeObject(group, "hook group")
	if err != nil {
		return nil, err
	}
	if len(obj["hooks"]) == 0 {
		return nil, nil
	}
	var hooks []json.RawMessage
	if err := json.Unmarshal(obj["hooks"], &hooks); err != nil {
		return nil, errors.Wrap(err, "malformed hooks list")
	}
	return hooks, nil
}

// setGroupHooks replaces a group's hooks list, preserving its other keys.
func setGroupHooks(group json.RawMessage, hooks []json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeObject(group, "hook group")
	if err != nil {
		return nil, err
	}
	raw, err := marshalNoHTML(hooks)
	if err != nil {
		return nil, err
	}
	obj["hooks"] = raw
	return marshalNoHTML(obj)
}

func eventGroups(events map[string]json.RawMessage, name string) ([]json.RawMessage, error) {
	raw, ok := events[name]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, errors.Errorf("%s hooks are not a list", name)
	}
	return groups, nil
}

func setEventGroups(events map[string]json.RawMessage, name string, groups []json.RawMessage) error {
	if len(groups) == 0 {
		delete(events, name)
		return nil
	}
	raw, err := marshalNoHTML(groups)
	if err != nil {
		return err
	}
	events[name] = raw
	return nil
}

// readSettings returns the file's top-level object and its hooks section.
func readSettings(path string) (top, events map[string]json.RawMessage, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to read %s", path)
	}
	if top, err = decodeObject(data, path); err != nil {
		return nil, nil, err
	}
	if events, err = decodeObject(top["hooks"], path+" hooks section"); err != nil {
		return nil, nil, err
	}
	return top, events, nil
}

// decodeObject requires a JSON object. A null or a scalar is an error rather
// than the nil map json.Unmarshal would otherwise hand back for us to write to.
func decodeObject(raw []byte, what string) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, errors.Wrapf(err, "%s is not a JSON object", what)
	}
	if obj == nil {
		return nil, errors.Errorf("%s is null, expected a JSON object", what)
	}
	return obj, nil
}

func writeSettings(path string, top, events map[string]json.RawMessage) error {
	if len(events) == 0 {
		delete(top, "hooks")
	} else {
		raw, err := marshalNoHTML(events)
		if err != nil {
			return err
		}
		top["hooks"] = raw
	}

	// Our hook was the file's only content, so don't leave an empty one behind.
	if len(top) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Wrapf(err, "failed to remove %s", path)
		}
		return nil
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(top); err != nil {
		return err
	}
	return writeFileAtomic(path, buf.Bytes())
}

// writeFileAtomic renames a completed temp file over path, so an interrupted
// write can't leave the agent's settings truncated.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".aviator-settings-*")
	if err != nil {
		return errors.Wrapf(err, "failed to write %s", path)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return errors.Wrapf(err, "failed to write %s", path)
	}
	if err := tmp.Close(); err != nil {
		return errors.Wrapf(err, "failed to write %s", path)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// canonical normalises key order and whitespace before comparing.
func canonical(raw json.RawMessage) []byte {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}
