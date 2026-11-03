package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"emperror.dev/errors"
)

// EvidenceManifest is the body for POST /api/v1/verify/<n>/evidence: the
// scenarios someone ran outside Aviator and the evidence each step produced.
// Files are declared by type, content type and size; the server answers with
// one upload URL per declared file.
type EvidenceManifest struct {
	CommitSHA string             `json:"commit_sha"`
	Scenarios []ManifestScenario `json:"scenarios"`
}

// ManifestScenario is one scenario in a manifest.
type ManifestScenario struct {
	Summary  string              `json:"summary"`
	Steps    []ManifestStep      `json:"steps"`
	Criteria []ManifestCriterion `json:"criteria,omitempty"`
}

// ManifestStep is one step of a manifest scenario.
type ManifestStep struct {
	Text     string             `json:"text"`
	Evidence []ManifestEvidence `json:"evidence,omitempty"`
}

// ManifestEvidence declares one evidence file to upload.
type ManifestEvidence struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

// ManifestCriterion pins a scenario to a criterion by the handles
// `aviator show` prints.
type ManifestCriterion struct {
	StableKey           *string `json:"stable_key,omitempty"`
	BaselineInvariantID *int    `json:"baseline_invariant_id,omitempty"`
}

// EvidenceSubmission is the response from POST /api/v1/verify/<n>/evidence.
type EvidenceSubmission struct {
	RunbookNumber       int                 `json:"runbook_number"`
	CommitSHA           string              `json:"commit_sha"`
	UploadURLTTLSeconds int                 `json:"upload_url_ttl_seconds"`
	Scenarios           []SubmittedScenario `json:"scenarios"`
}

// SubmittedScenario is one scenario the server created from a manifest.
type SubmittedScenario struct {
	ID            int             `json:"id"`
	ScenarioRunID int             `json:"scenario_run_id"`
	Steps         []SubmittedStep `json:"steps"`
}

// SubmittedStep is one step of a submitted scenario, with an upload slot per
// declared evidence file, in manifest order.
type SubmittedStep struct {
	ID       int          `json:"id"`
	Evidence []UploadSlot `json:"evidence"`
}

// UploadSlot is where one declared evidence file is uploaded to.
type UploadSlot struct {
	ID          int    `json:"id"`
	UploadURL   string `json:"upload_url"`
	ContentType string `json:"content_type"`
}

// CompleteUploadsResponse is the response from
// POST /api/v1/verify/<n>/evidence/complete.
type CompleteUploadsResponse struct {
	RunbookNumber           int   `json:"runbook_number"`
	UploadedEvidenceIDs     []int `json:"uploaded_evidence_ids"`
	MissingEvidenceIDs      []int `json:"missing_evidence_ids"`
	RejectedEvidenceIDs     []int `json:"rejected_evidence_ids"`
	CompletedScenarioRunIDs []int `json:"completed_scenario_run_ids"`
}

// SubmitEvidenceManifest replaces a session's submitted scenarios with the
// manifest and returns the upload slots for its evidence files.
func (c *Client) SubmitEvidenceManifest(
	ctx context.Context, runbookNumber int, manifest EvidenceManifest,
) (*EvidenceSubmission, error) {
	path := fmt.Sprintf("/api/v1/verify/%d/evidence", runbookNumber)
	var out EvidenceSubmission
	if err := c.postJSON(ctx, path, manifest, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CompleteEvidenceUploads tells the server which evidence files were uploaded
// so it can check storage and complete the scenario runs they belong to.
func (c *Client) CompleteEvidenceUploads(
	ctx context.Context, runbookNumber int, evidenceIDs []int,
) (*CompleteUploadsResponse, error) {
	path := fmt.Sprintf("/api/v1/verify/%d/evidence/complete", runbookNumber)
	body := struct {
		EvidenceIDs []int `json:"evidence_ids"`
	}{EvidenceIDs: evidenceIDs}
	var out CompleteUploadsResponse
	if err := c.postJSON(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadEvidenceFile PUTs one file to a signed upload URL. The URL authorizes
// the upload, so no bearer token is sent; the content type must match what the
// manifest declared or storage rejects the signature.
func (c *Client) UploadEvidenceFile(ctx context.Context, slot UploadSlot, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.Wrapf(err, "failed to open %s", path)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return errors.Wrapf(err, "failed to stat %s", path)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, slot.UploadURL, f)
	if err != nil {
		return errors.Wrap(err, "failed to build upload request")
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", slot.ContentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.Wrapf(err, "upload of %s failed", path)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return errors.Errorf("upload of %s failed (%d): %s", path, resp.StatusCode, string(data))
	}
	return nil
}
