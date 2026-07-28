package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/zalando/go-keyring"
)

// useTempLockDir keeps the refresh lock out of the real user cache directory.
func useTempLockDir(t *testing.T) {
	t.Helper()
	lockDirOverride = t.TempDir()
	t.Cleanup(func() { lockDirOverride = "" })
}

func TestTokenSourceReturnsValidTokenWithoutRefreshing(t *testing.T) {
	keyring.MockInit()
	useTempLockDir(t)

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("token endpoint should not be called for a valid session")
	}))
	defer server.Close()

	host := "https://valid.example.com"
	store := DefaultStore()
	mustSave(t, store, host, &Session{
		ClientID:     "av_mcp_1",
		AccessToken:  "still-good",
		RefreshToken: "refresh-1",
		Expiry:       time.Now().Add(time.Hour),
		TokenURL:     server.URL + "/oauth/token",
	})

	source, err := NewTokenSource(host, store, server.Client())
	if err != nil {
		t.Fatalf("NewTokenSource: %v", err)
	}
	token, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "still-good" {
		t.Fatalf("Token = %q, want still-good", token)
	}
}

func TestTokenSourceRefreshRotatesAndPersists(t *testing.T) {
	keyring.MockInit()
	useTempLockDir(t)

	var form url.Values
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		form = r.PostForm
		if _, _, ok := r.BasicAuth(); ok {
			t.Error("refresh used HTTP Basic auth; the client is public")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"access-2","refresh_token":"refresh-2",`+
			`"token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()

	host := "https://refresh.example.com"
	store := DefaultStore()
	mustSave(t, store, host, &Session{
		ClientID:     "av_mcp_1",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		Expiry:       time.Now().Add(-time.Minute),
		TokenURL:     server.URL + "/oauth/token",
	})

	source, err := NewTokenSource(host, store, server.Client())
	if err != nil {
		t.Fatalf("NewTokenSource: %v", err)
	}
	token, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "access-2" {
		t.Fatalf("Token = %q, want access-2", token)
	}

	if got := form.Get("grant_type"); got != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", got)
	}
	if got := form.Get("refresh_token"); got != "refresh-1" {
		t.Errorf("refresh_token = %q, want refresh-1", got)
	}
	if got := form.Get("client_id"); got != "av_mcp_1" {
		t.Errorf("client_id = %q, want av_mcp_1", got)
	}
	// The server issues empty-scope tokens and rejects a refresh that asks
	// for one.
	if _, ok := form["scope"]; ok {
		t.Errorf("refresh request sent scope=%q, want it omitted", form.Get("scope"))
	}

	stored, err := store.Load(host)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.RefreshToken != "refresh-2" || stored.AccessToken != "access-2" {
		t.Fatalf("stored session = %+v, want the rotated tokens", stored)
	}
	if stored.ClientID != "av_mcp_1" || stored.TokenURL == "" {
		t.Fatalf("stored session lost client metadata: %+v", stored)
	}

	// A second call reuses the refreshed token.
	if _, err := source.Token(context.Background()); err != nil {
		t.Fatalf("second Token: %v", err)
	}
	if calls != 1 {
		t.Fatalf("token endpoint called %d times, want 1", calls)
	}
}

func TestTokenSourceUsesSessionRefreshedByAnotherProcess(t *testing.T) {
	keyring.MockInit()
	useTempLockDir(t)

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("token endpoint should not be called when the keychain already has a fresh session")
	}))
	defer server.Close()

	host := "https://concurrent.example.com"
	store := DefaultStore()
	expired := &Session{
		ClientID:     "av_mcp_1",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		Expiry:       time.Now().Add(-time.Minute),
		TokenURL:     server.URL + "/oauth/token",
	}
	mustSave(t, store, host, expired)

	source, err := NewTokenSource(host, store, server.Client())
	if err != nil {
		t.Fatalf("NewTokenSource: %v", err)
	}

	// Simulate a concurrent CLI invocation refreshing first.
	mustSave(t, store, host, &Session{
		ClientID:     "av_mcp_1",
		AccessToken:  "access-2",
		RefreshToken: "refresh-2",
		Expiry:       time.Now().Add(time.Hour),
		TokenURL:     server.URL + "/oauth/token",
	})

	token, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "access-2" {
		t.Fatalf("Token = %q, want access-2", token)
	}
}

func TestTokenSourceInvalidGrantIsExpiredSession(t *testing.T) {
	keyring.MockInit()
	useTempLockDir(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
	}))
	defer server.Close()

	host := "https://revoked.example.com"
	store := DefaultStore()
	mustSave(t, store, host, &Session{
		ClientID:     "av_mcp_1",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		Expiry:       time.Now().Add(-time.Minute),
		TokenURL:     server.URL + "/oauth/token",
	})

	source, err := NewTokenSource(host, store, server.Client())
	if err != nil {
		t.Fatalf("NewTokenSource: %v", err)
	}
	if _, err := source.Token(context.Background()); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Token error = %v, want ErrSessionExpired", err)
	}
}

func TestNewTokenSourceWithoutSession(t *testing.T) {
	keyring.MockInit()
	if _, err := NewTokenSource("https://none.example.com", DefaultStore(), nil); !errors.Is(
		err, ErrNoSession,
	) {
		t.Fatalf("NewTokenSource error = %v, want ErrNoSession", err)
	}
}

func mustSave(t *testing.T, store Store, host string, session *Session) {
	t.Helper()
	if err := store.Save(host, session); err != nil {
		t.Fatalf("Save: %v", err)
	}
}
