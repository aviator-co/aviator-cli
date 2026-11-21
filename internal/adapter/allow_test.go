package adapter

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestReadOnlyCommand(t *testing.T) {
	allowed := []string{
		"aviator sessions --repo aviator-co/mergeit --branch my-branch --json",
		"aviator sessions --repo aviator-co/mergeit --pr 16033 --json",
		"aviator decisions list r/2096 --limit 100 --json",
		"aviator decisions list r/2096 --limit 100 --json 2>&1 | head -100",
		"aviator decisions show r/2096 42 --json | head -n 50",
		"cd /Users/x/dev/repo/.claude/worktrees/b && aviator decisions list r/2098 --json 2>&1 | head -150",
		"cd ~/dev/repo && aviator sessions --branch=feature/x --json",
	}
	for _, cmd := range allowed {
		if !isReadOnlyCommand(cmd) {
			t.Errorf("want allowed: %q", cmd)
		}
	}

	refused := []string{
		"aviator decisions apply r/1 --file batch.json --json",
		"aviator decisions",
		"aviator verify --intent x",
		"aviator edit r/1 --intent x",
		"aviator show r/1",
		"aviator sessions; rm -rf ~",
		"aviator sessions && rm -rf ~",
		"aviator sessions || rm -rf ~",
		"aviator sessions & rm -rf ~",
		"aviator sessions\nrm -rf ~",
		"aviator sessions --repo $(cat ~/.ssh/id_rsa)",
		"aviator sessions --repo `cat ~/.ssh/id_rsa`",
		"aviator sessions --repo ${HOME}",
		"aviator sessions > out.json",
		"aviator sessions | sh",
		"aviator sessions | head -5 | sh",
		"aviator sessions | head -5; rm -rf ~",
		"aviator sessions 'x'; rm -rf ~ 'y'",
		"cd /tmp && rm -rf ~",
		"cd /tmp; aviator sessions",
		"rm -rf ~ && aviator sessions",
		"FOO=1 aviator sessions",
		"xaviator sessions",
		"aviator sessionsx",
	}
	for _, cmd := range refused {
		if isReadOnlyCommand(cmd) {
			t.Errorf("want refused: %q", cmd)
		}
	}
}

func TestPreToolUseAllowsReadOnlyCommands(t *testing.T) {
	payload := commitPayload("aviator decisions list r/1 --json")
	var resp struct {
		HookSpecificOutput struct {
			HookEventName      string `json:"hookEventName"`
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(emitPre(t, payload)), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.HookSpecificOutput.HookEventName != "PreToolUse" || resp.HookSpecificOutput.PermissionDecision != "allow" {
		t.Errorf("got %+v, want a PreToolUse allow", resp.HookSpecificOutput)
	}

	var out bytes.Buffer
	if err := emitPreToolUse(strings.NewReader(payload), &out, false); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("without allowReads got %q, want no output", out.String())
	}
}
