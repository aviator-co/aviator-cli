package auth

import (
	"context"
	"net/http"
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
	return &TokenSource{host: host, store: store, http: httpClient, session: session}, nil
}

// Token returns a valid access token, refreshing the session if necessary.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session.valid() {
		return s.session.AccessToken, nil
	}

	unlock, err := lockRefresh(ctx, s.host)
	if err != nil {
		return "", err
	}
	defer unlock()

	// Another process may have refreshed while we waited for the lock.
	switch stored, err := s.store.Load(s.host); {
	case err == nil && stored.valid():
		s.session = stored
		return stored.AccessToken, nil
	case err == nil:
		s.session = stored
	case !errors.Is(err, ErrNoSession):
		return "", err
	}

	if s.session == nil || s.session.RefreshToken == "" {
		return "", ErrSessionExpired
	}
	refreshed, err := s.refresh(ctx, s.session)
	if err != nil {
		return "", err
	}
	if err := s.store.Save(s.host, refreshed); err != nil {
		return "", err
	}
	s.session = refreshed
	return refreshed.AccessToken, nil
}

// refresh exchanges the refresh token for a new token pair. The scope
// parameter is deliberately omitted: issued tokens carry an empty scope and
// the server rejects a refresh that requests one.
func (s *TokenSource) refresh(ctx context.Context, session *Session) (*Session, error) {
	cfg := &oauth2.Config{
		ClientID: session.ClientID,
		Endpoint: oauth2.Endpoint{
			TokenURL: session.TokenURL,
			// The client is public, so credentials go in the form body;
			// the server rejects HTTP Basic auth for it.
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.http)

	token, err := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: session.RefreshToken}).Token()
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
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
