package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
			"token_endpoint":"https://app.aviator.co/oauth/token",
			"registration_endpoint":"https://app.aviator.co/oauth/register"
		}`)
	}))
	defer server.Close()

	meta, err := Discover(context.Background(), server.Client(), server.URL+"/")
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

func TestDiscoverWithoutEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"issuer":"https://app.aviator.co"}`)
	}))
	defer server.Close()

	if _, err := Discover(context.Background(), server.Client(), server.URL); err == nil {
		t.Fatal("expected an error when no endpoints are advertised")
	}
}

func TestRegisterClient(t *testing.T) {
	var got registerRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"client_id":"av_mcp_generated","token_endpoint_auth_method":"none"}`)
	}))
	defer server.Close()

	meta := &Metadata{RegistrationEndpoint: server.URL + "/oauth/register"}
	reg, err := RegisterClient(context.Background(), server.Client(), meta, redirectURIs)
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if reg.ClientID != "av_mcp_generated" {
		t.Errorf("ClientID = %q", reg.ClientID)
	}
	if len(got.RedirectURIs) != len(redirectURIs) {
		t.Fatalf("sent %d redirect URIs, want %d", len(got.RedirectURIs), len(redirectURIs))
	}
	if !reg.matches(redirectURIs) {
		t.Error("registration does not record the redirect URIs it was created with")
	}
	for _, uri := range got.RedirectURIs {
		// The server matches redirect URIs by exact string and rejects
		// non-loopback HTTP hosts, so these must stay 127.0.0.1 literals.
		if !strings.HasPrefix(uri, "http://127.0.0.1:") {
			t.Errorf("redirect URI %q is not a 127.0.0.1 loopback URI", uri)
		}
	}
}

func TestRegisterClientRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"rate_limit_exceeded"}`)
	}))
	defer server.Close()

	meta := &Metadata{RegistrationEndpoint: server.URL + "/oauth/register"}
	_, err := RegisterClient(context.Background(), server.Client(), meta, redirectURIs)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "rate_limit_exceeded") {
		t.Fatalf("error = %v, want it to mention rate_limit_exceeded", err)
	}
}
