package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newTestClient points a Client at the given test server.
func newTestClient(server *httptest.Server) *Client {
	return &Client{
		host:  server.URL,
		token: "test-token",
		http:  server.Client(),
	}
}

func TestGetJSONHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("fields"); got != "a,b" {
			t.Errorf("fields query = %q, want a,b", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"value":"ok"}`))
	}))
	defer srv.Close()

	var out struct {
		Value string `json:"value"`
	}
	err := newTestClient(srv).getJSON(
		context.Background(), "/thing", url.Values{"fields": {"a,b"}}, &out,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Value != "ok" {
		t.Fatalf("value = %q, want ok", out.Value)
	}
}

func TestGetJSONErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found","message":"runbook missing"}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).getJSON(context.Background(), "/thing", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "runbook missing") ||
		!strings.Contains(got, "404") {
		t.Fatalf("error = %q, want to mention message and status", got)
	}
}
