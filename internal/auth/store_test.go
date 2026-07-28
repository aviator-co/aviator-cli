package auth

import (
	"strings"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/zalando/go-keyring"
)

// TestStoreDiscardsCorruptEntries covers the store repairing itself: an entry
// that can't be decoded must look absent, or `aviator login` — the command that
// would overwrite it — fails too.
func TestStoreDiscardsCorruptEntries(t *testing.T) {
	keyring.MockInit()
	host := "https://corrupt.example.com"
	if err := keyring.Set(keyringService, sessionKey(host), "{not json"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if _, err := DefaultStore().Load(host); !errors.Is(err, ErrNoSession) {
		t.Fatalf("load of a corrupt entry = %v, want ErrNoSession", err)
	}
	if _, err := keyring.Get(keyringService, sessionKey(host)); !errors.Is(
		err, keyring.ErrNotFound,
	) {
		t.Fatalf("corrupt entry was left in the keychain (%v)", err)
	}
}

func TestStoreKeyringErrorKeepsBothCauses(t *testing.T) {
	underlying := errors.New("the secret service is not running")
	keyring.MockInitWithError(underlying)
	t.Cleanup(keyring.MockInit)

	_, err := DefaultStore().Load("https://headless.example.com")
	if !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("Load = %v, want it to match ErrKeyringUnavailable", err)
	}
	if !errors.Is(err, underlying) {
		t.Fatalf("Load = %v, want it to keep the underlying keychain error", err)
	}
	if !strings.HasPrefix(err.Error(), ErrKeyringUnavailable.Error()) {
		t.Fatalf("Load = %q, want the actionable guidance first", err)
	}
}

func TestSessionNeedsRefresh(t *testing.T) {
	// with applies field overrides to a session the server has just issued.
	with := func(edit func(*Session)) *Session {
		s := freshSession()
		edit(s)
		return s
	}

	tests := []struct {
		name    string
		session *Session
		want    bool
	}{
		{"nil", nil, true},
		{"just issued", freshSession(), false},
		{"no access token", with(func(s *Session) { s.AccessToken = "" }), true},
		{
			"most of the way through its life",
			with(func(s *Session) { s.Expiry = time.Now().Add(10 * time.Minute) }), false,
		},
		{"expired access token", expiredSession(), true},
		{
			"inside the expiry skew allowance",
			with(func(s *Session) { s.Expiry = time.Now().Add(expiryDelta / 2) }), true,
		},
		// An unknown expiry can't be trusted, so renew rather than find out by
		// having the request rejected.
		{"unknown expiry", with(func(s *Session) { s.Expiry = time.Time{} }), true},
		// Nothing to refresh with: the token is used for as long as the server
		// accepts it.
		{"no refresh token", &Session{AccessToken: "a"}, false},
		{
			"no refresh token and expired",
			&Session{AccessToken: "a", Expiry: time.Now().Add(-time.Hour)}, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.session.needsRefresh(); got != tt.want {
				t.Fatalf("needsRefresh() = %v, want %v", got, tt.want)
			}
		})
	}
}
