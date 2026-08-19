package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// checkRedirectURI mirrors the server's RFC 8252 loopback matching: http, the
// literal 127.0.0.1, exactly /callback, and no query or fragment. Anything else
// is rejected before the browser ever opens.
func checkRedirectURI(t *testing.T, rawURL string) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("redirect URI %q does not parse: %v", rawURL, err)
	}
	switch {
	case parsed.Scheme != "http":
		t.Errorf("redirect URI %q: scheme = %q, want http", rawURL, parsed.Scheme)
	case parsed.Hostname() != "127.0.0.1":
		t.Errorf("redirect URI %q: host = %q, want 127.0.0.1", rawURL, parsed.Hostname())
	case parsed.Path != "/callback":
		t.Errorf("redirect URI %q: path = %q, want /callback", rawURL, parsed.Path)
	case parsed.RawQuery != "" || parsed.Fragment != "":
		t.Errorf("redirect URI %q carries a query or fragment", rawURL)
	}
	if port := parsed.Port(); port == "" || port == "0" {
		t.Errorf("redirect URI %q does not name the bound port", rawURL)
	}
}

// TestLogin drives the whole flow against a fake authorization server, with a
// stub browser that follows the redirect back to the loopback listener.
func TestLogin(t *testing.T) {
	keyring.MockInit()

	var tokenForm url.Values
	mux := http.NewServeMux()
	mux.HandleFunc(metadataPath, func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		body := `{
			"issuer":"` + base + `",
			"authorization_endpoint":"` + base + `/oauth/authorize",
			"token_endpoint":"` + base + `/oauth/token"
		}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body) //nolint:gosec // the test server's own address
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		tokenForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"access-1","refresh_token":"refresh-1",`+
			`"token_type":"Bearer","expires_in":7200}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	var authQuery url.Values
	fakeBrowser := func(rawURL string) error {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		authQuery = parsed.Query()
		callback := authQuery.Get("redirect_uri") +
			"?code=the-code&state=" + url.QueryEscape(authQuery.Get("state"))
		resp, err := server.Client().Get(callback) //nolint:noctx
		if err != nil {
			return err
		}
		return resp.Body.Close()
	}

	store := DefaultStore()
	err := Login(context.Background(), LoginOptions{
		APIHost:     server.URL,
		Store:       store,
		HTTPClient:  server.Client(),
		OpenBrowser: fakeBrowser,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	session, err := store.Load(server.URL)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if session.AccessToken != "access-1" || session.RefreshToken != "refresh-1" {
		t.Fatalf("session = %+v, want the issued tokens", session)
	}
	if session.TokenURL != server.URL+"/oauth/token" {
		t.Errorf("TokenURL = %q, want the discovered token endpoint", session.TokenURL)
	}

	if got := authQuery.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	checkRedirectURI(t, authQuery.Get("redirect_uri"))
	if _, ok := authQuery["scope"]; ok {
		t.Errorf("authorize request sent scope=%q, want it omitted", authQuery.Get("scope"))
	}

	verifier := tokenForm.Get("code_verifier")
	if verifier == "" {
		t.Fatal("token request did not include a code_verifier")
	}
	sum := sha256.Sum256([]byte(verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); want != authQuery.Get("code_challenge") {
		t.Errorf("code_challenge does not match the code_verifier")
	}
	if got := tokenForm.Get("client_id"); got != clientID {
		t.Errorf("token request client_id = %q, want %q", got, clientID)
	}
	if got := tokenForm.Get("code"); got != "the-code" {
		t.Errorf("token request code = %q, want the-code", got)
	}

	stored, err := store.Load(server.URL)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.AccessToken != "access-1" {
		t.Fatalf("stored session = %+v", stored)
	}
}

// TestCallbackIgnoresStrayRequests covers the callback listener directly:
// requests that aren't this login's redirect must be answered without consuming
// the one result the flow waits for.
func TestCallbackIgnoresStrayRequests(t *testing.T) {
	listener, redirectURI, err := listenLoopback(t.Context())
	if err != nil {
		t.Fatalf("listenLoopback: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := serveCallback(ctx, listener, redirectPath, "the-state")

	get := func(t *testing.T, rawURL string, host string) int {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		if host != "" {
			req.Host = host
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", rawURL, err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}

	strays := []struct {
		name string
		url  string
		host string
	}{
		// A tab left over from an earlier sign-in, or any page that merely
		// references the callback URL.
		{name: "wrong state", url: redirectURI + "?code=stray&state=wrong"},
		{name: "no state at all", url: redirectURI},
		{name: "wrong host header", url: redirectURI + "?code=x&state=the-state", host: "evil.test"},
	}
	for _, stray := range strays {
		t.Run(stray.name, func(t *testing.T) {
			if got := get(t, stray.url, stray.host); got != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", got)
			}
			select {
			case result := <-results:
				t.Fatalf("a stray request ended the sign-in: %+v", result)
			default:
			}
		})
	}

	// The real callback still completes after all of that.
	if got := get(t, redirectURI+"?code=the-code&state=the-state", ""); got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
	select {
	case result := <-results:
		if result.err != nil || result.code != "the-code" {
			t.Fatalf("result = %+v, want the-code", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the genuine callback was never reported")
	}
}

// TestLoginRequiresSecureHost covers the transport floor: a plain-HTTP API host
// that isn't loopback must never reach the browser.
func TestLoginRequiresSecureHost(t *testing.T) {
	err := Login(context.Background(), LoginOptions{
		APIHost: "http://api.aviator.co",
		Store:   DefaultStore(),
		OpenBrowser: func(string) error {
			t.Error("the browser was opened for a plain-HTTP host")
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Fatalf("error = %v, want it to name the https requirement", err)
	}
}
