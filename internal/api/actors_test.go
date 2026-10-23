//go:build experimental

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListActors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/verify/actors" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"actors":[{"id":12,"name":"claude-code","role":"coding_agent"}]}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(srv).ListActors(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Actors) != 1 || resp.Actors[0] != (Actor{ID: 12, Name: "claude-code", Role: "coding_agent"}) {
		t.Errorf("actors = %+v", resp.Actors)
	}
}
