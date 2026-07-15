// Package api is a thin REST client for the Aviator API.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/config"
)

// ErrNoAPIToken is returned when no API token is configured.
var ErrNoAPIToken = errors.Sentinel(
	"no Aviator API token configured; set AVIATOR_API_TOKEN or add aviator.apiToken to your config",
)

// Client talks to the Aviator REST API.
type Client struct {
	host  string
	token string
	http  *http.Client
}

// NewClient builds a Client from the loaded configuration.
func NewClient() (*Client, error) {
	if config.Av.Aviator.APIToken == "" {
		return nil, ErrNoAPIToken
	}
	return &Client{
		host:  strings.TrimRight(config.Av.Aviator.APIHost, "/"),
		token: config.Av.Aviator.APIToken,
		http:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

type apiError struct {
	Err     string   `json:"error"`
	Message string   `json:"message"`
	Issues  []string `json:"issues,omitempty"`
}

// postJSON sends body as JSON to path and decodes a successful response into
// out (which may be nil). Non-2xx responses are turned into an error using the
// API's {error, message} envelope when present.
func (c *Client) postJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, out)
}

// getJSON issues a GET to path with the given query and decodes a successful
// response into out (which may be nil).
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

// doJSON sends body (when non-nil) as JSON to path using method and decodes a
// successful response into out (which may be nil). Non-2xx responses are turned
// into an error using the API's {error, message} envelope when present.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return errors.Wrap(err, "failed to encode request")
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.host+path, reader)
	if err != nil {
		return errors.Wrap(err, "failed to build request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return errors.Wrap(err, "request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read response")
	}

	if resp.StatusCode >= 400 {
		var ae apiError
		_ = json.Unmarshal(data, &ae)
		msg := ae.Message
		if msg == "" {
			msg = ae.Err
		}
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		if len(ae.Issues) > 0 {
			msg += ": " + strings.Join(ae.Issues, "; ")
		}
		return errors.Errorf("aviator API error (%d): %s", resp.StatusCode, msg)
	}

	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return errors.Wrap(err, "failed to decode response")
		}
	}
	return nil
}

// Repository identifies a connected GitHub repository.
type Repository struct {
	Org  string `json:"org"`
	Name string `json:"name"`
}

// SpecFile is an optional spec document attached to a submission.
type SpecFile struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}
