package adapter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"emperror.dev/errors"
	"github.com/tailscale/hujson"
)

// toolMatcher covers the shell tool and the GitHub MCP server's PR call. The
// callback decides which commands actually open a PR.
const toolMatcher = `Bash|mcp__.*__create_pull_request`

const sessionStartEvent = "SessionStart"

// hookEvents are the events we install into, in the order an agent meets them:
// the standing instruction, then a commit, then the PR call itself. Each gets
// its own subcommand so the settings file says what it does.
var hookEvents = []hookEvent{
	{name: sessionStartEvent, subcommand: "session-start"},
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
// committed hook without the CLI gets a no-op, apart from session-start, which
// says so instead — and only when the CLI is missing, not when it fails. Its
// output must stay byte-stable — Codex and Gemini re-prompt for trust when it
// changes.
func callbackCommand(agent, subcommand string) string {
	call := "aviator hooks " + subcommand + " --agent=" + agent
	if subcommand == "session-start" {
		return "if command -v aviator >/dev/null 2>&1; then " + call +
			"; else " + missingCLIFallback() + "; fi"
	}
	return "command -v aviator >/dev/null 2>&1 && " + call + " || true"
}

// missingCLIFallback prints the payload the session-start callback would have
// printed, which is the one thing a missing CLI cannot report about itself.
func missingCLIFallback() string {
	var buf bytes.Buffer
	_ = emitContext(&buf, sessionStartEvent, missingCLIText)
	return "echo " + shellQuote(strings.TrimSpace(buf.String()))
}

// shellQuote wraps s for a POSIX shell, closing and reopening the quoted string
// around each apostrophe, so the message is written as prose rather than to
// suit the shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
// entries is added, changed, or removed, down to the byte.
func installSettingsHook(path, agentID string) (Change, error) {
	doc, events, err := readSettings(path)
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
		if err := setHookCommand(doc, found[i], desired); err != nil {
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
		if err := removeAll(doc, stale); err != nil {
			return ChangeNone, err
		}
		changed = true
	}

	for _, ev := range missing {
		if err := appendGroup(doc, ev, agentID); err != nil {
			return ChangeNone, err
		}
		changed = true
	}

	if err := prune(doc, stale); err != nil {
		return ChangeNone, err
	}

	switch {
	case !changed:
		return ChangeNone, nil
	case len(found) > 0:
		return ChangeUpdated, writeSettings(path, doc)
	default:
		return ChangeAdded, writeSettings(path, doc)
	}
}

func uninstallSettingsHook(path, agentID string) (Change, error) {
	doc, events, err := readSettings(path)
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
	if err := removeAll(doc, found); err != nil {
		return ChangeNone, err
	}
	if err := prune(doc, found); err != nil {
		return ChangeNone, err
	}
	return ChangeRemoved, writeSettings(path, doc)
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

func setHookCommand(doc *hujson.Value, h ourHook, command string) error {
	path := pointer("hooks", h.event, strconv.Itoa(h.group), "hooks", strconv.Itoa(h.idx), "command")
	return patch(doc, patchOp{Op: "replace", Path: path, Value: command})
}

func appendGroup(doc *hujson.Value, ev hookEvent, agentID string) error {
	group := desiredGroup(ev, agentID)
	if doc.Find("/hooks") == nil {
		return insert(doc, pointer(), "hooks", map[string][]matcherGroup{ev.name: {group}})
	}
	if doc.Find(pointer("hooks", ev.name)) != nil {
		return insert(doc, pointer("hooks", ev.name), "", group)
	}
	return insert(doc, pointer("hooks"), ev.name, []matcherGroup{group})
}

// removeAll works backwards through each event so the indices recorded for the
// earlier entries survive the lists shrinking. A group goes once it empties.
func removeAll(doc *hujson.Value, hooks []ourHook) error {
	sorted := slices.Clone(hooks)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.event != b.event {
			return a.event < b.event
		}
		if a.group != b.group {
			return a.group > b.group
		}
		return a.idx > b.idx
	})
	for _, h := range sorted {
		group := pointer("hooks", h.event, strconv.Itoa(h.group))
		if err := remove(doc, group+"/hooks/"+strconv.Itoa(h.idx)); err != nil {
			return err
		}
		if isEmpty(doc.Find(group + "/hooks")) {
			if err := remove(doc, group); err != nil {
				return err
			}
		}
	}
	return nil
}

// prune drops the events that removing our hooks emptied, and the hooks section
// if that leaves it empty. It runs after any append, so a hook moving between
// groups of one event leaves the event where it was.
func prune(doc *hujson.Value, removed []ourHook) error {
	if len(removed) == 0 {
		return nil
	}
	for _, h := range removed {
		ptr := pointer("hooks", h.event)
		if v := doc.Find(ptr); v != nil && isEmpty(v) {
			if err := remove(doc, ptr); err != nil {
				return err
			}
		}
	}
	if isEmpty(doc.Find("/hooks")) {
		return remove(doc, "/hooks")
	}
	return nil
}

// remove deletes the entry at ptr. Patch hands a comment trailing the entry
// before it on to the closing bracket, along with the removed entry's
// indentation, so the bracket gets its own indentation back.
func remove(doc *hujson.Value, ptr string) error {
	parent := ptr[:strings.LastIndexByte(ptr, '/')]
	before := slices.Clone(*closingExtra(doc.Find(parent)))
	if err := patch(doc, patchOp{Op: "remove", Path: ptr}); err != nil {
		return err
	}
	after := closingExtra(doc.Find(parent))
	i, j := bytes.LastIndexByte(before, '\n'), bytes.LastIndexByte(*after, '\n')
	if i >= 0 && j >= 0 {
		*after = append((*after)[:j:j], before[i:]...)
	}
	return nil
}

func closingExtra(v *hujson.Value) *hujson.Extra {
	switch c := v.Value.(type) {
	case *hujson.Object:
		return &c.AfterExtra
	case *hujson.Array:
		return &c.AfterExtra
	}
	return new(hujson.Extra)
}

type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

func patch(doc *hujson.Value, op patchOp) error {
	raw, err := marshalNoHTML([]patchOp{op})
	if err != nil {
		return err
	}
	return errors.Wrapf(doc.Patch(raw), "failed to %s %s", op.Op, op.Path)
}

// pointer builds an RFC 6901 JSON pointer from unescaped segments.
func pointer(segments ...string) string {
	var b strings.Builder
	for _, s := range segments {
		b.WriteString("/")
		b.WriteString(strings.NewReplacer("~", "~0", "/", "~1").Replace(s))
	}
	return b.String()
}

func isEmpty(v *hujson.Value) bool {
	if v == nil {
		return false
	}
	switch c := v.Value.(type) {
	case *hujson.Object:
		return len(c.Members) == 0
	case *hujson.Array:
		return len(c.Elements) == 0
	}
	return false
}

// insert appends value to the object or array at ptr, named name when it is an
// object, laid out like the entries already there. Patch moves a comment
// trailing the previous entry onto the new one, where we only add indentation.
func insert(doc *hujson.Value, ptr, name string, value any) error {
	container := doc.Find(ptr)
	lead, indent, err := entryLayout(doc, container)
	if err != nil {
		return err
	}
	var raw []byte
	if indent == nil {
		raw, err = marshalNoHTML(value)
	} else {
		raw, err = marshalIndent(value, *indent, indentUnit(doc))
	}
	if err != nil {
		return err
	}

	var colon hujson.Extra
	path := ptr + "/-"
	if obj, ok := container.Value.(*hujson.Object); ok {
		colon = hujson.Extra(" ")
		if n := len(obj.Members); n > 0 {
			colon = slices.Clone(obj.Members[n-1].Value.BeforeExtra)
		}
		path = ptr + pointer(name)
	}
	quoted, err := marshalNoHTML(path)
	if err != nil {
		return err
	}
	op := `[{"op": "add", "path": ` + string(quoted) + `, "value": ` + string(raw) + `}]`
	if err := doc.Patch([]byte(op)); err != nil {
		return errors.Wrapf(err, "failed to add %s", path)
	}

	var added *hujson.Value
	switch c := doc.Find(ptr).Value.(type) {
	case *hujson.Object:
		m := &c.Members[len(c.Members)-1]
		m.Value.BeforeExtra = colon
		added = &m.Name
	case *hujson.Array:
		added = &c.Elements[len(c.Elements)-1]
	}
	moved := added.BeforeExtra
	if bytes.HasSuffix(moved, []byte("\n")) {
		lead = bytes.TrimPrefix(lead, []byte("\n"))
	}
	added.BeforeExtra = append(slices.Clone(moved), lead...)
	return nil
}

// entryLayout returns the whitespace to put before a new entry in container,
// and the indent its lines continue at, or a nil indent when the container is
// laid out on one line and the entry should be too.
func entryLayout(doc, container *hujson.Value) (hujson.Extra, *string, error) {
	var last *hujson.Value
	var closing *hujson.Extra
	switch c := container.Value.(type) {
	case *hujson.Object:
		if n := len(c.Members); n > 0 {
			last = &c.Members[n-1].Name
		}
		closing = &c.AfterExtra
	case *hujson.Array:
		if n := len(c.Elements); n > 0 {
			last = &c.Elements[n-1]
		}
		closing = &c.AfterExtra
	default:
		return nil, nil, errors.New("can only insert into an object or array")
	}

	if last != nil {
		i := bytes.LastIndexByte(last.BeforeExtra, '\n')
		if i < 0 {
			return hujson.Extra(" "), nil, nil
		}
		indent := string(last.BeforeExtra[i+1:])
		return hujson.Extra("\n" + indent), &indent, nil
	}

	packed := doc.Pack()
	if !bytes.Contains(bytes.TrimSpace(packed), []byte("\n")) {
		return nil, nil, nil
	}
	doc.UpdateOffsets()
	packed = doc.Pack()
	lineStart := bytes.LastIndexByte(packed[:container.StartOffset], '\n') + 1
	outer := leadingIndent(packed[lineStart:])
	if len(bytes.TrimSpace(*closing)) == 0 {
		*closing = hujson.Extra("\n" + outer)
	}
	indent := outer + indentUnit(doc)
	return hujson.Extra("\n" + indent), &indent, nil
}

// indentUnit is the indent of the file's first top-level key, defaulting to two
// spaces.
func indentUnit(doc *hujson.Value) string {
	if obj, ok := doc.Value.(*hujson.Object); ok && len(obj.Members) > 0 {
		before := obj.Members[0].Name.BeforeExtra
		if i := bytes.LastIndexByte(before, '\n'); i >= 0 && i < len(before)-1 {
			return string(before[i+1:])
		}
	}
	return "  "
}

func leadingIndent(line []byte) string {
	return string(line[:len(line)-len(bytes.TrimLeft(line, " \t"))])
}

func marshalIndent(v any, prefix, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, indent)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
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

// readSettings returns the file as written, for editing, and its hooks section
// as plain JSON, for finding our entries. A missing or blank file reads as an
// empty object.
func readSettings(path string) (*hujson.Value, map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		data = []byte("{\n}\n")
	case err != nil:
		return nil, nil, errors.Wrapf(err, "failed to read %s", path)
	case len(bytes.TrimSpace(data)) == 0:
		data = []byte("{\n}\n")
	}
	doc, err := hujson.Parse(data)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "%s is not valid JSON", path)
	}
	std := doc.Clone()
	std.Standardize()
	top, err := decodeObject(std.Pack(), path)
	if err != nil {
		return nil, nil, err
	}
	events, err := decodeObject(top["hooks"], path+" hooks section")
	if err != nil {
		return nil, nil, err
	}
	return &doc, events, nil
}

// decodeObject requires a JSON object. A null or a scalar is an error rather
// than the nil map json.Unmarshal would otherwise hand back.
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

func writeSettings(path string, doc *hujson.Value) error {
	// Our hook was the file's only content, so don't leave an empty one behind.
	if isEmpty(doc) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Wrapf(err, "failed to remove %s", path)
		}
		return nil
	}
	return writeFileAtomic(path, doc.Pack())
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
