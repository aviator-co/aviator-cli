package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"emperror.dev/errors"
)

// InvariantCondition narrows when an invariant applies. Type is one of the
// server's condition types (file_path_glob, language); Negate inverts the
// match.
type InvariantCondition struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Negate bool   `json:"negate"`
}

// Invariant is a baseline invariant as returned by /api/v1/invariants. An
// invariant with no repositories is account-scoped and applies to every repo.
type Invariant struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Category string `json:"category"`
	Source   string `json:"source"`
	Status   string `json:"status"`
	Enabled  bool   `json:"enabled"`
	// Reason is why the rule was proposed; set by the AI pipelines, empty for
	// manual ones. SourceRefs are the evidence behind it (PR URLs and such).
	Reason       string               `json:"reason"`
	SourceRefs   []string             `json:"source_refs"`
	Repositories []Repository         `json:"repositories"`
	Conditions   []InvariantCondition `json:"conditions"`
	CreatedAt    string               `json:"created_at"`
	ModifiedAt   string               `json:"modified_at"`
}

// ListInvariantsQuery is the query string for GET /api/v1/invariants. A
// non-nil Repo narrows to the invariants that apply to that repo (account-scoped
// ones included); an empty Status lists every status; IDs and Sources narrow to
// those ids and provenance values. Empty filters are omitted rather than sent
// blank (the server reads a blank list as "no filter"), and Page and PerPage
// are sent only when positive, so the server's defaults hold otherwise.
type ListInvariantsQuery struct {
	Repo    *Repository
	Status  string
	IDs     []int
	Sources []string
	Page    int
	PerPage int
}

// InvariantCategory is one of the account's categories, whose slug create and
// update take.
type InvariantCategory struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListInvariantCategoriesResponse is the response from
// GET /api/v1/invariants/categories. It is empty until something has seeded
// the account's categories (the settings page, verify onboarding, or mining).
type ListInvariantCategoriesResponse struct {
	Categories []InvariantCategory `json:"categories"`
}

// ListInvariantsResponse is one page of GET /api/v1/invariants, newest first.
type ListInvariantsResponse struct {
	Invariants []Invariant `json:"invariants"`
	Page       int         `json:"page"`
	PerPage    int         `json:"per_page"`
	HasMore    bool        `json:"has_more"`
}

// CreateInvariantRequest is the body for POST /api/v1/invariants. Leaving
// Repositories empty makes the invariant account-scoped. Enabled defaults to
// true on the server when omitted.
type CreateInvariantRequest struct {
	Title        string               `json:"title"`
	Body         string               `json:"body"`
	Category     string               `json:"category"`
	Repositories []Repository         `json:"repositories,omitempty"`
	Enabled      *bool                `json:"enabled,omitempty"`
	Conditions   []InvariantCondition `json:"conditions,omitempty"`
}

// UpdateInvariantRequest is the body for PATCH /api/v1/invariants/<id>. Every
// field is optional: nil leaves the current value alone. Repositories and
// Conditions replace the current set wholesale when non-nil, so a pointer to an
// empty slice clears them (an empty Repositories makes the invariant
// account-scoped).
type UpdateInvariantRequest struct {
	Title        *string               `json:"title,omitempty"`
	Body         *string               `json:"body,omitempty"`
	Category     *string               `json:"category,omitempty"`
	Repositories *[]Repository         `json:"repositories,omitempty"`
	Enabled      *bool                 `json:"enabled,omitempty"`
	Conditions   *[]InvariantCondition `json:"conditions,omitempty"`
}

// DeleteInvariantResponse is the response from DELETE /api/v1/invariants/<id>.
type DeleteInvariantResponse struct {
	DeletedInvariantID int `json:"deleted_invariant_id"`
}

// SetInvariantStatusRequest is the body for POST /api/v1/invariants/status.
// Status is pending, active, or rejected; the server derives enabled from it.
// The whole batch fails if any id is not under the caller's account.
type SetInvariantStatusRequest struct {
	InvariantIDs []int  `json:"invariant_ids"`
	Status       string `json:"status"`
}

// SetInvariantStatusResponse carries the updated invariants.
type SetInvariantStatusResponse struct {
	Invariants []Invariant `json:"invariants"`
}

// Every method here returns the verbatim response body alongside the decoded
// value, so --json can print exactly what the server sent: fields this client
// does not model, and nulls that decode to zero values, survive.

// ListInvariants fetches one page of the account's baseline invariants.
func (c *Client) ListInvariants(
	ctx context.Context, q ListInvariantsQuery,
) (json.RawMessage, *ListInvariantsResponse, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/api/v1/invariants", listInvariantsQuery(q), &raw); err != nil {
		return nil, nil, err
	}
	var out ListInvariantsResponse
	return raw, &out, decodeInvariantResponse(raw, &out)
}

func decodeInvariantResponse(raw json.RawMessage, out any) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.Wrap(err, "failed to decode invariant response")
	}
	return nil
}

func listInvariantsQuery(q ListInvariantsQuery) url.Values {
	values := url.Values{}
	if q.Repo != nil {
		values.Set("org", q.Repo.Org)
		values.Set("repo", q.Repo.Name)
	}
	if q.Status != "" {
		values.Set("status", q.Status)
	}
	if len(q.IDs) > 0 {
		ids := make([]string, len(q.IDs))
		for i, id := range q.IDs {
			ids[i] = strconv.Itoa(id)
		}
		values.Set("ids", strings.Join(ids, ","))
	}
	if len(q.Sources) > 0 {
		values.Set("source", strings.Join(q.Sources, ","))
	}
	if q.Page > 0 {
		values.Set("page", strconv.Itoa(q.Page))
	}
	if q.PerPage > 0 {
		values.Set("per_page", strconv.Itoa(q.PerPage))
	}
	return values
}

// ListInvariantCategories fetches the account's invariant categories.
func (c *Client) ListInvariantCategories(
	ctx context.Context,
) (json.RawMessage, *ListInvariantCategoriesResponse, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/api/v1/invariants/categories", nil, &raw); err != nil {
		return nil, nil, err
	}
	var out ListInvariantCategoriesResponse
	return raw, &out, decodeInvariantResponse(raw, &out)
}

// CreateInvariant creates a baseline invariant. Requires a maintainer's user
// access token.
func (c *Client) CreateInvariant(
	ctx context.Context, req CreateInvariantRequest,
) (json.RawMessage, *Invariant, error) {
	var raw json.RawMessage
	if err := c.postJSON(ctx, "/api/v1/invariants", req, &raw); err != nil {
		return nil, nil, err
	}
	var out Invariant
	return raw, &out, decodeInvariantResponse(raw, &out)
}

// UpdateInvariant patches the given fields of an invariant.
func (c *Client) UpdateInvariant(
	ctx context.Context, invariantID int, req UpdateInvariantRequest,
) (json.RawMessage, *Invariant, error) {
	var raw json.RawMessage
	if err := c.patchJSON(ctx, invariantPath(invariantID), req, &raw); err != nil {
		return nil, nil, err
	}
	var out Invariant
	return raw, &out, decodeInvariantResponse(raw, &out)
}

// DeleteInvariant removes an invariant and its conditions. To keep the row
// while taking it out of service, reject it via SetInvariantStatus instead.
func (c *Client) DeleteInvariant(
	ctx context.Context, invariantID int,
) (json.RawMessage, *DeleteInvariantResponse, error) {
	var raw json.RawMessage
	if err := c.deleteJSON(ctx, invariantPath(invariantID), &raw); err != nil {
		return nil, nil, err
	}
	var out DeleteInvariantResponse
	return raw, &out, decodeInvariantResponse(raw, &out)
}

// SetInvariantStatus approves, rejects, or restores invariants in bulk.
func (c *Client) SetInvariantStatus(
	ctx context.Context, req SetInvariantStatusRequest,
) (json.RawMessage, *SetInvariantStatusResponse, error) {
	var raw json.RawMessage
	if err := c.postJSON(ctx, "/api/v1/invariants/status", req, &raw); err != nil {
		return nil, nil, err
	}
	var out SetInvariantStatusResponse
	return raw, &out, decodeInvariantResponse(raw, &out)
}

func invariantPath(invariantID int) string {
	return fmt.Sprintf("/api/v1/invariants/%d", invariantID)
}
