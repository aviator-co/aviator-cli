package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscover(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != metadataPath {
			t.Errorf("path = %q, want %q", r.URL.Path, metadataPath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"issuer":"https://app.aviator.co",
			"authorization_endpoint":"https://app.aviator.co/oauth/authorize",
			"token_endpoint":"https://app.aviator.co/oauth/token"
		}`)
	}))
	defer server.Close()

	meta, err := discover(context.Background(), server.Client(), server.URL+"/")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// The authorization endpoint lives on the web app host, not the API host.
	if meta.AuthorizationEndpoint != "https://app.aviator.co/oauth/authorize" {
		t.Errorf("AuthorizationEndpoint = %q", meta.AuthorizationEndpoint)
	}
	if meta.TokenEndpoint != "https://app.aviator.co/oauth/token" {
		t.Errorf("TokenEndpoint = %q", meta.TokenEndpoint)
	}
}

// jsonServer replies to every request with body.
func jsonServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestDiscoverRejectsUnusableMetadata pins the transport floor and the presence
// of the endpoints the login flow needs.
func TestDiscoverRejectsUnusableMetadata(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"no endpoints", `{"issuer":"https://app.aviator.co"}`},
		{"no token endpoint", `{"issuer":"https://app.aviator.co",
			"authorization_endpoint":"https://app.aviator.co/oauth/authorize"}`},
		{"plain http endpoints", `{"issuer":"http://app.aviator.co",
			"authorization_endpoint":"http://app.aviator.co/oauth/authorize",
			"token_endpoint":"http://app.aviator.co/oauth/token"}`},
		{"plain http token endpoint", `{"issuer":"https://app.aviator.co",
			"authorization_endpoint":"https://app.aviator.co/oauth/authorize",
			"token_endpoint":"http://app.aviator.co/oauth/token"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, tt.body)
			if _, err := discover(context.Background(), server.Client(), server.URL); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	if _, err := discover(context.Background(), nil, "http://api.aviator.co"); err == nil {
		t.Fatal("expected an error for a plain-HTTP API host")
	}
}
