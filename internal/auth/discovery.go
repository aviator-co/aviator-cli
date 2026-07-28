package auth

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"time"

	"emperror.dev/errors"
)

const metadataPath = "/.well-known/oauth-authorization-server"

// metadata is the subset of RFC 8414 authorization server metadata the CLI
// uses. Endpoints are used verbatim: the authorization endpoint lives on the
// web app host, not the API host.
type metadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// discover fetches the OAuth authorization server metadata for an API host.
func discover(ctx context.Context, httpClient *http.Client, apiHost string) (*metadata, error) {
	host := normalizeHost(apiHost)
	if err := requireSecureURL(host, "Aviator API host"); err != nil {
		return nil, err
	}

	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	metaURL := host + metadataPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to discover OAuth endpoints at %s", metaURL)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to discover OAuth endpoints at %s", metaURL)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, errors.Errorf(
			"failed to discover OAuth endpoints at %s: HTTP %d", metaURL, resp.StatusCode,
		)
	}

	var meta metadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, errors.Wrapf(err, "failed to discover OAuth endpoints at %s", metaURL)
	}
	if err := meta.validate(); err != nil {
		return nil, errors.Wrapf(err, "%s advertised unusable OAuth metadata", metaURL)
	}
	return &meta, nil
}

// validate checks that the metadata carries the endpoints the login flow needs,
// so a missing one is reported here rather than as a URL parse failure later,
// and that none of them would put tokens on the wire in the clear.
func (m *metadata) validate() error {
	if m.AuthorizationEndpoint == "" || m.TokenEndpoint == "" {
		return errors.New("no authorization endpoint or token endpoint")
	}
	endpoints := []struct{ name, rawURL string }{
		{"authorization endpoint", m.AuthorizationEndpoint},
		{"token endpoint", m.TokenEndpoint},
	}
	for _, endpoint := range endpoints {
		if err := requireSecureURL(endpoint.rawURL, "OAuth "+endpoint.name); err != nil {
			return err
		}
	}
	return nil
}

// requireSecureURL rejects URLs that would carry credentials in the clear.
// Plain HTTP is allowed only for loopback, which local development and the
// backend's own local-dev URLs depend on.
func requireSecureURL(rawURL, what string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.Wrapf(err, "invalid %s URL %q", what, rawURL)
	}
	if parsed.Scheme == "https" || (parsed.Scheme == "http" && isLoopback(parsed.Hostname())) {
		return nil
	}
	return errors.Errorf("the %s must use https: %q", what, rawURL)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
