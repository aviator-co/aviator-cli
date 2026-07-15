package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// RunbookDetail is the response from GET /api/v1/runbook/<n>/detail. The
// per-field sections are only populated when requested (or when no fields are
// specified, in which case all are returned).
type RunbookDetail struct {
	RunbookNumber      int                 `json:"runbook_number"`
	URL                string              `json:"url"`
	RunbookVersion     *int                `json:"runbook_version"`
	StepsMarkdown      *string             `json:"steps_markdown,omitempty"`
	SpecFiles          []DetailSpecFile    `json:"spec_files,omitempty"`
	RunbookState       *RunbookState       `json:"runbook_state,omitempty"`
	AcceptanceCriteria []DetailCriterion   `json:"acceptance_criteria,omitempty"`
	LatestVerification *LatestVerification `json:"latest_verification,omitempty"`
}

// DetailSpecFile is a spec document attached to a runbook.
type DetailSpecFile struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
	MimeType string `json:"mime_type"`
}

// RunbookState describes the branches and steps of a runbook.
type RunbookState struct {
	TargetBranch  *string       `json:"target_branch"`
	WorkingBranch *string       `json:"working_branch"`
	Steps         []RunbookStep `json:"steps"`
}

// RunbookStep is a single step within a runbook's execution.
type RunbookStep struct {
	StepNumber int    `json:"step_number"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	CommitSHA  string `json:"commit_sha,omitempty"`
	PRNumber   int    `json:"pr_number,omitempty"`
	PRURL      string `json:"pr_url,omitempty"`
}

// DetailCriterion is one acceptance criterion.
type DetailCriterion struct {
	Ordinal int    `json:"ordinal"`
	RawText string `json:"raw_text"`
	Source  string `json:"source"`
}

// LatestVerification summarizes the most recent verification run.
type LatestVerification struct {
	Status          string         `json:"status"`
	TriggerSource   string         `json:"trigger_source"`
	RunbookVersion  *int           `json:"runbook_version"`
	CommitSHA       *string        `json:"commit_sha"`
	CriteriaTotal   int            `json:"criteria_total"`
	CriteriaPassed  int            `json:"criteria_passed"`
	CriteriaFailed  int            `json:"criteria_failed"`
	CriteriaSkipped int            `json:"criteria_skipped"`
	CriteriaWaived  int            `json:"criteria_waived"`
	ErrorMessage    *string        `json:"error_message"`
	FailedResults   []FailedResult `json:"failed_results"`
}

// FailedResult is a single failing criterion within a verification.
type FailedResult struct {
	Criterion   string          `json:"criterion"`
	IsInvariant bool            `json:"is_invariant"`
	IsWaived    bool            `json:"is_waived"`
	Status      string          `json:"status"`
	Reason      *string         `json:"reason"`
	Evidence    json.RawMessage `json:"evidence"`
	Location    json.RawMessage `json:"location"`
}

// GetRunbookDetail fetches a runbook's detail. When fields is non-empty, only
// those sections are requested via the fields query parameter.
func (c *Client) GetRunbookDetail(
	ctx context.Context, runbookNumber int, fields []string,
) (*RunbookDetail, error) {
	var out RunbookDetail
	if err := c.getJSON(
		ctx, runbookDetailPath(runbookNumber), runbookDetailQuery(fields), &out,
	); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRunbookDetailRaw fetches a runbook's detail and returns the response body
// verbatim, preserving fields this client version doesn't model.
func (c *Client) GetRunbookDetailRaw(
	ctx context.Context, runbookNumber int, fields []string,
) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.getJSON(
		ctx, runbookDetailPath(runbookNumber), runbookDetailQuery(fields), &out,
	); err != nil {
		return nil, err
	}
	return out, nil
}

func runbookDetailPath(runbookNumber int) string {
	return fmt.Sprintf("/api/v1/runbook/%d/detail", runbookNumber)
}

func runbookDetailQuery(fields []string) url.Values {
	if len(fields) == 0 {
		return nil
	}
	return url.Values{"fields": {strings.Join(fields, ",")}}
}
