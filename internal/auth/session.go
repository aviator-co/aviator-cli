// Package auth implements the browser-based OAuth 2.0 login flow for the
// Aviator CLI and stores the resulting session in the OS keychain.
package auth

import (
	"strings"
	"time"
)

// expiryDelta treats a token as expired slightly early so a request issued
// right after the check still arrives with a valid token.
const expiryDelta = 2 * time.Minute

// Session is an OAuth session for a single Aviator API host. It is stored as
// JSON in the OS keychain and never written to disk.
type Session struct {
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	// TokenURL is recorded at login so refreshes don't need to re-run
	// discovery.
	TokenURL string `json:"token_url"`
}

func (s *Session) valid() bool {
	return s != nil && s.AccessToken != "" &&
		(s.Expiry.IsZero() || time.Now().Add(expiryDelta).Before(s.Expiry))
}

// ClientRegistration is the dynamically registered OAuth client for an API
// host. It outlives logout so signing back in doesn't hit the registration
// rate limit.
type ClientRegistration struct {
	ClientID     string   `json:"client_id"`
	RedirectURIs []string `json:"redirect_uris"`
}

func (c *ClientRegistration) matches(redirectURIs []string) bool {
	if c == nil || c.ClientID == "" || len(c.RedirectURIs) != len(redirectURIs) {
		return false
	}
	for i, uri := range redirectURIs {
		if c.RedirectURIs[i] != uri {
			return false
		}
	}
	return true
}

// normalizeHost trims the trailing slash so a host is keyed consistently.
func normalizeHost(host string) string {
	return strings.TrimRight(strings.TrimSpace(host), "/")
}
