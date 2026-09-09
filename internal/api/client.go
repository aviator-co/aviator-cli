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
	"github.com/aviator-co/aviator-cli/internal/auth"
	"github.com/aviator-co/aviator-cli/internal/config"
)

// Client talks to the Aviator REST API.
type Client struct {
	host   string
	tokens TokenSource
	http   *http.Client
}

// NewClient builds a Client from the loaded configuration.
func NewClient() (*Client, error) {
	tokens, err := resolveTokenSource()
	if err != nil {
		return nil, err
	}
	return &Client{
		host:   strings.TrimRight(config.Av.Aviator.APIHost, "/"),
		tokens: tokens,
		http:   &http.Client{Timeout: 30 * time.Second},
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

// patchJSON sends body as JSON to path via PATCH and decodes a successful
// response into out (which may be nil).
func (c *Client) patchJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPatch, path, body, out)
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
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return errors.Wrap(err, "failed to encode request")
		}
	}

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return err
	}
	status, data, err := c.send(ctx, method, path, payload, token)
	if err != nil {
		return err
	}

	// The server can revoke a token before it expires, so a 401 is the first
	// sign the stored session is stale. Renew it once and retry; a credential
	// that can't be renewed (a static token) falls through to the error below.
	if status == http.StatusUnauthorized {
		if source, ok := c.tokens.(refresher); ok {
			refreshed, err := source.ForceRefresh(ctx, token)
			if errors.Is(err, auth.ErrSessionExpired) {
				return err
			}
			if err != nil {
				return errors.Wrap(err, "could not renew the Aviator session")
			}
			if status, data, err = c.send(ctx, method, path, payload, refreshed); err != nil {
				return err
			}
		}
	}

	if status >= 400 {
		return c.statusError(status, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return errors.Wrap(err, "failed to decode response")
		}
	}
	return nil
}

// send performs one request and reads its whole response.
func (c *Client) send(
	ctx context.Context, method, path string, payload []byte, token string,
) (int, []byte, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, reader)
	if err != nil {
		return 0, nil, errors.Wrap(err, "failed to build request")
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, errors.Wrap(err, "request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, errors.Wrap(err, "failed to read response")
	}
	return resp.StatusCode, data, nil
}

func (c *Client) statusError(status int, data []byte) error {
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
	if status == http.StatusUnauthorized {
		msg += "; " + c.reauthHint()
	}
	// Account-scoped tokens have no owner, so endpoints that act as the caller
	// reject them. That is a configured credential, not an expired one.
	if _, ok := c.tokens.(refresher); status == http.StatusForbidden && !ok {
		msg += "; run `aviator login` to act as yourself"
	}
	return errors.Errorf("aviator API error (%d): %s", status, msg)
}

func (c *Client) reauthHint() string {
	if _, ok := c.tokens.(refresher); ok {
		return "run `aviator login` to sign in again"
	}
	return "check the Aviator API token you configured"
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
