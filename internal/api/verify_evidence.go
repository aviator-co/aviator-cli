package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

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

// DownloadEvidence streams an evidence file into w.
func (c *Client) DownloadEvidence(ctx context.Context, evidenceID int, w io.Writer) error {
	signedURL, err := c.EvidenceURL(ctx, evidenceID)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil {
		return errors.Wrap(err, "failed to build download request")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "download failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return errors.Errorf("evidence download failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return errors.Wrap(err, "failed to write evidence")
	}
	return nil
}
