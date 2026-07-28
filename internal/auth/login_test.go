package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestLogin drives the whole flow against a fake authorization server, with a
// stub browser that follows the redirect back to the loopback listener.
func TestLogin(t *testing.T) {
	keyring.MockInit()

	var tokenForm url.Values
	registrations := 0
	mux := http.NewServeMux()
	mux.HandleFunc(metadataPath, func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"issuer":"`+base+`",
			"authorization_endpoint":"`+base+`/oauth/authorize",
			"token_endpoint":"`+base+`/oauth/token",
			"registration_endpoint":"`+base+`/oauth/register"
		}`)
	})
	mux.HandleFunc("/oauth/register", func(w http.ResponseWriter, _ *http.Request) {
		registrations++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"client_id":"av_mcp_login"}`)
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		tokenForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"access-1","refresh_token":"refresh-1",`+
			`"token_type":"Bearer","expires_in":864000}`)
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
	session, err := Login(context.Background(), LoginOptions{
		APIHost:     server.URL,
		Store:       store,
		HTTPClient:  server.Client(),
		OpenBrowser: fakeBrowser,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if session.AccessToken != "access-1" || session.RefreshToken != "refresh-1" {
		t.Fatalf("session = %+v, want the issued tokens", session)
	}
	if session.ClientID != "av_mcp_login" {
		t.Errorf("ClientID = %q", session.ClientID)
	}
	if session.TokenURL != server.URL+"/oauth/token" {
		t.Errorf("TokenURL = %q, want the discovered token endpoint", session.TokenURL)
	}

	if got := authQuery.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	if got := authQuery.Get("redirect_uri"); !slices.Contains(redirectURIs, got) {
		t.Errorf("redirect_uri = %q, want one of the registered URIs", got)
	}
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
	if got := tokenForm.Get("client_id"); got != "av_mcp_login" {
		t.Errorf("token request client_id = %q", got)
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

	// A second login reuses the stored client registration.
	if _, err := Login(context.Background(), LoginOptions{
		APIHost:     server.URL,
		Store:       store,
		HTTPClient:  server.Client(),
		OpenBrowser: fakeBrowser,
	}); err != nil {
		t.Fatalf("second Login: %v", err)
	}
	if registrations != 1 {
		t.Fatalf("registered %d times, want 1", registrations)
	}
}

func TestLoginRejectsMismatchedState(t *testing.T) {
	keyring.MockInit()

	mux := http.NewServeMux()
	mux.HandleFunc(metadataPath, func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"issuer":"`+base+`",
			"authorization_endpoint":"`+base+`/oauth/authorize",
			"token_endpoint":"`+base+`/oauth/token",
			"registration_endpoint":"`+base+`/oauth/register"}`)
	})
	mux.HandleFunc("/oauth/register", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"client_id":"av_mcp_state"}`)
	})
	mux.HandleFunc("/oauth/token", func(http.ResponseWriter, *http.Request) {
		t.Error("token endpoint must not be reached when the state does not match")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	fakeBrowser := func(rawURL string) error {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		resp, err := server.Client().Get( //nolint:noctx
			parsed.Query().Get("redirect_uri") + "?code=the-code&state=wrong",
		)
		if err != nil {
			return err
		}
		return resp.Body.Close()
	}

	_, err := Login(context.Background(), LoginOptions{
		APIHost:     server.URL,
		Store:       DefaultStore(),
		HTTPClient:  server.Client(),
		OpenBrowser: fakeBrowser,
	})
	if err == nil {
		t.Fatal("expected an error for a mismatched state")
	}
}

func TestListenLoopbackBindsARegisteredURI(t *testing.T) {
	listener, uri, err := listenLoopback()
	if err != nil {
		t.Fatalf("listenLoopback: %v", err)
	}
	defer func() { _ = listener.Close() }()

	if !slices.Contains(redirectURIs, uri) {
		t.Fatalf("bound %q, which is not a registered redirect URI", uri)
	}
	if callbackPath(uri) != "/callback" {
		t.Fatalf("callbackPath(%q) = %q", uri, callbackPath(uri))
	}
}
