package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"emperror.dev/errors"
)

const (
	metadataPath = "/.well-known/oauth-authorization-server"
	clientName   = "Aviator CLI"
)

// Metadata is the subset of RFC 8414 authorization server metadata the CLI
// uses. Endpoints are used verbatim: the authorization endpoint lives on the
// web app host, not the API host.
type Metadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// Discover fetches the OAuth authorization server metadata for an API host.
func Discover(ctx context.Context, httpClient *http.Client, apiHost string) (*Metadata, error) {
	var meta Metadata
	url := normalizeHost(apiHost) + metadataPath
	if err := getJSON(ctx, httpClient, url, &meta); err != nil {
		return nil, errors.Wrapf(err, "failed to discover OAuth endpoints at %s", url)
	}
	if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return nil, errors.Errorf("%s did not advertise OAuth endpoints", url)
	}
	return &meta, nil
}

type registerRequest struct {
	RedirectURIs []string `json:"redirect_uris"`
	ClientName   string   `json:"client_name"`
}

// RegisterClient dynamically registers a public OAuth client (RFC 7591) and
// returns its registration. The server rate limits this endpoint, so callers
// should persist the result rather than registering per login.
func RegisterClient(
	ctx context.Context, httpClient *http.Client, meta *Metadata, redirectURIs []string,
) (*ClientRegistration, error) {
	if meta.RegistrationEndpoint == "" {
		return nil, errors.New("the Aviator server does not support OAuth client registration")
	}
	body, err := json.Marshal(registerRequest{RedirectURIs: redirectURIs, ClientName: clientName})
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode the client registration")
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, meta.RegistrationEndpoint, bytes.NewReader(body),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build the client registration request")
	}
	req.Header.Set("Content-Type", "application/json")

	var reg ClientRegistration
	if err := doJSON(httpClient, req, &reg); err != nil {
		return nil, errors.Wrap(err, "failed to register an OAuth client with Aviator")
	}
	if reg.ClientID == "" {
		return nil, errors.New("the Aviator server returned an empty OAuth client ID")
	}
	reg.RedirectURIs = redirectURIs
	return &reg, nil
}

func getJSON(ctx context.Context, httpClient *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.Wrap(err, "failed to build request")
	}
	return doJSON(httpClient, req, out)
}

func doJSON(httpClient *http.Client, req *http.Request, out any) error {
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read response")
	}
	if resp.StatusCode >= 400 {
		return errors.Errorf("HTTP %d: %s", resp.StatusCode, oauthErrorMessage(data))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.Wrap(err, "failed to decode response")
	}
	return nil
}

// oauthErrorMessage renders the {error, error_description} envelope used by
// both the OAuth endpoints and the registration endpoint.
func oauthErrorMessage(data []byte) string {
	var env struct {
		Err         string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(data, &env)
	switch {
	case env.Err != "" && env.Description != "":
		return env.Err + ": " + env.Description
	case env.Err != "":
		return env.Err
	default:
		return strings.TrimSpace(string(data))
	}
}
