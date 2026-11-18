package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/spf13/pflag"
)

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
			"github unavailable",
			&api.Error{Status: http.StatusServiceUnavailable, Code: "stack-check-unavailable", Message: "Retry in a minute."},
			0,
			[]string{"GitHub couldn't check the stack of r/42", "Retry in a minute."},
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
	for _, c := range []interface{ IsAvailableCommand() bool }{verifyAddCmd, verifyReadyCmd, verifyRemoveCmd} {
		if c.IsAvailableCommand() {
			t.Errorf("%v is listed in help", c)
		}
	}
	if f := verifyCmd.Flags().Lookup("draft"); f == nil || !f.Hidden {
		t.Errorf("--draft should be hidden")
	}
}

// add --new has no review yet, so its errors don't name one.
func TestStackedVerifyErrorWithoutReview(t *testing.T) {
	err := &api.Error{Status: http.StatusBadRequest, Code: "stack-invalid", Message: "#103 is closed."}
	if got := stackedVerifyError(err, 0, 0).Error(); !strings.Contains(got, "the stack isn't valid: #103 is closed.") {
		t.Errorf("error = %q", got)
	}
}

func TestValidPRNumbers(t *testing.T) {
	if got, err := validPRNumbers([]int{103, 104}); err != nil || len(got) != 2 {
		t.Errorf("got %v, %v", got, err)
	}
	for _, tc := range []struct {
		name string
		prs  []int
		want string
	}{
		{"none", nil, "--pr is required"},
		{"zero", []int{0}, "invalid --pr 0"},
		{"repeat", []int{103, 103}, "--pr 103 is given twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validPRNumbers(tc.prs); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func runRemove(t *testing.T) (*[]string, error) {
	t.Helper()
	got := useTestServer(t, `{"runbook_number":42,"version":4,"state":"active",`+
		`"members":[{"pr_number":101,"position":1,"head_sha":null}]}`)
	t.Cleanup(func() {
		verifyRemoveFlags = struct {
			PRs          []int
			Criteria     []string
			CriteriaFile string
			BaseVersion  int
			Intent       string
			JSON         bool
		}{}
		verifyRemoveCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	})
	verifyRemoveCmd.SetContext(context.Background())
	if err := verifyRemoveCmd.ParseFlags([]string{
		"--pr", "103", "--pr", "104", "--criteria", "one", "--base-version", "3", "--json",
	}); err != nil {
		t.Fatal(err)
	}
	return got, verifyRemoveCmd.RunE(verifyRemoveCmd, []string{"r/42"})
}

// The user agrees to the split in the agent's conversation, so remove asks nothing.
func TestRemoveSendsTheRequest(t *testing.T) {
	got, err := runRemove(t)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `POST /api/v1/verify/42/prs/remove {"pr_numbers":[103,104],"criteria":["one"],"base_version":3}`
	if len(*got) != 1 || (*got)[0] != want {
		t.Errorf("requests = %v, want %s", *got, want)
	}
}

func resetAddFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		verifyAddFlags = struct {
			PRs          []int
			Criteria     []string
			CriteriaFile string
			BaseVersion  int
			Intent       string
			New          bool
			From         string
			Repo         string
			JSON         bool
		}{}
		verifyAddCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	})
}

func TestAddNewSendsTheStack(t *testing.T) {
	got := useTestServer(t, `{"runbook_number":57,"url":"https://app.aviator.co/r/57","state":"active",`+
		`"members":[{"pr_number":103,"position":1,"head_sha":null}]}`)
	resetAddFlags(t)
	runCommand(t, verifyAddCmd, "--new", "--repo", "acme/web", "--pr", "103", "--pr", "104",
		"--intent", "top half", "--criteria", "two", "--from", "r/42", "--json")
	want := `POST /api/v1/verify/stack {"repository":{"org":"acme","name":"web"},"pr_numbers":[103,104],` +
		`"intent":"top half","criteria":["two"],"from_runbook_number":42}`
	if len(*got) != 1 || (*got)[0] != want {
		t.Errorf("requests = %v, want %s", *got, want)
	}
}

// One PR keeps pr_number, which servers without pr_numbers still read.
func TestAddSendsOneOrSeveralPRs(t *testing.T) {
	for _, tc := range []struct {
		name string
		prs  []string
		want string
	}{
		{"one", []string{"--pr", "102"}, `"pr_number":102,`},
		{"several", []string{"--pr", "103", "--pr", "102"}, `"pr_numbers":[103,102],`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := useTestServer(t, `{"runbook_number":42,"url":"https://app.aviator.co/r/42","version":4,`+
				`"state":"draft","members":[{"pr_number":101,"position":1,"head_sha":null}]}`)
			resetAddFlags(t)
			args := append([]string{"r/42"}, tc.prs...)
			runCommand(t, verifyAddCmd, append(args, "--criteria", "one", "--base-version", "3", "--json")...)
			want := `POST /api/v1/verify/42/prs {` + tc.want + `"criteria":["one"],"base_version":3}`
			if len(*got) != 1 || (*got)[0] != want {
				t.Errorf("requests = %v, want %s", *got, want)
			}
		})
	}
}

func TestAddFlagCombinationsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		pos  []string
		want string
	}{
		{
			"new with id",
			[]string{"--new", "--repo", "a/b", "--pr", "1", "--intent", "x", "--criteria", "c"},
			[]string{"r/42"},
			"takes no <id>",
		},
		{"new with base version", []string{
			"--new", "--repo", "a/b", "--pr", "1", "--intent", "x", "--criteria", "c",
			"--base-version", "3",
		}, nil, "--base-version doesn't apply with --new"},
		{"new without repo", []string{"--new", "--pr", "1", "--intent", "x", "--criteria", "c"}, nil, "--repo is required"},
		{"new without intent", []string{"--new", "--repo", "a/b", "--pr", "1", "--criteria", "c"}, nil, "--intent is required"},
		{
			"from without new",
			[]string{"--pr", "1", "--criteria", "c", "--base-version", "3", "--from", "r/41"},
			[]string{"r/42"},
			"--from only applies with --new",
		},
		{
			"repeated pr",
			[]string{"--pr", "1", "--pr", "1", "--criteria", "c", "--base-version", "3"},
			[]string{"r/42"},
			"--pr 1 is given twice",
		},
		{"no id without new", []string{"--pr", "1", "--criteria", "c", "--base-version", "3"}, nil, "needs the review id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := useTestServer(t, `{}`)
			resetAddFlags(t)
			verifyAddCmd.SetContext(context.Background())
			if err := verifyAddCmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			err := verifyAddCmd.RunE(verifyAddCmd, tc.pos)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
			if len(*got) != 0 {
				t.Errorf("requests = %v, want none", *got)
			}
		})
	}
}
