package api

import (
	"context"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/auth"
	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/zalando/go-keyring"
)

// withConfig points the loaded configuration at a host/token for one test.
func withConfig(t *testing.T, host, token string) {
	t.Helper()
	previous := config.Av.Aviator
	config.Av.Aviator.APIHost = host
	config.Av.Aviator.APIToken = token
	t.Cleanup(func() { config.Av.Aviator = previous })
}

// storeSession saves an unexpired OAuth session for host.
func storeSession(t *testing.T, host, accessToken string) {
	t.Helper()
	session := &auth.Session{AccessToken: accessToken, Expiry: time.Now().Add(time.Hour)}
	if err := auth.DefaultStore().Save(host, session); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestResolveTokenSourcePrefersStaticToken(t *testing.T) {
	keyring.MockInit()
	host := "https://static.example.com"
	withConfig(t, host, "static-token")
	storeSession(t, host, "oauth-token")

	source, err := resolveTokenSource()
	if err != nil {
		t.Fatalf("resolveTokenSource: %v", err)
	}
	got, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != "static-token" {
		t.Fatalf("Token = %q, want the statically configured token", got)
	}
}

func TestResolveTokenSourceFallsBackToOAuthSession(t *testing.T) {
	keyring.MockInit()
	host := "https://session.example.com"
	withConfig(t, host, "")
	storeSession(t, host, "oauth-token")

	source, err := resolveTokenSource()
	if err != nil {
		t.Fatalf("resolveTokenSource: %v", err)
	}
	got, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != "oauth-token" {
		t.Fatalf("Token = %q, want the stored OAuth token", got)
	}
}

func TestResolveTokenSourceWithoutCredentials(t *testing.T) {
	keyring.MockInit()
	withConfig(t, "https://empty.example.com", "")

	if _, err := resolveTokenSource(); !errors.Is(err, ErrNoAPIToken) {
		t.Fatalf("resolveTokenSource error = %v, want ErrNoAPIToken", err)
	}
}
