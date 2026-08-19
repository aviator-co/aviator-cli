// Package auth implements the browser-based OAuth 2.0 login flow for the
// Aviator CLI and stores the resulting session in the OS keychain.
package auth

import (
	"strings"
	"time"
)

const (
	// clientID identifies the CLI to the Aviator OAuth server. It is a
	// first-party public client defined in the server's code, so it is the same
	// everywhere including on-prem, and it is not a secret: the CLI holds no
	// client secret and authenticates with PKCE alone.
	clientID = "aviator_cli"

	// expiryDelta treats a token as expired slightly early so a request issued
	// right after the check still arrives with a valid token.
	expiryDelta = 2 * time.Minute
)

// Session is an OAuth session for a single Aviator API host. It is stored as
// JSON in the OS keychain and never written to disk.
type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	// TokenURL is recorded at login so refreshes don't need to re-run
	// discovery.
	TokenURL string `json:"token_url"`
}

// needsRefresh reports whether the session must be renewed before its access
// token is handed out.
func (s *Session) needsRefresh() bool {
	switch {
	case s == nil || s.AccessToken == "":
		return true
	case s.RefreshToken == "":
		// Renewal is impossible, so an unknown or past expiry is no reason to
		// discard a token that may still be accepted.
		return false
	case s.Expiry.IsZero():
		return true
	}
	return !time.Now().Add(expiryDelta).Before(s.Expiry)
}

// normalizeHost trims the trailing slash so a host is keyed consistently.
func normalizeHost(host string) string {
	return strings.TrimRight(strings.TrimSpace(host), "/")
}
