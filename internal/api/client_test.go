package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/auth"
)

// newTestClient points a Client at the given test server.
func newTestClient(server *httptest.Server) *Client {
	return &Client{
		host:   server.URL,
		tokens: staticToken("test-token"),
		http:   server.Client(),
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

// renewableToken is an OAuth-session-like source: the server can reject its
// token, and it can hand out a new one.
type renewableToken struct {
	current   string
	refreshes int
	err       error
}

func (r *renewableToken) Token(context.Context) (string, error) { return r.current, nil }

func (r *renewableToken) ForceRefresh(_ context.Context, rejected string) (string, error) {
	r.refreshes++
	if r.err != nil {
		return "", r.err
	}
	if rejected != r.current {
		return "", errors.Errorf("rejected %q is not the current token %q", rejected, r.current)
	}
	r.current = "token-2"
	return r.current, nil
}

// TestUnauthorizedRefreshesOnce covers a token the server revoked before it
// expired: the client renews it once and retries, and does not loop.
func TestUnauthorizedRefreshesOnce(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		seen = append(seen, token)
		if token != "token-2" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		_, _ = w.Write([]byte(`{"value":"ok"}`))
	}))
	defer srv.Close()

	tokens := &renewableToken{current: "token-1"}
	client := newTestClient(srv)
	client.tokens = tokens

	var out struct {
		Value string `json:"value"`
	}
	if err := client.getJSON(context.Background(), "/thing", nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Value != "ok" {
		t.Fatalf("value = %q, want ok", out.Value)
	}
	if len(seen) != 2 || seen[0] != "token-1" || seen[1] != "token-2" {
		t.Fatalf("requests carried %v, want the rejected token then the renewed one", seen)
	}
	if tokens.refreshes != 1 {
		t.Fatalf("refreshed %d times, want 1", tokens.refreshes)
	}
}

func TestUnauthorizedReportsHowToReauthenticate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","message":"token revoked"}`))
	}))
	defer srv.Close()

	t.Run("oauth session", func(t *testing.T) {
		client := newTestClient(srv)
		tokens := &renewableToken{current: "token-1"}
		client.tokens = tokens

		err := client.getJSON(context.Background(), "/thing", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "aviator login") {
			t.Fatalf("error = %v, want it to point at `aviator login`", err)
		}
		if tokens.refreshes != 1 {
			t.Fatalf("refreshed %d times, want exactly one retry", tokens.refreshes)
		}
	})

	t.Run("expired session", func(t *testing.T) {
		client := newTestClient(srv)
		client.tokens = &renewableToken{current: "token-1", err: auth.ErrSessionExpired}

		err := client.getJSON(context.Background(), "/thing", nil, nil)
		if !errors.Is(err, auth.ErrSessionExpired) {
			t.Fatalf("error = %v, want ErrSessionExpired", err)
		}
		if !strings.Contains(err.Error(), "aviator login") {
			t.Fatalf("error = %q, want it to say how to sign in again", err)
		}
	})

	// A static token cannot be renewed, so it must not be retried.
	t.Run("static token", func(t *testing.T) {
		err := newTestClient(srv).getJSON(context.Background(), "/thing", nil, nil)
		if err == nil || strings.Contains(err.Error(), "aviator login") {
			t.Fatalf("error = %v, want it to point at the configured token", err)
		}
	})
}

// A 403 means the token has no owner, so the fix is `aviator login` rather
// than a new token. Only a static credential can cause it, and a session that
// is already signed in shouldn't be told to sign in again.
func TestForbiddenPointsAStaticTokenAtLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden","message":"needs a user access token"}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).getJSON(context.Background(), "/thing", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "aviator login") {
		t.Fatalf("error = %v, want it to point at `aviator login`", err)
	}

	client := newTestClient(srv)
	client.tokens = &renewableToken{current: "token-1"}
	err = client.getJSON(context.Background(), "/thing", nil, nil)
	if err == nil || strings.Contains(err.Error(), "aviator login") {
		t.Fatalf("error = %v, want no sign-in hint for a session that is already signed in", err)
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
