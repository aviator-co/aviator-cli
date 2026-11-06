package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"emperror.dev/errors"
)

// stackServer answers one POST to path, checking the body it was sent.
func stackServer(t *testing.T, path, wantBody string, status int, resp string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != path {
			t.Errorf("path = %q, want %q", r.URL.Path, path)
		}
		body, _ := io.ReadAll(r.Body)
		if got := strings.TrimSpace(string(body)); got != wantBody {
			t.Errorf("body = %s, want %s", got, wantBody)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
}

func TestSubmitVerifySendsDraft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"draft":true`) {
			t.Errorf("body = %s, missing draft", body)
		}
		_, _ = w.Write([]byte(`{"runbook_number":42}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).SubmitVerify(context.Background(), SubmitVerifyRequest{
		Intent: "stack", AcceptanceCriteria: []string{"one"}, Draft: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Old callers must not start sending draft:false to backends that predate it.
func TestSubmitVerifyOmitsDraftByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "draft") {
			t.Errorf("body = %s, should not carry draft", body)
		}
		_, _ = w.Write([]byte(`{"runbook_number":42}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).SubmitVerify(context.Background(), SubmitVerifyRequest{Intent: "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddVerifyPR(t *testing.T) {
	srv := stackServer(t, "/api/v1/verify/42/prs",
		`{"pr_number":102,"criteria":["one","two"],"base_version":3}`, http.StatusOK,
		`{"runbook_number":42,"url":"https://app.aviator.co/r/42","version":4,"state":"draft",`+
			`"members":[{"pr_number":101,"position":1,"head_sha":"aaa"},{"pr_number":102,"position":2,"head_sha":null}]}`)
	defer srv.Close()

	resp, err := newTestClient(srv).AddVerifyPR(context.Background(), 42, AddVerifyPRRequest{
		PRNumber: 102, Criteria: []string{"one", "two"}, BaseVersion: 3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Version == nil || *resp.Version != 4 || resp.State == nil || *resp.State != "draft" {
		t.Errorf("resp = %+v", resp)
	}
	if len(resp.Members) != 2 || resp.Members[1].PRNumber != 102 || resp.Members[1].HeadSHA != nil {
		t.Errorf("members = %+v", resp.Members)
	}
}

func TestAddVerifyPRStaleVersion(t *testing.T) {
	srv := stackServer(t, "/api/v1/verify/42/prs",
		`{"pr_number":102,"criteria":["one"],"base_version":3}`, http.StatusConflict,
		`{"error":"stale-runbook-version","message":"r/42 is currently at version 5"}`)
	defer srv.Close()

	_, err := newTestClient(srv).AddVerifyPR(context.Background(), 42, AddVerifyPRRequest{
		PRNumber: 102, Criteria: []string{"one"}, BaseVersion: 3,
	})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || apiErr.Code != "stale-runbook-version" {
		t.Fatalf("err = %v", err)
	}
}

func TestConfirmVerify(t *testing.T) {
	srv := stackServer(t, "/api/v1/verify/42/confirm", `{"run":true}`, http.StatusOK,
		`{"runbook_number":42,"state":"active","run_id":900,"run_status":"pending"}`)
	defer srv.Close()

	resp, err := newTestClient(srv).ConfirmVerify(context.Background(), 42, ConfirmVerifyRequest{Run: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RunID == nil || *resp.RunID != 900 || resp.RunStatus != "pending" || *resp.State != "active" {
		t.Errorf("resp = %+v", resp)
	}
}

// --no-run must send run:false explicitly rather than dropping the key.
func TestConfirmVerifyNoRun(t *testing.T) {
	srv := stackServer(t, "/api/v1/verify/42/confirm", `{"run":false}`, http.StatusOK,
		`{"runbook_number":42,"state":"active"}`)
	defer srv.Close()

	resp, err := newTestClient(srv).ConfirmVerify(context.Background(), 42, ConfirmVerifyRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RunID != nil {
		t.Errorf("run_id = %v, want nil", *resp.RunID)
	}
}

func TestRegroupVerify(t *testing.T) {
	srv := stackServer(t, "/api/v1/verify/42/regroup",
		`{"groups":[{"pr_numbers":[101],"intent":"a","criteria":["one"],"decision_ids":[]},`+
			`{"pr_numbers":[102,103],"intent":"b","criteria":["two"],"decision_ids":[7]}]}`,
		http.StatusOK,
		`{"sessions":[{"runbook_number":57,"state":"active"},{"runbook_number":58,"state":"active"}]}`)
	defer srv.Close()

	resp, err := newTestClient(srv).RegroupVerify(context.Background(), 42, RegroupVerifyRequest{
		Groups: []RegroupGroup{
			{PRNumbers: []int{101}, Intent: "a", Criteria: []string{"one"}, DecisionIDs: []int{}},
			{PRNumbers: []int{102, 103}, Intent: "b", Criteria: []string{"two"}, DecisionIDs: []int{7}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Sessions) != 2 || resp.Sessions[1].RunbookNumber != 58 {
		t.Errorf("sessions = %+v", resp.Sessions)
	}
}

func TestGetRunbookDetailStackedFields(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		wantState   string
		wantMembers int
	}{
		{"absent", `{"runbook_number":42}`, "", 0},
		{"stacked", `{"runbook_number":42,"state":"draft","members":[{"pr_number":101,"position":1,"head_sha":"abc"}]}`, "draft", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			d, err := newTestClient(srv).GetRunbookDetail(context.Background(), 42, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			state := ""
			if d.State != nil {
				state = *d.State
			}
			if state != tc.wantState || len(d.Members) != tc.wantMembers {
				t.Errorf("state = %q, members = %+v", state, d.Members)
			}
		})
	}
}
