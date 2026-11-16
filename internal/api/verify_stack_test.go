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

func TestMarkVerifyReady(t *testing.T) {
	srv := stackServer(t, "/api/v1/verify/42/ready", `{}`, http.StatusOK,
		`{"runbook_number":42,"state":"active"}`)
	defer srv.Close()

	resp, err := newTestClient(srv).MarkVerifyReady(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *resp.State != "active" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestRemoveVerifyPRs(t *testing.T) {
	srv := stackServer(t, "/api/v1/verify/42/prs/remove",
		`{"pr_numbers":[103,104],"criteria":["one"],"base_version":3,"intent":"bottom half"}`, http.StatusOK,
		`{"runbook_number":42,"version":4,"state":"active","members":[{"pr_number":101,"position":1,"head_sha":"aaa"}]}`)
	defer srv.Close()

	resp, err := newTestClient(srv).RemoveVerifyPRs(context.Background(), 42, RemoveVerifyPRsRequest{
		PRNumbers: []int{103, 104}, Criteria: []string{"one"}, BaseVersion: 3, Intent: "bottom half",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *resp.Version != 4 || len(resp.Members) != 1 || resp.Members[0].PRNumber != 101 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestNewStackedVerify(t *testing.T) {
	from := 42
	for _, tc := range []struct {
		name     string
		from     *int
		wantBody string
	}{
		{"linked", &from, `{"repository":{"org":"acme","name":"web"},"pr_numbers":[103,104],"intent":"top half",` +
			`"criteria":["two"],"from_runbook_number":42}`},
		{"unlinked", nil, `{"repository":{"org":"acme","name":"web"},"pr_numbers":[103,104],"intent":"top half",` +
			`"criteria":["two"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := stackServer(t, "/api/v1/verify/stack", tc.wantBody, http.StatusOK,
				`{"runbook_number":57,"state":"active","members":[{"pr_number":103,"position":1,"head_sha":null}]}`)
			defer srv.Close()

			resp, err := newTestClient(srv).NewStackedVerify(context.Background(), NewStackedVerifyRequest{
				Repository: Repository{Org: "acme", Name: "web"}, PRNumbers: []int{103, 104},
				Intent: "top half", Criteria: []string{"two"}, FromRunbookNumber: tc.from,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.RunbookNumber != 57 || *resp.State != "active" {
				t.Errorf("resp = %+v", resp)
			}
		})
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
