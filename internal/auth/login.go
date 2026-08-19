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
	"time"

	"emperror.dev/errors"
	"github.com/pkg/browser"
	"golang.org/x/oauth2"
)

const (
	// redirectPath is the path the server expects a loopback redirect URI to
	// use. The server matches loopback redirects per RFC 8252: the scheme must
	// be http, the host the literal 127.0.0.1, the path exactly this, and there
	// must be no query or fragment. The port is ignored, so the CLI binds an
	// ephemeral one.
	redirectPath = "/callback"

	// loginTimeout bounds how long the user has to finish signing in.
	loginTimeout = 5 * time.Minute
	// exchangeTimeout bounds the code exchange, which must not inherit
	// whatever is left of the browser deadline.
	exchangeTimeout = 30 * time.Second
)

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

// Login runs the OAuth authorization code flow with PKCE against the API host
// and stores the resulting session in the keychain.
func Login(ctx context.Context, opts LoginOptions) error {
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

	meta, err := discover(ctx, httpClient, host)
	if err != nil {
		return err
	}
	listener, redirectURI, err := listenLoopback(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	cfg := &oauth2.Config{
		ClientID:    clientID,
		RedirectURL: redirectURI,
		Endpoint: oauth2.Endpoint{
			AuthURL:   meta.AuthorizationEndpoint,
			TokenURL:  meta.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}

	state, err := randomString()
	if err != nil {
		return err
	}
	verifier := oauth2.GenerateVerifier()
	authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	waitCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	codes := serveCallback(waitCtx, listener, redirectPath, state)

	logf(opts.Out, "Opening your browser to sign in to Aviator.\n")
	if err := opts.OpenBrowser(authURL); err != nil {
		logf(opts.Out, "Could not open a browser automatically.\n")
	}
	logf(opts.Out, "If nothing happens, visit:\n  %s\n\n", authURL)

	var code string
	select {
	case result := <-codes:
		if result.err != nil {
			return result.err
		}
		code = result.code
	case <-waitCtx.Done():
		return errors.Wrap(waitCtx.Err(), "timed out waiting for the browser sign-in to complete")
	}

	// The exchange gets its own deadline: the browser wait may have consumed
	// nearly all of loginTimeout.
	exchangeCtx, cancelExchange := context.WithTimeout(ctx, exchangeTimeout)
	defer cancelExchange()

	token, err := cfg.Exchange(
		context.WithValue(exchangeCtx, oauth2.HTTPClient, httpClient),
		code,
		oauth2.VerifierOption(verifier),
	)
	if err != nil {
		return errors.Wrap(err, "failed to exchange the authorization code")
	}

	session := &Session{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		TokenURL:     meta.TokenEndpoint,
	}
	return opts.Store.Save(host, session)
}

// listenLoopback binds an ephemeral loopback port and returns the redirect URI
// that reaches it.
func listenLoopback(ctx context.Context) (net.Listener, string, error) {
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", errors.Wrap(err, "could not open a local port for the sign-in callback")
	}
	return listener, "http://" + listener.Addr().String() + redirectPath, nil
}

type callbackResult struct {
	code string
	err  error
}

// serveCallback answers the OAuth redirect on listener and reports the
// authorization code exactly once. Requests that don't carry this login's
// state are answered but otherwise ignored, so a stale browser tab or a page
// that merely references the callback URL cannot cancel a sign-in that is
// still in flight.
func serveCallback(
	ctx context.Context, listener net.Listener, path, state string,
) <-chan callbackResult {
	results := make(chan callbackResult, 1)
	boundHost := listener.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != boundHost {
			http.Error(w, "unexpected host", http.StatusBadRequest)
			return
		}
		query := r.URL.Query()
		if query.Get("state") != state {
			writePage(w, http.StatusBadRequest, "Unexpected sign-in response",
				"This response does not belong to a sign-in started here. "+
					"Return to your terminal and run `aviator login` again.")
			return
		}

		result := callbackResult{code: query.Get("code")}
		switch {
		case query.Get("error") != "":
			message := query.Get("error")
			if description := query.Get("error_description"); description != "" {
				message += ": " + description
			}
			result.err = errors.Errorf("authorization was denied: %s", message)
		case result.code == "":
			result.err = errors.New("the sign-in response did not include an authorization code")
		}

		if result.err != nil {
			writePage(w, http.StatusBadRequest, "Sign-in failed", result.err.Error())
		} else {
			writePage(w, http.StatusOK, "You're signed in",
				"You can close this tab and return to your terminal.")
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
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	return results
}

func writePage(w http.ResponseWriter, status int, heading, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w,
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

// warnf reports a non-fatal problem. A nil writer discards it.
func warnf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	logf(w, "warning: "+format, args...)
}

func randomString() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.Wrap(err, "failed to generate random state")
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
