package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// InvariantCondition narrows when an invariant applies. Type is one of the
// server's condition types (file_path_glob, language); Negate inverts the
// match.
type InvariantCondition struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Negate bool   `json:"negate"`
}

// OwnerTeam names a GitHub team by org and slug. Name is the display name the
// server returns; requests leave it empty.
type OwnerTeam struct {
	Org  string `json:"org"`
	Slug string `json:"slug"`
	Name string `json:"name,omitempty"`
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
	// OwnerTeams are the effective owners: the invariant's own teams, else the
	// account's default. Nil means the backend predates owner teams, which
	// tells an unsupported backend apart from an unowned invariant.
	OwnerTeams *[]OwnerTeam `json:"owner_teams"`
	CreatedAt  string       `json:"created_at"`
	ModifiedAt string       `json:"modified_at"`
}

// ListInvariantsQuery is the query string for GET /api/v1/invariants. A
// non-nil Repo narrows to the invariants that apply to that repo (account-scoped
// ones included); an empty Status lists every status; IDs and Sources narrow to
// those ids and provenance values. Empty filters are omitted rather than sent
// blank (the server reads a blank list as "no filter"), and Page and PerPage
// are sent only when positive, so the server's defaults hold otherwise.
// OwnerTeams (org/slug) and Unowned OR together and match the effective owner.
type ListInvariantsQuery struct {
	Repo       *Repository
	Status     string
	IDs        []int
	Sources    []string
	OwnerTeams []string
	Unowned    bool
	Page       int
	PerPage    int
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
// field is optional: nil leaves the current value alone. Repositories,
// Conditions and OwnerTeams replace the current set wholesale when non-nil, so
// a pointer to an empty slice clears them (an empty Repositories makes the
// invariant account-scoped, an empty OwnerTeams falls back to the account's
// default owner team).
type UpdateInvariantRequest struct {
	Title        *string               `json:"title,omitempty"`
	Body         *string               `json:"body,omitempty"`
	Category     *string               `json:"category,omitempty"`
	Repositories *[]Repository         `json:"repositories,omitempty"`
	Enabled      *bool                 `json:"enabled,omitempty"`
	Conditions   *[]InvariantCondition `json:"conditions,omitempty"`
	OwnerTeams   *[]OwnerTeam          `json:"owner_teams,omitempty"`
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

// SetInvariantOwnersRequest is the body for POST /api/v1/invariants/owners.
// OwnerTeams replaces each invariant's owners; empty falls back to the
// account's default owner team. The whole batch fails if the caller can't
// manage every id.
type SetInvariantOwnersRequest struct {
	InvariantIDs []int       `json:"invariant_ids"`
	OwnerTeams   []OwnerTeam `json:"owner_teams"`
}

// SetInvariantOwnersResponse carries the updated invariants.
type SetInvariantOwnersResponse struct {
	Invariants []Invariant `json:"invariants"`
}

// InvariantOwnerTeam is one of the account's teams with how many invariants
// it owns, counting those that fall back to it as the default.
type InvariantOwnerTeam struct {
	OwnerTeam
	InvariantCount int  `json:"invariant_count"`
	IsDefault      bool `json:"is_default"`
}

// ListInvariantOwnerTeamsQuery pages GET /api/v1/invariants/owner-teams;
// zero values leave the server's defaults.
type ListInvariantOwnerTeamsQuery struct {
	Page    int
	PerPage int
}

// ListInvariantOwnerTeamsResponse is one page of
// GET /api/v1/invariants/owner-teams, teams that own the most first.
// UnownedCount is the invariants with no owner and no default team to fall
// back to, the same on every page.
type ListInvariantOwnerTeamsResponse struct {
	OwnerTeams   []InvariantOwnerTeam `json:"owner_teams"`
	UnownedCount int                  `json:"unowned_count"`
	Page         int                  `json:"page"`
	PerPage      int                  `json:"per_page"`
	HasMore      bool                 `json:"has_more"`
}

// ListInvariants fetches one page of the account's baseline invariants.
func (c *Client) ListInvariants(
	ctx context.Context, q ListInvariantsQuery,
) (*ListInvariantsResponse, error) {
	var out ListInvariantsResponse
	if err := c.getJSON(ctx, "/api/v1/invariants", listInvariantsQuery(q), &out); err != nil {
		return nil, err
	}
	return &out, nil
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
	if len(q.OwnerTeams) > 0 {
		values.Set("owner_team", strings.Join(q.OwnerTeams, ","))
	}
	if q.Unowned {
		values.Set("unowned", "true")
	}
	setPage(values, q.Page, q.PerPage)
	return values
}

func setPage(values url.Values, page, perPage int) {
	if page > 0 {
		values.Set("page", strconv.Itoa(page))
	}
	if perPage > 0 {
		values.Set("per_page", strconv.Itoa(perPage))
	}
}

// ListInvariantCategories fetches the account's invariant categories.
func (c *Client) ListInvariantCategories(
	ctx context.Context,
) (*ListInvariantCategoriesResponse, error) {
	var out ListInvariantCategoriesResponse
	if err := c.getJSON(ctx, "/api/v1/invariants/categories", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListInvariantOwnerTeams fetches one page of the account's teams with their
// invariant counts — the org/slug names the owner filters and edits take.
func (c *Client) ListInvariantOwnerTeams(
	ctx context.Context, q ListInvariantOwnerTeamsQuery,
) (*ListInvariantOwnerTeamsResponse, error) {
	var out ListInvariantOwnerTeamsResponse
	values := url.Values{}
	setPage(values, q.Page, q.PerPage)
	if err := c.getJSON(ctx, "/api/v1/invariants/owner-teams", values, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateInvariant creates a baseline invariant. Requires a maintainer's user
// access token.
func (c *Client) CreateInvariant(
	ctx context.Context, req CreateInvariantRequest,
) (*Invariant, error) {
	var out Invariant
	if err := c.postJSON(ctx, "/api/v1/invariants", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateInvariant patches the given fields of an invariant.
func (c *Client) UpdateInvariant(
	ctx context.Context, invariantID int, req UpdateInvariantRequest,
) (*Invariant, error) {
	var out Invariant
	if err := c.patchJSON(ctx, invariantPath(invariantID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteInvariant removes an invariant and its conditions. To keep the row
// while taking it out of service, reject it via SetInvariantStatus instead.
func (c *Client) DeleteInvariant(
	ctx context.Context, invariantID int,
) (*DeleteInvariantResponse, error) {
	var out DeleteInvariantResponse
	if err := c.deleteJSON(ctx, invariantPath(invariantID), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetInvariantStatus approves, rejects, or restores invariants in bulk.
func (c *Client) SetInvariantStatus(
	ctx context.Context, req SetInvariantStatusRequest,
) (*SetInvariantStatusResponse, error) {
	var out SetInvariantStatusResponse
	if err := c.postJSON(ctx, "/api/v1/invariants/status", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetInvariantOwners replaces the owner teams of invariants in bulk.
func (c *Client) SetInvariantOwners(
	ctx context.Context, req SetInvariantOwnersRequest,
) (*SetInvariantOwnersResponse, error) {
	var out SetInvariantOwnersResponse
	if err := c.postJSON(ctx, "/api/v1/invariants/owners", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func invariantPath(invariantID int) string {
	return fmt.Sprintf("/api/v1/invariants/%d", invariantID)
}
