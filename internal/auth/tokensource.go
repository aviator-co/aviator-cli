package auth

import (
	"context"
	"io"
	"net/http"
	"os"
	"sync"

	"emperror.dev/errors"
	"golang.org/x/oauth2"
)

// ErrSessionExpired is returned when the stored session can no longer be
// refreshed and the user has to sign in again.
var ErrSessionExpired = errors.Sentinel(
	"your Aviator session has expired; run `aviator login` to sign in again",
)

// TokenSource hands out access tokens for an API host, refreshing and
// re-persisting them as needed.
type TokenSource struct {
	host  string
	store Store
	http  *http.Client
	// warn receives non-fatal problems, such as a session that could be
	// refreshed but not written back to the keychain.
	warn io.Writer

	mu      sync.Mutex
	session *Session
}

// NewTokenSource loads the stored session for host. It returns ErrNoSession
// when the user has not logged in.
func NewTokenSource(host string, store Store, httpClient *http.Client) (*TokenSource, error) {
	host = normalizeHost(host)
	session, err := store.Load(host)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	return &TokenSource{
		host:    host,
		store:   store,
		http:    httpClient,
		warn:    os.Stderr,
		session: session,
	}, nil
}

// Token returns a valid access token, refreshing the session if necessary.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.session.needsRefresh() {
		return s.session.AccessToken, nil
	}
	return s.renew(ctx, "")
}

// ForceRefresh renews the session even when the current access token still
// looks valid, for when the server has rejected it. The rejected token is never
// handed back, so the caller can safely retry with the result.
func (s *TokenSource) ForceRefresh(ctx context.Context, rejected string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.renew(ctx, rejected)
}

// renew refreshes the session under the cross-process refresh lock. Callers
// must hold s.mu.
func (s *TokenSource) renew(ctx context.Context, rejected string) (string, error) {
	unlock, err := lockRefresh(ctx, s.host, s.warn)
	if err != nil {
		return "", err
	}
	defer unlock()

	// Another process may have refreshed while we waited for the lock.
	switch stored, err := s.store.Load(s.host); {
	case err == nil && !stored.needsRefresh() && stored.AccessToken != rejected:
		s.session = stored
		return stored.AccessToken, nil
	case err == nil:
		s.session = stored
	case errors.Is(err, ErrNoSession):
		// The session was removed while this command was in flight, by a
		// logout or by another command discarding it. Refreshing what we still
		// hold would write it back and undo that.
		s.session = nil
		return "", ErrSessionExpired
	default:
		return "", err
	}

	if s.session == nil || s.session.RefreshToken == "" {
		return "", ErrSessionExpired
	}
	refreshed, err := s.refresh(ctx, s.session)
	if err != nil {
		if errors.Is(err, ErrSessionExpired) {
			// The server has told us these credentials are dead. Leaving them
			// in place would re-present a dead refresh token on every later
			// command.
			s.session = nil
			_ = s.store.Delete(s.host)
		}
		return "", err
	}

	// The server has already rotated the refresh token, so the tokens we
	// started with are dead regardless of what happens next: adopt the new
	// session before persisting it, and treat a keychain write failure as a
	// warning so the command in flight still completes.
	s.session = refreshed
	if err := s.store.Save(s.host, refreshed); err != nil {
		warnf(s.warn, "failed to save the refreshed Aviator session: %v\n"+
			"  a later command may ask you to run `aviator login` again\n", err)
	}
	return refreshed.AccessToken, nil
}

// refresh exchanges the refresh token for a new token pair. The scope
// parameter is deliberately omitted: the server rejects a refresh that requests
// a scope.
func (s *TokenSource) refresh(ctx context.Context, session *Session) (*Session, error) {
	if err := requireSecureURL(session.TokenURL, "OAuth token endpoint"); err != nil {
		return nil, err
	}
	cfg := &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			TokenURL: session.TokenURL,
			// The client is public, so credentials go in the form body
			// rather than HTTP Basic auth.
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.http)

	token, err := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: session.RefreshToken}).Token()
	if err != nil {
		if oauthErrorCode(err) == "invalid_grant" {
			return nil, ErrSessionExpired
		}
		return nil, errors.Wrap(err, "failed to refresh the Aviator session")
	}

	refreshed := *session
	refreshed.AccessToken = token.AccessToken
	refreshed.Expiry = token.Expiry
	if token.RefreshToken != "" {
		refreshed.RefreshToken = token.RefreshToken
	}
	return &refreshed, nil
}

// oauthErrorCode returns the RFC 6749 error code carried by a token endpoint
// failure, or "" for transport-level errors.
func oauthErrorCode(err error) string {
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) {
		return retrieveErr.ErrorCode
	}
	return ""
}
