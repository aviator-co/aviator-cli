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

// authFixture is a fake token endpoint plus the keychain a TokenSource for it
// reads from.
type authFixture struct {
	t     *testing.T
	host  string
	store Store
	http  *http.Client
	// calls counts token endpoint requests; form is the last one's body.
	calls int
	form  url.Values
}

func newAuthFixture(t *testing.T, handler http.HandlerFunc) *authFixture {
	t.Helper()
	keyring.MockInit()
	useTempLockDir(t)

	f := &authFixture{t: t, store: DefaultStore()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		f.form = r.PostForm
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	f.host = server.URL
	f.http = server.Client()
	return f
}

// save stores session, pointing it at the fixture's token endpoint.
func (f *authFixture) save(session *Session) {
	f.t.Helper()
	session.TokenURL = f.host + "/oauth/token"
	if err := f.store.Save(f.host, session); err != nil {
		f.t.Fatalf("Save: %v", err)
	}
}

func (f *authFixture) source(store Store) *TokenSource {
	f.t.Helper()
	if store == nil {
		store = f.store
	}
	source, err := NewTokenSource(f.host, store, f.http)
	if err != nil {
		f.t.Fatalf("NewTokenSource: %v", err)
	}
	source.warn = io.Discard
	return source
}

// freshSession is a session as the server issues it, with a short-lived access
// token and the longer-lived refresh token that renews it.
func freshSession() *Session {
	return &Session{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		Expiry:       time.Now().Add(2 * time.Hour),
	}
}

// expiredSession's access token has run out, so it must be refreshed before it
// is handed to anyone.
func expiredSession() *Session {
	s := freshSession()
	s.Expiry = time.Now().Add(-time.Minute)
	return s
}

// issueTokens is a token endpoint that rotates the pair, as the real one does.
func issueTokens(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"access_token":"access-2","refresh_token":"refresh-2",`+
		`"token_type":"Bearer","expires_in":7200}`)
}

func rejectWith(code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"`+code+`"}`)
	}
}

func unreachable(t *testing.T, why string) http.HandlerFunc {
	t.Helper()
	return func(http.ResponseWriter, *http.Request) { t.Errorf("token endpoint called: %s", why) }
}

// TestTokenSourceRefreshRotatesAndPersists covers the whole happy path: an
// expired access token is renewed, the rotated pair is persisted, and a second
// call reuses it rather than refreshing again.
func TestTokenSourceRefreshRotatesAndPersists(t *testing.T) {
	f := newAuthFixture(t, issueTokens)
	f.save(expiredSession())

	source := f.source(nil)
	token, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "access-2" {
		t.Fatalf("Token = %q, want access-2 (the stored access token had expired)", token)
	}

	if got := f.form.Get("grant_type"); got != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", got)
	}
	if got := f.form.Get("refresh_token"); got != "refresh-1" {
		t.Errorf("refresh_token = %q, want refresh-1", got)
	}
	if got := f.form.Get("client_id"); got != clientID {
		t.Errorf("client_id = %q, want the session's client", got)
	}
	// The server issues empty-scope tokens and rejects a refresh that asks
	// for one.
	if _, ok := f.form["scope"]; ok {
		t.Errorf("refresh request sent scope=%q, want it omitted", f.form.Get("scope"))
	}

	stored, err := f.store.Load(f.host)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.RefreshToken != "refresh-2" || stored.AccessToken != "access-2" {
		t.Fatalf("stored session = %+v, want the rotated tokens", stored)
	}
	if stored.TokenURL == "" || stored.Expiry.IsZero() {
		t.Fatalf("stored session lost client metadata: %+v", stored)
	}

	if _, err := source.Token(context.Background()); err != nil {
		t.Fatalf("second Token: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("token endpoint called %d times, want 1", f.calls)
	}
}

func TestTokenSourceRereadsStoreBeforeRefreshing(t *testing.T) {
	f := newAuthFixture(t, unreachable(t, "the keychain already held a fresh session"))
	f.save(expiredSession())
	source := f.source(nil)

	// Another CLI invocation refreshes after this source loaded the session.
	rotated := freshSession()
	rotated.AccessToken, rotated.RefreshToken = "access-2", "refresh-2"
	f.save(rotated)

	token, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "access-2" {
		t.Fatalf("Token = %q, want access-2", token)
	}
}

func TestTokenSourceDoesNotResurrectARemovedSession(t *testing.T) {
	f := newAuthFixture(t, unreachable(t, "the session was removed"))
	f.save(expiredSession())
	source := f.source(nil)

	// `aviator logout` runs while this command is in flight.
	if err := f.store.Delete(f.host); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := source.Token(context.Background()); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Token error = %v, want ErrSessionExpired", err)
	}
	if _, err := f.store.Load(f.host); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Load error = %v, want ErrNoSession", err)
	}
}

func TestTokenSourceForceRefreshRenewsAValidSession(t *testing.T) {
	f := newAuthFixture(t, issueTokens)
	f.save(freshSession())
	source := f.source(nil)

	token, err := source.ForceRefresh(context.Background(), "access-1")
	if err != nil {
		t.Fatalf("ForceRefresh: %v", err)
	}
	if token != "access-2" {
		t.Fatalf("ForceRefresh = %q, want access-2", token)
	}
	if f.calls != 1 {
		t.Fatalf("token endpoint called %d times, want 1", f.calls)
	}
}

// TestTokenSourceKeepsRefreshedSessionWhenSaveFails pins the recovery
// behaviour: the old refresh token is already dead once the server rotates, so
// a keychain write failure must not throw the new one away.
func TestTokenSourceKeepsRefreshedSessionWhenSaveFails(t *testing.T) {
	f := newAuthFixture(t, issueTokens)
	f.save(expiredSession())

	source := f.source(readOnlyStore{f.store})
	token, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "access-2" {
		t.Fatalf("Token = %q, want the refreshed token despite the save failure", token)
	}
	if _, err := source.Token(context.Background()); err != nil {
		t.Fatalf("second Token: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("token endpoint called %d times, want 1: the session was not adopted", f.calls)
	}
}

func TestTokenSourceDiscardsARevokedSession(t *testing.T) {
	f := newAuthFixture(t, rejectWith("invalid_grant"))
	f.save(expiredSession())

	if _, err := f.source(nil).Token(context.Background()); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Token error = %v, want ErrSessionExpired", err)
	}
	// A session the server has rejected must not be presented again: re-using a
	// revoked refresh token revokes the whole family.
	if _, err := f.store.Load(f.host); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Load after a rejected refresh = %v, want ErrNoSession", err)
	}
}

// TestTokenSourceKeepsASessionAfterATransientFailure covers the other side of
// discarding: a token endpoint that is merely down has said nothing about
// whether the refresh token is still good, so it must survive.
func TestTokenSourceKeepsASessionAfterATransientFailure(t *testing.T) {
	f := newAuthFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	f.save(expiredSession())

	if _, err := f.source(nil).Token(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := f.store.Load(f.host); err != nil {
		t.Fatalf("Load after a failed refresh = %v, want the session kept", err)
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

// readOnlyStore stands in for a keychain that has become unwritable, e.g. one
// that locked between login and refresh.
type readOnlyStore struct{ Store }

func (readOnlyStore) Save(string, *Session) error {
	return errors.New("the keychain is locked")
}
