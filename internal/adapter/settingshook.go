package adapter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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

// ourCommand reports whether cmd is one of our callbacks for agentID, and which
// subcommand it calls. The id must end at a boundary, or --agent=claude would
// claim --agent=claude-custom.
func ourCommand(cmd, agentID string) (subcommand string, ours bool) {
	_, rest, found := strings.Cut(cmd, "aviator hooks ")
	if !found {
		return "", false
	}
	_, after, found := strings.Cut(rest, "--agent="+agentID)
	if !found || (after != "" && isAgentIDChar(after[0])) {
		return "", false
	}
	sub, _, _ := strings.Cut(rest, " ")
	if strings.HasPrefix(sub, "-") {
		return "", true
	}
	return sub, true
}

func ownsCommand(cmd, agentID string) bool {
	_, ours := ourCommand(cmd, agentID)
	return ours
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

type ourHook struct {
	event      string
	group, idx int
	matcher    string
	subcommand string
	command    string
}

// installSettingsHook mends the entries of ours already in place, drops every
// other entry of ours, and appends what is missing. Nothing outside our own
// entries is added, changed, or removed.
func installSettingsHook(path, agentID string) (Change, error) {
	top, events, err := readSettings(path)
	if err != nil {
		return ChangeNone, err
	}
	found, err := ourHooks(events, agentID)
	if err != nil {
		return ChangeNone, err
	}

	claimed := make([]bool, len(found))
	missing := make([]hookEvent, 0, len(hookEvents))
	changed := false
	for _, ev := range hookEvents {
		i := claimFor(found, claimed, ev)
		if i < 0 {
			missing = append(missing, ev)
			continue
		}
		claimed[i] = true
		desired := callbackCommand(agentID, ev.subcommand)
		if found[i].command == desired {
			continue
		}
		if err := setHookCommand(events, found[i], desired); err != nil {
			return ChangeNone, err
		}
		changed = true
	}

	var stale []ourHook
	for i, h := range found {
		if !claimed[i] {
			stale = append(stale, h)
		}
	}
	if len(stale) > 0 {
		if err := removeAll(events, stale); err != nil {
			return ChangeNone, err
		}
		changed = true
	}

	for _, ev := range missing {
		if err := appendGroup(events, ev, agentID); err != nil {
			return ChangeNone, err
		}
		changed = true
	}

	switch {
	case !changed:
		return ChangeNone, nil
	case len(found) > 0:
		return ChangeUpdated, writeSettings(path, top, events)
	default:
		return ChangeAdded, writeSettings(path, top, events)
	}
}

func uninstallSettingsHook(path, agentID string) (Change, error) {
	top, events, err := readSettings(path)
	if err != nil {
		return ChangeNone, err
	}
	found, err := ourHooks(events, agentID)
	if err != nil {
		return ChangeNone, err
	}
	if len(found) == 0 {
		return ChangeNone, nil
	}
	if err := removeAll(events, found); err != nil {
		return ChangeNone, err
	}
	return ChangeRemoved, writeSettings(path, top, events)
}

// claimFor picks the entry already serving ev, so install leaves a hook in
// whatever group the user filed it in. A hook under the wrong event or matcher
// can never fire, so it goes unclaimed and install replaces it.
func claimFor(found []ourHook, claimed []bool, ev hookEvent) int {
	for i, h := range found {
		if claimed[i] {
			continue
		}
		if h.event == ev.name && h.matcher == ev.matcher && h.subcommand == ev.subcommand {
			return i
		}
	}
	return -1
}

// ourHooks walks the whole hooks section rather than hookEvents, so a hook left
// behind by a version that installed somewhere we no longer do is still found.
func ourHooks(events map[string]json.RawMessage, agentID string) ([]ourHook, error) {
	names := make([]string, 0, len(events))
	for name := range events {
		names = append(names, name)
	}
	sort.Strings(names)

	var found []ourHook
	for _, name := range names {
		hooks, err := scanEvent(events, name, agentID)
		if err != nil {
			// An event we write into has to be readable; an unreadable one we
			// never touch holds nothing of ours anyway.
			if !installsInto(name) {
				continue
			}
			return nil, err
		}
		found = append(found, hooks...)
	}
	return found, nil
}

func scanEvent(events map[string]json.RawMessage, name, agentID string) ([]ourHook, error) {
	groups, err := eventGroups(events, name)
	if err != nil {
		return nil, err
	}
	var found []ourHook
	for gi, group := range groups {
		hooks, err := groupHooks(group)
		if err != nil {
			return nil, err
		}
		for hi, raw := range hooks {
			var h cmdHook
			if err := json.Unmarshal(raw, &h); err != nil {
				return nil, errors.Wrap(err, "malformed hook entry")
			}
			sub, ours := ourCommand(h.Command, agentID)
			if !ours {
				continue
			}
			found = append(found, ourHook{
				event: name, group: gi, idx: hi,
				matcher: groupMatcher(group), subcommand: sub, command: h.Command,
			})
		}
	}
	return found, nil
}

func installsInto(event string) bool {
	for _, ev := range hookEvents {
		if ev.name == event {
			return true
		}
	}
	return false
}

func setHookCommand(events map[string]json.RawMessage, h ourHook, command string) error {
	groups, err := eventGroups(events, h.event)
	if err != nil {
		return err
	}
	hooks, err := groupHooks(groups[h.group])
	if err != nil {
		return err
	}
	if hooks[h.idx], err = marshalNoHTML(cmdHook{Type: "command", Command: command}); err != nil {
		return err
	}
	if groups[h.group], err = setGroupHooks(groups[h.group], hooks); err != nil {
		return err
	}
	return setEventGroups(events, h.event, groups)
}

func appendGroup(events map[string]json.RawMessage, ev hookEvent, agentID string) error {
	groups, err := eventGroups(events, ev.name)
	if err != nil {
		return err
	}
	group, err := marshalNoHTML(desiredGroup(ev, agentID))
	if err != nil {
		return err
	}
	return setEventGroups(events, ev.name, append(groups, group))
}

// removeAll works backwards through each event so the indices recorded for the
// earlier entries survive the lists shrinking.
func removeAll(events map[string]json.RawMessage, hooks []ourHook) error {
	byEvent := map[string][]ourHook{}
	for _, h := range hooks {
		byEvent[h.event] = append(byEvent[h.event], h)
	}
	for name, list := range byEvent {
		groups, err := eventGroups(events, name)
		if err != nil {
			return err
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].group != list[j].group {
				return list[i].group > list[j].group
			}
			return list[i].idx > list[j].idx
		})
		for _, h := range list {
			if groups, err = removeHook(groups, h.group, h.idx); err != nil {
				return err
			}
		}
		if err := setEventGroups(events, name, groups); err != nil {
			return err
		}
	}
	return nil
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
