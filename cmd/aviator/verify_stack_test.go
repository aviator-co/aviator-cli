package main

import (
	"bytes"
	"context"
	"io"
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

var regroupTestGroups = []api.RegroupGroup{
	{PRNumbers: []int{101, 102}, Intent: "Add the API", Criteria: []string{"a", "b"}, DecisionIDs: []int{3}},
	{PRNumbers: []int{103}, Intent: "Add the UI", Criteria: []string{"c"}, DecisionIDs: []int{}},
}

func TestConfirmRegroup(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		want         bool
	}{
		{"y", "y\n", true},
		{"yes any case", " YES \n", true},
		{"no", "n\n", false},
		{"empty", "\n", false},
		{"eof", "", false},
		{"other", "sure\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got, err := confirmRegroup(strings.NewReader(tc.answer), &out, true, 42, regroupTestGroups)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("confirmed = %v, want %v", got, tc.want)
			}
			if cancelled := strings.Contains(out.String(), "Cancelled — r/42 is unchanged."); cancelled == tc.want {
				t.Errorf("cancel line shown = %v, want %v\n%s", cancelled, !tc.want, out.String())
			}
		})
	}
}

func TestConfirmRegroupPrintsSplit(t *testing.T) {
	var out bytes.Buffer
	if _, err := confirmRegroup(strings.NewReader("n\n"), &out, true, 42, regroupTestGroups); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"archives r/42, cancels any verification running on it",
		"1. #101, #102 — Add the API",
		"2 criteria, 1 decisions",
		"2. #103 — Add the UI",
		"Split r/42 into 2 sessions? [y/N] ",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q\n%s", want, out.String())
		}
	}
}

// Without a terminal nobody can say yes, so regroup refuses rather than read.
func TestConfirmRegroupNonInteractive(t *testing.T) {
	var out bytes.Buffer
	ok, err := confirmRegroup(strings.NewReader("y\n"), &out, false, 42, regroupTestGroups)
	if ok || err == nil || !strings.Contains(err.Error(), "needs an interactive terminal") {
		t.Errorf("ok = %v, err = %v", ok, err)
	}
	if out.Len() != 0 {
		t.Errorf("unexpected output:\n%s", out.String())
	}
}

func runRegroup(t *testing.T, answer string, terminal bool) (*[]string, error) {
	t.Helper()
	got := useTestServer(t, `{"sessions":[{"runbook_number":43,"state":"active"},{"runbook_number":44,"state":"active"}]}`)
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return terminal }
	t.Cleanup(func() {
		stdinIsTerminal = orig
		verifyRegroupFlags.Groups = nil
		verifyRegroupCmd.SetIn(nil)
		verifyRegroupCmd.SetErr(nil)
	})
	a := writeGroupFile(t, `{"pr_numbers":[101],"intent":"a","criteria":["one"]}`)
	b := writeGroupFile(t, `{"pr_numbers":[102],"intent":"b","criteria":["two"]}`)
	verifyRegroupCmd.SetContext(context.Background())
	verifyRegroupCmd.SetIn(strings.NewReader(answer))
	verifyRegroupCmd.SetErr(io.Discard)
	if err := verifyRegroupCmd.ParseFlags([]string{"--group", a, "--group", b}); err != nil {
		t.Fatal(err)
	}
	return got, verifyRegroupCmd.RunE(verifyRegroupCmd, []string{"r/42"})
}

func TestRegroupCommandConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		terminal     bool
		wantCalls    int
		wantErr      string
	}{
		{"yes splits", "y\n", true, 1, ""},
		{"no makes no call", "n\n", true, 0, ""},
		{"no terminal makes no call", "y\n", false, 0, "needs an interactive terminal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := runRegroup(t, tc.answer, tc.terminal)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if len(*got) != tc.wantCalls {
				t.Fatalf("requests = %v, want %d", *got, tc.wantCalls)
			}
			if tc.wantCalls == 1 && !strings.HasPrefix((*got)[0], "POST /api/v1/verify/42/regroup ") {
				t.Errorf("request = %s", (*got)[0])
			}
		})
	}
}
