package adapter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
)

// ownerPrefix identifies hook commands this CLI owns, so we can find, update,
// and remove exactly our entries without touching the user's other hooks.
const ownerPrefix = "aviator hooks"

// settingsHook installs, removes, and inspects a PreToolUse reminder hook in a
// settings file that uses Claude Code's hook schema (also used by Codex's
// hooks.json):
//
//	{ "hooks": { "PreToolUse": [ { "matcher": "...", "hooks": [ {"type":"command","command":"..."} ] } ] } }
//
// Unknown top-level keys, unknown hook events, and other matcher groups
// round-trip untouched; only our own entry (identified by ownerPrefix) is
// written or removed.

type cmdHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type matcherGroup struct {
	Matcher string    `json:"matcher,omitempty"`
	Hooks   []cmdHook `json:"hooks"`
}

// installSettingsHook ensures path contains our PreToolUse hook (matcher +
// command). It reports whether the entry was added, updated, or already current.
func installSettingsHook(path, matcher, command string) (Change, error) {
	top, err := readObjectFile(path)
	if err != nil {
		return ChangeNone, err
	}
	events, err := rawObject(top["hooks"])
	if err != nil {
		return ChangeNone, errors.Wrap(err, "malformed hooks section")
	}
	groups, err := preToolUse(events)
	if err != nil {
		return ChangeNone, err
	}

	desired, err := json.Marshal(matcherGroup{
		Matcher: matcher,
		Hooks:   []cmdHook{{Type: "command", Command: command}},
	})
	if err != nil {
		return ChangeNone, err
	}

	idx := ownedIndex(groups)
	change := ChangeAdded
	switch {
	case idx >= 0 && bytes.Equal(canonical(groups[idx]), canonical(desired)):
		return ChangeNone, nil
	case idx >= 0:
		groups[idx] = desired
		change = ChangeUpdated
	default:
		groups = append(groups, desired)
	}
	return change, writeGroups(path, top, events, groups)
}

// uninstallSettingsHook removes our PreToolUse entry from path, if present.
func uninstallSettingsHook(path string) (Change, error) {
	top, err := readObjectFile(path)
	if err != nil {
		return ChangeNone, err
	}
	events, err := rawObject(top["hooks"])
	if err != nil {
		return ChangeNone, err
	}
	groups, err := preToolUse(events)
	if err != nil {
		return ChangeNone, err
	}
	idx := ownedIndex(groups)
	if idx < 0 {
		return ChangeNone, nil
	}
	groups = append(groups[:idx], groups[idx+1:]...)
	return ChangeRemoved, writeGroups(path, top, events, groups)
}

// statusSettingsHook reports whether our entry is present and current at path.
func statusSettingsHook(path, matcher, command string) (Status, error) {
	st := Status{Path: path}
	top, err := readObjectFile(path)
	if err != nil {
		return st, err
	}
	events, err := rawObject(top["hooks"])
	if err != nil {
		return st, err
	}
	groups, err := preToolUse(events)
	if err != nil {
		return st, err
	}
	idx := ownedIndex(groups)
	if idx < 0 {
		return st, nil
	}
	st.Installed = true
	desired, err := json.Marshal(matcherGroup{Matcher: matcher, Hooks: []cmdHook{{Type: "command", Command: command}}})
	if err != nil {
		return st, err
	}
	st.Stale = !bytes.Equal(canonical(groups[idx]), canonical(desired))
	return st, nil
}

// preToolUse extracts the PreToolUse matcher groups as raw JSON, so other
// groups round-trip untouched.
func preToolUse(events map[string]json.RawMessage) ([]json.RawMessage, error) {
	raw, ok := events["PreToolUse"]
	if !ok {
		return nil, nil
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, errors.Wrap(err, "malformed PreToolUse hooks")
	}
	return groups, nil
}

// ownedIndex returns the index of the matcher group whose command we own, or -1.
func ownedIndex(groups []json.RawMessage) int {
	for i, raw := range groups {
		var g matcherGroup
		if err := json.Unmarshal(raw, &g); err != nil {
			continue
		}
		for _, h := range g.Hooks {
			if strings.HasPrefix(h.Command, ownerPrefix) {
				return i
			}
		}
	}
	return -1
}

// writeGroups rebuilds path with the updated PreToolUse groups, preserving every
// untouched key and event as raw JSON.
func writeGroups(path string, top, events map[string]json.RawMessage, groups []json.RawMessage) error {
	if len(groups) == 0 {
		delete(events, "PreToolUse")
	} else {
		raw, err := json.Marshal(groups)
		if err != nil {
			return err
		}
		events["PreToolUse"] = raw
	}

	if len(events) == 0 {
		delete(top, "hooks")
	} else {
		raw, err := json.Marshal(events)
		if err != nil {
			return err
		}
		top["hooks"] = raw
	}

	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

// readObjectFile reads path as a JSON object of raw values. A missing or empty
// file yields an empty (non-nil) map so callers can add to it.
func readObjectFile(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s", path)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, errors.Wrapf(err, "failed to parse %s", path)
	}
	return obj, nil
}

// rawObject parses a raw JSON object value (e.g. the "hooks" section) into a raw
// map, tolerating an absent value.
func rawObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// canonical re-marshals a raw JSON value to a stable form for comparison, so key
// ordering or whitespace differences don't read as "stale".
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
