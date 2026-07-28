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

func TestResolveTokenSourcePrefersStaticToken(t *testing.T) {
	keyring.MockInit()
	host := "https://static.example.com"
	withConfig(t, host, "static-token")

	if err := auth.DefaultStore().Save(host, &auth.Session{
		AccessToken: "oauth-token",
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	source, err := resolveTokenSource()
	if err != nil {
		t.Fatalf("resolveTokenSource: %v", err)
	}
	token, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "static-token" {
		t.Fatalf("Token = %q, want the statically configured token", token)
	}
}

func TestResolveTokenSourceFallsBackToOAuthSession(t *testing.T) {
	keyring.MockInit()
	host := "https://session.example.com"
	withConfig(t, host, "")

	if err := auth.DefaultStore().Save(host, &auth.Session{
		AccessToken: "oauth-token",
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	source, err := resolveTokenSource()
	if err != nil {
		t.Fatalf("resolveTokenSource: %v", err)
	}
	token, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "oauth-token" {
		t.Fatalf("Token = %q, want the stored OAuth token", token)
	}
}

func TestResolveTokenSourceWithoutCredentials(t *testing.T) {
	keyring.MockInit()
	withConfig(t, "https://empty.example.com", "")

	if _, err := resolveTokenSource(); !errors.Is(err, ErrNoAPIToken) {
		t.Fatalf("resolveTokenSource error = %v, want ErrNoAPIToken", err)
	}
}
