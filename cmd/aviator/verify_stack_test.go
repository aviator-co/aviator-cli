package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
)

func writeGroupFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "group.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadRegroupGroup(t *testing.T) {
	g, err := readRegroupGroup(writeGroupFile(t,
		`{"pr_numbers":[101,102],"intent":" split ","criteria":["one"],"decision_ids":[3]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.PRNumbers) != 2 || g.Intent != "split" || g.Criteria[0] != "one" || g.DecisionIDs[0] != 3 {
		t.Errorf("group = %+v", g)
	}
}

// A group with no decisions still sends [] so the server never sees null.
func TestReadRegroupGroupNoDecisions(t *testing.T) {
	g, err := readRegroupGroup(writeGroupFile(t, `{"pr_numbers":[101],"intent":"a","criteria":["one"]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.DecisionIDs == nil || len(g.DecisionIDs) != 0 {
		t.Errorf("decision_ids = %v, want []", g.DecisionIDs)
	}
}

func TestReadRegroupGroupRefuses(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"unknown key", `{"prs":[101],"intent":"a","criteria":["one"]}`, "unknown field"},
		{"no prs", `{"intent":"a","criteria":["one"]}`, "pr_numbers is required"},
		{"blank intent", `{"pr_numbers":[1],"intent":" ","criteria":["one"]}`, "intent is required"},
		{"no criteria", `{"pr_numbers":[1],"intent":"a"}`, "criteria is required"},
		{"not json", `prs: [1]`, "invalid group file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readRegroupGroup(writeGroupFile(t, tc.content))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestStackedVerifyError(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         *api.Error
		baseVersion int
		want        []string
	}{
		{
			"draft",
			&api.Error{Status: http.StatusConflict, Code: "session-draft", Message: "draft"},
			0,
			[]string{"r/42 is a draft", "aviator verify ready r/42"},
		},
		{
			"stale on add",
			&api.Error{Status: http.StatusConflict, Code: "stale-runbook-version", Message: "now at version 5"},
			3,
			[]string{"changed since version 3", "aviator show r/42", "now at version 5"},
		},
		{
			"stack invalid",
			&api.Error{Status: http.StatusBadRequest, Code: "stack-invalid", Message: "#103's base is not a member"},
			0,
			[]string{"isn't valid", "#103's base is not a member"},
		},
		{
			"other",
			&api.Error{Status: http.StatusNotFound, Code: "not-found", Message: "r/42 not found."},
			0,
			[]string{"(404): r/42 not found."},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := stackedVerifyError(tc.err, 42, tc.baseVersion).Error()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("error %q missing %q", got, want)
				}
			}
		})
	}
}

func TestStackedVerifyErrorPassesThroughNonAPI(t *testing.T) {
	err := errors.New("request failed")
	if got := stackedVerifyError(err, 42, 0); got != err {
		t.Errorf("got %v, want the original error", got)
	}
}

func TestFormatMembers(t *testing.T) {
	if got := formatMembers(nil); got != "" {
		t.Errorf("no members = %q, want empty", got)
	}
	got := formatMembers([]api.SessionMember{
		{PRNumber: 101, Position: 1, HeadSHA: ptr("abcdef1234")},
		{PRNumber: 102, Position: 2},
	})
	for _, want := range []string{"Members:", "1. #101 abcdef1", "2. #102\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n%s", want, got)
		}
	}
}

func TestShowStackedSession(t *testing.T) {
	detail := &api.RunbookDetail{
		RunbookNumber: 42,
		URL:           "https://app.aviator.co/r/42",
		State:         ptr("draft"),
		Members:       []api.SessionMember{{PRNumber: 101, Position: 1, HeadSHA: ptr("abcdef1234")}},
	}
	out := formatRunbookDetail(detail, nil, false)
	for _, want := range []string{"State: draft", "1. #101 abcdef1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	got := decodeJSON(t, newShowJSON(detail, nil, false))
	if got["state"] != "draft" {
		t.Errorf("state = %v", got["state"])
	}
	if m := got["members"].([]any); len(m) != 1 || m[0].(map[string]any)["pr_number"] != float64(101) {
		t.Errorf("members = %v", m)
	}
}

// Unstacked sessions and older backends carry neither field.
func TestShowUnstackedSession(t *testing.T) {
	detail := &api.RunbookDetail{RunbookNumber: 7, URL: "https://app.aviator.co/r/7"}
	out := formatRunbookDetail(detail, nil, false)
	if strings.Contains(out, "State:") || strings.Contains(out, "Members:") {
		t.Errorf("unexpected stacked lines:\n%s", out)
	}
	got := decodeJSON(t, newShowJSON(detail, nil, false))
	if got["state"] != nil {
		t.Errorf("state = %v, want null", got["state"])
	}
	if m, ok := got["members"].([]any); !ok || len(m) != 0 {
		t.Errorf("members = %v, want []", got["members"])
	}
}

// Stacked Verify is allowlisted, so its commands and flag stay out of help.
func TestStackedVerifyHidden(t *testing.T) {
	for _, c := range []interface{ IsAvailableCommand() bool }{verifyAddCmd, verifyReadyCmd, verifyRegroupCmd} {
		if c.IsAvailableCommand() {
			t.Errorf("%v is listed in help", c)
		}
	}
	if f := verifyCmd.Flags().Lookup("draft"); f == nil || !f.Hidden {
		t.Errorf("--draft should be hidden")
	}
}
