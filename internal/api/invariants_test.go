package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const invariantJSON = `{"id":7,"title":"T","body":"B","category":"security","source":"manual",` +
	`"status":"active","enabled":true,"reason":null,"source_refs":[],` +
	`"repositories":[{"org":"acme","name":"web"}],` +
	`"conditions":[{"type":"language","value":"go","negate":true}],` +
	`"created_at":"2026-09-06T00:00:00-07:00","modified_at":"2026-09-06T00:00:00-07:00"}`

func TestListInvariantsQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/invariants" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"invariants":[` + invariantJSON + `],"page":2,"per_page":5,"has_more":true}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).ListInvariants(context.Background(), ListInvariantsQuery{
		Repo:    &Repository{Org: "acme", Name: "web"},
		Status:  "active",
		IDs:     []int{7, 12},
		Sources: []string{"manual", "ai_generated_docs"},
		Page:    2,
		PerPage: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotQuery != "ids=7%2C12&org=acme&page=2&per_page=5&repo=web&source=manual%2Cai_generated_docs&status=active" {
		t.Errorf("query = %q", gotQuery)
	}
	if !resp.HasMore || resp.Page != 2 || len(resp.Invariants) != 1 {
		t.Errorf("response = %+v", resp)
	}
	inv := resp.Invariants[0]
	if inv.ID != 7 || inv.Repositories[0].Name != "web" || !inv.Conditions[0].Negate {
		t.Errorf("invariant = %+v", inv)
	}
}

// TestListInvariantsDefaultQuery covers the zero query: nothing is sent, so
// the server's own defaults and "every status" apply.
func TestListInvariantsDefaultQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"invariants":[],"page":1,"per_page":20,"has_more":false}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).ListInvariants(context.Background(), ListInvariantsQuery{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListInvariantCategories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/invariants/categories" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"categories":[{"slug":"security","name":"Security","description":"d"}]}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).ListInvariantCategories(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Categories) != 1 || resp.Categories[0].Slug != "security" {
		t.Errorf("categories = %+v", resp.Categories)
	}
}

func TestCreateInvariant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/invariants" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		// An omitted Enabled and no repositories must stay absent so the
		// server's defaults (enabled, account-scoped) apply.
		want := `{"title":"T","body":"B","category":"security","conditions":[{"type":"language","value":"go","negate":false}]}`
		if string(data) != want {
			t.Errorf("body = %s\n want %s", data, want)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(invariantJSON))
	}))
	defer srv.Close()

	inv, err := newTestClient(srv).CreateInvariant(context.Background(), CreateInvariantRequest{
		Title:      "T",
		Body:       "B",
		Category:   "security",
		Conditions: []InvariantCondition{{Type: "language", Value: "go"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.ID != 7 {
		t.Errorf("id = %d", inv.ID)
	}
}

// TestUpdateInvariantBody pins the replace-or-leave-alone encoding: absent
// fields are omitted, and pointers to empty slices are sent as [] so the
// server clears the set.
func TestUpdateInvariantBody(t *testing.T) {
	var got map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/invariants/7" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(invariantJSON))
	}))
	defer srv.Close()

	title := "New"
	enabled := false
	_, err := newTestClient(srv).UpdateInvariant(context.Background(), 7, UpdateInvariantRequest{
		Title:        &title,
		Repositories: &[]Repository{},
		Enabled:      &enabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got["title"]) != `"New"` || string(got["repositories"]) != `[]` || string(got["enabled"]) != `false` {
		t.Errorf("body = %v", got)
	}
	for _, absent := range []string{"body", "category", "conditions"} {
		if _, ok := got[absent]; ok {
			t.Errorf("%s should be omitted, body = %v", absent, got)
		}
	}
}

func TestDeleteInvariant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/invariants/7" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"deleted_invariant_id":7}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).DeleteInvariant(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.DeletedInvariantID != 7 {
		t.Errorf("deleted_invariant_id = %d", resp.DeletedInvariantID)
	}
}

func TestSetInvariantStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/invariants/status" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		if want := `{"invariant_ids":[7,8],"status":"rejected"}`; string(data) != want {
			t.Errorf("body = %s, want %s", data, want)
		}
		_, _ = w.Write([]byte(`{"invariants":[` + invariantJSON + `]}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).SetInvariantStatus(context.Background(), SetInvariantStatusRequest{
		InvariantIDs: []int{7, 8},
		Status:       "rejected",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Invariants) != 1 {
		t.Errorf("invariants = %+v", resp.Invariants)
	}
}

// TestSetInvariantStatusForeignID mirrors the backend refusing the whole batch
// when one id is not under the account.
func TestSetInvariantStatusForeignID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not-found","message":"Baseline invariants not found under this account: [999]"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).SetInvariantStatus(context.Background(), SetInvariantStatusRequest{
		InvariantIDs: []int{7, 999},
		Status:       "active",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "404") || !strings.Contains(got, "[999]") {
		t.Errorf("error = %q", got)
	}
}

// TestListInvariantsOwnerQuery pins the owner filters' encoding, and that an
// invariant from a backend without owner teams decodes OwnerTeams as nil
// while an unowned one decodes it as empty.
func TestListInvariantsOwnerQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		owned := strings.Replace(invariantJSON, `"id":7`, `"id":8,"owner_teams":[]`, 1)
		_, _ = w.Write([]byte(`{"invariants":[` + invariantJSON + `,` + owned + `],"page":1,"per_page":20,"has_more":false}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).ListInvariants(context.Background(), ListInvariantsQuery{
		OwnerTeams: []string{"acme/payments", "acme/infra"},
		Unowned:    true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotQuery != "owner_team=acme%2Fpayments%2Cacme%2Finfra&unowned=true" {
		t.Errorf("query = %q", gotQuery)
	}
	if resp.Invariants[0].OwnerTeams != nil {
		t.Errorf("absent owner_teams should decode as nil, got %v", *resp.Invariants[0].OwnerTeams)
	}
	if teams := resp.Invariants[1].OwnerTeams; teams == nil || len(*teams) != 0 {
		t.Errorf("owner_teams [] should decode as empty, got %v", teams)
	}
}

func TestUpdateInvariantOwnerTeamsBody(t *testing.T) {
	var got map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(invariantJSON))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	teams := []OwnerTeam{{Org: "acme", Slug: "payments"}}
	if _, err := client.UpdateInvariant(context.Background(), 7, UpdateInvariantRequest{OwnerTeams: &teams}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := `[{"org":"acme","slug":"payments"}]`; string(got["owner_teams"]) != want {
		t.Errorf("owner_teams = %s, want %s", got["owner_teams"], want)
	}
	if _, err := client.UpdateInvariant(context.Background(), 7, UpdateInvariantRequest{OwnerTeams: &[]OwnerTeam{}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got["owner_teams"]) != `[]` {
		t.Errorf("owner_teams = %s, want []", got["owner_teams"])
	}
}

// TestSetInvariantOwnersDefault pins that falling back to the default sends
// [] rather than null, which the server would refuse.
func TestSetInvariantOwnersDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/invariants/owners" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		if want := `{"invariant_ids":[7,8],"owner_teams":[]}`; string(data) != want {
			t.Errorf("body = %s, want %s", data, want)
		}
		_, _ = w.Write([]byte(`{"invariants":[` + invariantJSON + `]}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).SetInvariantOwners(context.Background(), SetInvariantOwnersRequest{
		InvariantIDs: []int{7, 8},
		OwnerTeams:   []OwnerTeam{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Invariants) != 1 {
		t.Errorf("invariants = %+v", resp.Invariants)
	}
}

func TestListInvariantOwnerTeams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/invariants/owner-teams" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"owner_teams":[{"org":"acme","slug":"infra","name":"Infra",` +
			`"invariant_count":3,"is_default":true}],"unowned_count":2}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).ListInvariantOwnerTeams(context.Background(), ListInvariantOwnerTeamsQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := InvariantOwnerTeam{
		OwnerTeam:      OwnerTeam{Org: "acme", Slug: "infra", Name: "Infra"},
		InvariantCount: 3,
		IsDefault:      true,
	}
	if len(resp.OwnerTeams) != 1 || resp.OwnerTeams[0] != want || resp.UnownedCount != 2 {
		t.Errorf("response = %+v", resp)
	}
}
