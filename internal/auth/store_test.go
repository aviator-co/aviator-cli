package auth

import (
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/zalando/go-keyring"
)

func TestStoreSessionRoundTrip(t *testing.T) {
	keyring.MockInit()
	store := DefaultStore()
	host := "https://api.example.com"

	want := &Session{
		ClientID:     "av_mcp_abc",
		AccessToken:  "access",
		RefreshToken: "refresh",
		Expiry:       time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		TokenURL:     "https://app.example.com/oauth/token",
	}
	if err := store.Save(host, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load(host)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken ||
		got.ClientID != want.ClientID || got.TokenURL != want.TokenURL {
		t.Fatalf("Load = %+v, want %+v", got, want)
	}
	if !got.Expiry.Equal(want.Expiry) {
		t.Fatalf("Expiry = %v, want %v", got.Expiry, want.Expiry)
	}
}

func TestStoreTrailingSlashIsSameHost(t *testing.T) {
	keyring.MockInit()
	store := DefaultStore()

	if err := store.Save("https://slash.example.com/", &Session{AccessToken: "a"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.Load("https://slash.example.com"); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestStoreDeleteLeavesClientRegistration(t *testing.T) {
	keyring.MockInit()
	store := DefaultStore()
	host := "https://delete.example.com"

	if err := store.SaveClient(host, &ClientRegistration{
		ClientID: "av_mcp_keep", RedirectURIs: redirectURIs,
	}); err != nil {
		t.Fatalf("SaveClient: %v", err)
	}
	if err := store.Save(host, &Session{AccessToken: "a"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete(host); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := store.Load(host); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Load after Delete = %v, want ErrNoSession", err)
	}
	reg, err := store.LoadClient(host)
	if err != nil {
		t.Fatalf("LoadClient after Delete: %v", err)
	}
	if reg.ClientID != "av_mcp_keep" {
		t.Fatalf("ClientID = %q, want av_mcp_keep", reg.ClientID)
	}
}

func TestStoreDeleteWithoutSession(t *testing.T) {
	keyring.MockInit()
	if err := DefaultStore().Delete("https://empty.example.com"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Delete = %v, want ErrNoSession", err)
	}
}

func TestSessionValid(t *testing.T) {
	tests := []struct {
		name    string
		session *Session
		want    bool
	}{
		{"nil", nil, false},
		{"no access token", &Session{Expiry: time.Now().Add(time.Hour)}, false},
		{"fresh", &Session{AccessToken: "a", Expiry: time.Now().Add(time.Hour)}, true},
		{"expired", &Session{AccessToken: "a", Expiry: time.Now().Add(-time.Minute)}, false},
		{"about to expire", &Session{AccessToken: "a", Expiry: time.Now().Add(time.Second)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.session.valid(); got != tt.want {
				t.Fatalf("valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClientRegistrationMatches(t *testing.T) {
	reg := &ClientRegistration{ClientID: "id", RedirectURIs: redirectURIs}
	if !reg.matches(redirectURIs) {
		t.Fatal("matches() = false for the current redirect URIs")
	}
	if reg.matches(append([]string{"http://127.0.0.1:1/callback"}, redirectURIs...)) {
		t.Fatal("matches() = true for a different redirect URI set")
	}
	if (&ClientRegistration{RedirectURIs: redirectURIs}).matches(redirectURIs) {
		t.Fatal("matches() = true for an empty client ID")
	}
}
