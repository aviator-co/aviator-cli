package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/pkg/browser"
	"golang.org/x/oauth2"
)

// redirectURIs are the loopback callbacks registered with the server. The
// server matches redirect URIs by exact string, so the CLI cannot pick an
// ephemeral port: it registers this fixed set once and binds the first one
// that is free. Note the server rejects IPv6 loopback and, over HTTP, any
// host other than localhost or 127.0.0.1.
var redirectURIs = []string{
	"http://127.0.0.1:8976/callback",
	"http://127.0.0.1:8977/callback",
	"http://127.0.0.1:8978/callback",
	"http://127.0.0.1:8979/callback",
}

const loginTimeout = 5 * time.Minute

// LoginOptions configures the browser-based login flow.
type LoginOptions struct {
	APIHost    string
	Store      Store
	HTTPClient *http.Client
	// Out receives progress messages such as the authorization URL.
	Out io.Writer
	// OpenBrowser defaults to opening the user's browser.
	OpenBrowser func(url string) error
}

// Login runs the OAuth authorization code flow with PKCE against the API
// host, stores the resulting session in the keychain and returns it.
func Login(ctx context.Context, opts LoginOptions) (*Session, error) {
	host := normalizeHost(opts.APIHost)
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.OpenBrowser == nil {
		opts.OpenBrowser = openBrowser
	}

	meta, err := Discover(ctx, httpClient, host)
	if err != nil {
		return nil, err
	}
	reg, err := loadOrRegisterClient(ctx, httpClient, opts.Store, host, meta)
	if err != nil {
		return nil, err
	}

	listener, redirectURI, err := listenLoopback()
	if err != nil {
		return nil, err
	}
	defer func() { _ = listener.Close() }()

	cfg := &oauth2.Config{
		ClientID:    reg.ClientID,
		RedirectURL: redirectURI,
		Endpoint: oauth2.Endpoint{
			AuthURL:   meta.AuthorizationEndpoint,
			TokenURL:  meta.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}

	state, err := randomString()
	if err != nil {
		return nil, err
	}
	verifier := oauth2.GenerateVerifier()
	authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	codes := serveCallback(ctx, listener, callbackPath(redirectURI), state)

	logf(opts.Out, "Opening your browser to sign in to Aviator.\n")
	if err := opts.OpenBrowser(authURL); err != nil {
		logf(opts.Out, "Could not open a browser automatically.\n")
	}
	logf(opts.Out, "If nothing happens, visit:\n  %s\n\n", authURL)

	var code string
	select {
	case result := <-codes:
		if result.err != nil {
			return nil, result.err
		}
		code = result.code
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "timed out waiting for the browser sign-in to complete")
	}

	token, err := cfg.Exchange(
		context.WithValue(ctx, oauth2.HTTPClient, httpClient),
		code,
		oauth2.VerifierOption(verifier),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to exchange the authorization code")
	}

	session := &Session{
		ClientID:     reg.ClientID,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		TokenURL:     meta.TokenEndpoint,
	}
	if err := opts.Store.Save(host, session); err != nil {
		return nil, err
	}
	return session, nil
}

// loadOrRegisterClient reuses the stored client registration when it still
// covers the current redirect URIs. Registration is rate limited per IP, so it
// must not run on every login.
func loadOrRegisterClient(
	ctx context.Context, httpClient *http.Client, store Store, host string, meta *Metadata,
) (*ClientRegistration, error) {
	reg, err := store.LoadClient(host)
	if err != nil && !errors.Is(err, ErrNoSession) {
		return nil, err
	}
	if reg.matches(redirectURIs) {
		return reg, nil
	}

	reg, err = RegisterClient(ctx, httpClient, meta, redirectURIs)
	if err != nil {
		return nil, err
	}
	if err := store.SaveClient(host, reg); err != nil {
		return nil, err
	}
	return reg, nil
}

func listenLoopback() (net.Listener, string, error) {
	for _, uri := range redirectURIs {
		parsed, err := url.Parse(uri)
		if err != nil {
			return nil, "", errors.Wrapf(err, "invalid redirect URI %q", uri)
		}
		listener, err := net.Listen("tcp", parsed.Host)
		if err == nil {
			return listener, uri, nil
		}
	}
	return nil, "", errors.Errorf(
		"could not bind any of the local callback ports (%s); close whatever is using them and retry",
		portList(),
	)
}

type callbackResult struct {
	code string
	err  error
}

// serveCallback answers the OAuth redirect on listener and reports the
// authorization code exactly once.
func serveCallback(
	ctx context.Context, listener net.Listener, path, state string,
) <-chan callbackResult {
	results := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		result := callbackResult{code: query.Get("code")}
		switch {
		case query.Get("error") != "":
			message := query.Get("error")
			if description := query.Get("error_description"); description != "" {
				message += ": " + description
			}
			result.err = errors.Errorf("authorization was denied: %s", message)
		case query.Get("state") != state:
			result.err = errors.New("the sign-in response did not match this request; start over")
		case result.code == "":
			result.err = errors.New("the sign-in response did not include an authorization code")
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if result.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, resultPage("Sign-in failed", result.err.Error()))
		} else {
			_, _ = io.WriteString(w, resultPage(
				"You're signed in", "You can close this tab and return to your terminal.",
			))
		}

		select {
		case results <- result:
		default:
		}
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	return results
}

func callbackPath(redirectURI string) string {
	parsed, err := url.Parse(redirectURI)
	if err != nil || parsed.Path == "" {
		return "/"
	}
	return parsed.Path
}

func portList() string {
	ports := make([]string, 0, len(redirectURIs))
	for _, uri := range redirectURIs {
		if parsed, err := url.Parse(uri); err == nil {
			ports = append(ports, parsed.Port())
		}
	}
	return strings.Join(ports, ", ")
}

func resultPage(heading, detail string) string {
	return fmt.Sprintf(
		`<!doctype html><meta charset="utf-8"><title>Aviator CLI</title>`+
			`<body style="font-family:system-ui,sans-serif;margin:4rem auto;max-width:32rem">`+
			`<h1>%s</h1><p>%s</p></body>`,
		html.EscapeString(heading), html.EscapeString(detail),
	)
}

func openBrowser(url string) error {
	// pkg/browser writes the launcher's output to these by default.
	browser.Stdout = io.Discard
	browser.Stderr = io.Discard
	return browser.OpenURL(url)
}

func logf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func randomString() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.Wrap(err, "failed to generate random state")
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
