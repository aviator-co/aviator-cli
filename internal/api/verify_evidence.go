package api

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"emperror.dev/errors"
)

// EvidenceURL resolves an evidence file to the short-lived signed storage URL
// the API redirects to. The redirect isn't followed, so the bearer token never
// reaches the storage host.
func (c *Client) EvidenceURL(ctx context.Context, evidenceID int) (string, error) {
	noRedirect := *c.http
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	path := fmt.Sprintf("/api/v1/verify/evidence/%d", evidenceID)
	status, header, data, err := c.do(ctx, &noRedirect, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	if location := header.Get("Location"); status == http.StatusFound && location != "" {
		return location, nil
	}
	if status >= 400 {
		return "", c.statusError(status, data)
	}
	return "", errors.Errorf("expected a redirect to the evidence file, got %d", status)
}

// OpenEvidence opens an evidence file for reading once storage has answered
// 200. The download carries no bearer token: the signed URL authorizes it.
func (c *Client) OpenEvidence(ctx context.Context, evidenceID int) (io.ReadCloser, error) {
	signedURL, err := c.EvidenceURL(ctx, evidenceID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build download request")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "download failed")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, errors.Errorf("evidence download failed (%d)", resp.StatusCode)
	}
	return resp.Body, nil
}
