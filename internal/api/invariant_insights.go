package api

import (
	"context"
	"net/url"
	"strconv"
)

// WaiveCounts tallies waivers by reason. Every reason is present, zero when
// none.
type WaiveCounts struct {
	FalsePositive int `json:"false_positive"`
	DoesntApply   int `json:"doesnt_apply"`
	AcceptedRisk  int `json:"accepted_risk"`
	FixInFollowup int `json:"fix_in_followup"`
}

// InvariantSummary names an invariant inside an insights or waiver row.
type InvariantSummary struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Status   string `json:"status"`
}

// InvariantReview is the review session an invariant was applied on.
type InvariantReview struct {
	RunbookNumber int                 `json:"runbook_number"`
	URL           string              `json:"url"`
	Repositories  []Repository        `json:"repositories"`
	PullRequests  []LinkedPullRequest `json:"pull_requests"`
}

// InvariantUser is who waived an invariant.
type InvariantUser struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}

// InvariantWaiver is one waive of an invariant on a review. WaivedBy is nil
// when the user row is gone; Version is the invariant version the waiver was
// made on, nil when none was pinned.
type InvariantWaiver struct {
	Category      string         `json:"category"`
	Justification string         `json:"justification"`
	WaivedBy      *InvariantUser `json:"waived_by"`
	CreatedAt     string         `json:"created_at"`
	Version       *int           `json:"version"`
}

// InvariantInsightsQuery is the query string for
// GET /api/v1/invariants/insights. Since is an RFC 3339 instant, empty for
// all time; Limit caps each table; an empty Status means active.
type InvariantInsightsQuery struct {
	Since  string
	Repo   *Repository
	Limit  int
	Status string
}

// InvariantInsightsResponse ranks the account's invariants by how they are
// applied, failed and waived. Rates are nil when the denominator is zero.
type InvariantInsightsResponse struct {
	Since        *string                 `json:"since"`
	Status       string                  `json:"status"`
	MostApplied  []InvariantAppliedRow   `json:"most_applied"`
	FailureRatio []InvariantFailureRow   `json:"failure_ratio"`
	MostWaived   []InvariantWaivedRow    `json:"most_waived"`
	WaiveRate    []InvariantWaiveRateRow `json:"waive_rate"`
	NeverApplied InvariantNeverApplied   `json:"never_applied"`
	Reasons      InvariantWaiveReasons   `json:"reasons"`
}

// InvariantAppliedRow is a most-applied ranking entry.
type InvariantAppliedRow struct {
	Invariant    InvariantSummary `json:"invariant"`
	TimesApplied int              `json:"times_applied"`
	TimesFailed  int              `json:"times_failed"`
	TotalWaives  int              `json:"total_waives"`
}

// InvariantFailureRow is a failure-rate ranking entry.
type InvariantFailureRow struct {
	Invariant    InvariantSummary `json:"invariant"`
	TimesApplied int              `json:"times_applied"`
	TimesFailed  int              `json:"times_failed"`
	FailureRate  *float64         `json:"failure_rate"`
}

// InvariantWaivedRow is a most-waived ranking entry.
type InvariantWaivedRow struct {
	Invariant InvariantSummary `json:"invariant"`
	Total     int              `json:"total"`
	Counts    WaiveCounts      `json:"counts"`
}

// InvariantWaiveRateRow is a waive-rate ranking entry.
type InvariantWaiveRateRow struct {
	Invariant      InvariantSummary `json:"invariant"`
	TimesFailed    int              `json:"times_failed"`
	WaivedFailures int              `json:"waived_failures"`
	WaiveRate      *float64         `json:"waive_rate"`
}

// InvariantNeverApplied lists invariants no review has applied. Count is the
// whole set; Invariants is the first page of it, newest first.
type InvariantNeverApplied struct {
	Count      int                     `json:"count"`
	Invariants []NeverAppliedInvariant `json:"invariants"`
}

// NeverAppliedInvariant is an invariant summary with when it was created.
type NeverAppliedInvariant struct {
	InvariantSummary
	CreatedAt string `json:"created_at"`
}

// InvariantWaiveReasons tallies every waiver in the window by reason.
type InvariantWaiveReasons struct {
	Total  int         `json:"total"`
	Counts WaiveCounts `json:"counts"`
}

// GetInvariantQuery is the query string for GET /api/v1/invariants/<id>.
// ByRepo adds the by-repo breakdown.
type GetInvariantQuery struct {
	ByRepo bool
}

// InvariantDetail is one invariant with its all-time usage (on the embedded
// Usage).
type InvariantDetail struct {
	Invariant
	ByVersion []InvariantVersionUsage `json:"by_version"`
	// ByRepo is only present when the query asked for it.
	ByRepo []InvariantRepoUsage `json:"by_repo"`
}

// InvariantVersionUsage is the counts for one invariant version, newest
// first. Version and CreatedAt are nil for criteria and waivers with no
// version pinned.
type InvariantVersionUsage struct {
	Version   *int    `json:"version"`
	CreatedAt *string `json:"created_at"`
	InvariantUsage
}

// InvariantRepoUsage is the counts for one repository.
type InvariantRepoUsage struct {
	Repository Repository `json:"repository"`
	InvariantUsage
}

// AccountWaiversQuery filters and pages GET /api/v1/invariants/waivers, the
// account-wide listing. InvariantID narrows to one invariant; User is "me"
// (needs a user token) or an email on the account; Category is a waiver
// reason.
type AccountWaiversQuery struct {
	InvariantID int
	User        string
	Category    string
	Repo        *Repository
	Page        int
	PerPage     int
}

// InvariantWaiverRow is one waive with the invariant and the review it was
// made on.
type InvariantWaiverRow struct {
	ID int `json:"id"`
	InvariantWaiver
	Review    InvariantReview  `json:"review"`
	Invariant InvariantSummary `json:"invariant"`
}

// InvariantWaiversResponse is one page of waives, newest first.
type InvariantWaiversResponse struct {
	Waivers []InvariantWaiverRow `json:"waivers"`
	Page    int                  `json:"page"`
	PerPage int                  `json:"per_page"`
	HasMore bool                 `json:"has_more"`
}

// InvariantReviewsQuery filters and pages GET /api/v1/invariants/<id>/reviews.
// Outcome is one of passed, failed, warned, errored, waived, fixed, not_run.
type InvariantReviewsQuery struct {
	Repo    *Repository
	Outcome string
	Page    int
	PerPage int
}

// InvariantReviewRow is one review an invariant was applied on. Result is
// the verdict run's raw status for it (pass, fail, warn, error), nil when no
// run concluded on it; Outcome folds waivers and earlier failures in.
type InvariantReviewRow struct {
	Review     InvariantReview     `json:"review"`
	AppliedAt  string              `json:"applied_at"`
	Version    *int                `json:"version"`
	VerdictRun *InvariantReviewRun `json:"verdict_run"`
	Result     *string             `json:"result"`
	Outcome    string              `json:"outcome"`
	Waiver     *InvariantWaiver    `json:"waiver"`
}

// InvariantReviewRun is the run of the latest concluded verdict for the
// invariant, which may be older than the review's latest run.
type InvariantReviewRun struct {
	ID          int     `json:"id"`
	Status      string  `json:"status"`
	CompletedAt *string `json:"completed_at"`
}

// InvariantReviewsResponse is one page of reviews, newest applied first,
// with the invariant they are about.
type InvariantReviewsResponse struct {
	Invariant InvariantSummary     `json:"invariant"`
	Reviews   []InvariantReviewRow `json:"reviews"`
	Page      int                  `json:"page"`
	PerPage   int                  `json:"per_page"`
	HasMore   bool                 `json:"has_more"`
}

// GetInvariantInsights fetches the account-wide usage rankings.
func (c *Client) GetInvariantInsights(
	ctx context.Context, q InvariantInsightsQuery,
) (*InvariantInsightsResponse, error) {
	values := url.Values{}
	if q.Since != "" {
		values.Set("since", q.Since)
	}
	setRepo(values, q.Repo)
	if q.Limit > 0 {
		values.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Status != "" {
		values.Set("status", q.Status)
	}
	var out InvariantInsightsResponse
	if err := c.getJSON(ctx, "/api/v1/invariants/insights", values, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetInvariant fetches one invariant with its usage.
func (c *Client) GetInvariant(
	ctx context.Context, invariantID int, q GetInvariantQuery,
) (*InvariantDetail, error) {
	values := url.Values{}
	if q.ByRepo {
		values.Set("include", "by_repo")
	}
	var out InvariantDetail
	if err := c.getJSON(ctx, invariantPath(invariantID), values, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListInvariantReviews fetches one page of the reviews an invariant was
// applied on.
func (c *Client) ListInvariantReviews(
	ctx context.Context, invariantID int, q InvariantReviewsQuery,
) (*InvariantReviewsResponse, error) {
	values := url.Values{}
	setRepo(values, q.Repo)
	if q.Outcome != "" {
		values.Set("outcome", q.Outcome)
	}
	setPage(values, q.Page, q.PerPage)
	var out InvariantReviewsResponse
	if err := c.getJSON(ctx, invariantPath(invariantID)+"/reviews", values, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAccountWaivers fetches one page of the account's waives across every
// invariant.
func (c *Client) ListAccountWaivers(
	ctx context.Context, q AccountWaiversQuery,
) (*InvariantWaiversResponse, error) {
	values := url.Values{}
	if q.InvariantID > 0 {
		values.Set("invariant", strconv.Itoa(q.InvariantID))
	}
	if q.User != "" {
		values.Set("user", q.User)
	}
	if q.Category != "" {
		values.Set("category", q.Category)
	}
	setRepo(values, q.Repo)
	setPage(values, q.Page, q.PerPage)
	var out InvariantWaiversResponse
	if err := c.getJSON(ctx, "/api/v1/invariants/waivers", values, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
