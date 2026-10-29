package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
)

// waiverTitleWidth is how much of a title the account-wide waivers table
// shows, since it sits mid-row with several columns after it.
const waiverTitleWidth = 40

// formatInvariantInsights renders the ranking tables under one header that
// says what they cover.
func formatInvariantInsights(resp *api.InvariantInsightsResponse, window, repo, seeAll string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Invariant insights · %s invariants · %s · %s\n", resp.Status, window, repo)

	b.WriteString("\nMost applied\n")
	rows := make([][]string, 0, len(resp.MostApplied))
	for _, r := range resp.MostApplied {
		rows = append(rows, []string{
			"#" + strconv.Itoa(r.Invariant.ID), strconv.Itoa(r.TimesApplied), strconv.Itoa(r.TimesFailed),
			strconv.Itoa(r.TotalWaives), r.Invariant.Title,
		})
	}
	writeTable(&b, "", []string{"ID", "APPLIED", "FAILED", "WAIVED", "TITLE"}, rows)

	b.WriteString("\nHighest failure rate\n")
	rows = rows[:0]
	for _, r := range resp.FailureRatio {
		rows = append(rows, []string{
			"#" + strconv.Itoa(r.Invariant.ID), strconv.Itoa(r.TimesApplied), strconv.Itoa(r.TimesFailed),
			formatRate(r.FailureRate), r.Invariant.Title,
		})
	}
	writeTable(&b, "", []string{"ID", "APPLIED", "FAILED", "RATE", "TITLE"}, rows)

	b.WriteString("\nMost waived, by reason\n")
	rows = rows[:0]
	for _, r := range resp.MostWaived {
		rows = append(rows, []string{
			"#" + strconv.Itoa(r.Invariant.ID), strconv.Itoa(r.Total), strconv.Itoa(r.Counts.FalsePositive),
			strconv.Itoa(r.Counts.DoesntApply), strconv.Itoa(r.Counts.AcceptedRisk),
			strconv.Itoa(r.Counts.FixInFollowup), r.Invariant.Title,
		})
	}
	writeTable(&b, "", []string{"ID", "TOTAL", "FALSE+", "N/A", "RISK", "FOLLOWUP", "TITLE"}, rows)

	b.WriteString("\nHighest waive rate\n")
	rows = rows[:0]
	for _, r := range resp.WaiveRate {
		rows = append(rows, []string{
			"#" + strconv.Itoa(r.Invariant.ID), strconv.Itoa(r.TimesFailed), strconv.Itoa(r.WaivedFailures),
			formatRate(r.WaiveRate), r.Invariant.Title,
		})
	}
	writeTable(&b, "", []string{"ID", "FAILED", "WAIVED", "RATE", "TITLE"}, rows)

	fmt.Fprintf(&b, "\nNever applied (%d)\n", resp.NeverApplied.Count)
	rows = rows[:0]
	for _, inv := range resp.NeverApplied.Invariants {
		rows = append(rows, []string{"#" + strconv.Itoa(inv.ID), formatDate(inv.CreatedAt), inv.Title})
	}
	writeTable(&b, "", []string{"ID", "CREATED", "TITLE"}, rows)
	if resp.NeverApplied.Count > len(resp.NeverApplied.Invariants) {
		b.WriteString("See all: " + seeAll + "\n")
	}

	fmt.Fprintf(&b, "\nWaiver reasons: %d · %s\n", resp.Reasons.Total, formatWaiveCounts(resp.Reasons.Counts))
	return b.String()
}

// formatInvariantShow renders one invariant's fields, its all-time counts,
// the per-version and per-repo breakdowns, and a page of waivers.
func formatInvariantShow(d *api.InvariantDetail, waivers *api.InvariantWaiversResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n", d.ID, d.Title)
	b.WriteString(formatInvariantFields(&d.Invariant))

	var u api.InvariantUsage
	if d.Usage != nil {
		u = *d.Usage
	}
	b.WriteString("\nUsage · all time\n")
	fmt.Fprintf(&b, "  Applied %d · Failed %d (%s) · Waived %d of %d failures (%s)\n",
		u.TimesApplied, u.TimesFailed, formatRate(u.FailureRate),
		u.WaivedFailures, u.TimesFailed, formatRate(u.WaiveRate))
	if u.TotalWaives > 0 {
		fmt.Fprintf(&b, "  Waived as: %s\n", formatWaiveCounts(u.WaivesByCategory))
	}

	b.WriteString("\nBy version\n")
	rows := make([][]string, 0, len(d.ByVersion))
	for _, v := range d.ByVersion {
		since := "before"
		if v.CreatedAt != nil {
			since = formatDate(*v.CreatedAt)
		}
		rows = append(rows, []string{
			formatOptionalInt(v.Version), since, strconv.Itoa(v.TimesApplied), strconv.Itoa(v.TimesFailed),
			formatRate(v.FailureRate), strconv.Itoa(v.TotalWaives),
		})
	}
	writeTable(&b, "  ", []string{"VERSION", "SINCE", "APPLIED", "FAILED", "RATE", "WAIVED"}, rows)

	if d.ByRepo != nil {
		b.WriteString("\nBy repo\n")
		rows = rows[:0]
		for _, r := range d.ByRepo {
			rows = append(rows, []string{
				r.Repository.Org + "/" + r.Repository.Name, strconv.Itoa(r.TimesApplied),
				strconv.Itoa(r.TimesFailed), formatRate(r.FailureRate), strconv.Itoa(r.TotalWaives),
			})
		}
		writeTable(&b, "  ", []string{"REPO", "APPLIED", "FAILED", "RATE", "WAIVED"}, rows)
	}

	fmt.Fprintf(&b, "\nWaivers (%d)\n", u.TotalWaives)
	if len(waivers.Waivers) == 0 && waivers.Page > 1 {
		b.WriteString("  " + emptyPageMessage(waivers.Page, "waivers"))
	} else {
		header, rows := waiverTable(waivers.Waivers, false)
		writeTable(&b, "  ", header, rows)
	}
	if waivers.HasMore {
		fmt.Fprintf(&b, "  See more waivers --page %d\n", max(waivers.Page, 1)+1)
	}
	return b.String()
}

// formatInvariantReviews renders the reviews an invariant was applied on
// under its title.
func formatInvariantReviews(resp *api.InvariantReviewsResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n", resp.Invariant.ID, resp.Invariant.Title)
	if len(resp.Reviews) == 0 {
		b.WriteString(emptyPageMessage(resp.Page, "reviews"))
		return b.String()
	}
	rows := make([][]string, 0, len(resp.Reviews))
	for i := range resp.Reviews {
		r := &resp.Reviews[i]
		rows = append(rows, []string{
			formatDate(r.AppliedAt), formatRunbookID(r.Review.RunbookNumber), formatReviewPR(&r.Review),
			r.Outcome, formatOptionalInt(r.Version), formatReviewNote(r),
		})
	}
	writeTable(&b, "", []string{"WHEN", "REVIEW", "PR", "OUTCOME", "VERSION", "NOTE"}, rows)
	if resp.HasMore {
		fmt.Fprintf(&b, "See more reviews --page %d\n", max(resp.Page, 1)+1)
	}
	return b.String()
}

// formatReviewNote explains an outcome that a run status alone does not: who
// waived it and why, or which run a fixed invariant passed on.
func formatReviewNote(r *api.InvariantReviewRow) string {
	switch r.Outcome {
	case "waived":
		if r.Waiver == nil {
			return ""
		}
		return waiveCategoryLabel(r.Waiver.Category) + ", " + formatWaivedBy(r.Waiver.WaivedBy)
	case "fixed":
		if r.VerdictRun == nil {
			return "failed earlier run"
		}
		return fmt.Sprintf("failed earlier run, passed run %d", r.VerdictRun.ID)
	}
	return ""
}

// formatAccountWaivers renders the account-wide waivers, one per row.
func formatAccountWaivers(resp *api.InvariantWaiversResponse) string {
	if len(resp.Waivers) == 0 {
		return emptyPageMessage(resp.Page, "waivers")
	}
	var b strings.Builder
	header, rows := waiverTable(resp.Waivers, true)
	writeTable(&b, "", header, rows)
	if resp.HasMore {
		fmt.Fprintf(&b, "See more waivers --page %d\n", max(resp.Page, 1)+1)
	}
	return b.String()
}

// waiverTable is the waivers table `show` and `waivers` share; the
// account-wide one adds which invariant each waiver is on.
func waiverTable(waivers []api.InvariantWaiverRow, withInvariant bool) ([]string, [][]string) {
	header := []string{"WHEN", "REVIEW", "PR", "REASON", "BY", "JUSTIFICATION"}
	if withInvariant {
		header = slices.Insert(header, 1, "INVARIANT")
	}
	rows := make([][]string, 0, len(waivers))
	for i := range waivers {
		w := &waivers[i]
		row := []string{
			formatDate(w.CreatedAt), formatRunbookID(w.Review.RunbookNumber), formatReviewPR(&w.Review),
			waiveCategoryLabel(w.Category), formatWaivedBy(w.WaivedBy), formatJustification(w.Justification),
		}
		if withInvariant {
			row = slices.Insert(row, 1, fmt.Sprintf("#%d %s", w.Invariant.ID, truncateTitle(w.Invariant.Title, waiverTitleWidth)))
		}
		rows = append(rows, row)
	}
	return header, rows
}

func emptyPageMessage(page int, what string) string {
	if page > 1 {
		return fmt.Sprintf("Nothing on page %d, the listing ends before it. Try a lower --page.\n", page)
	}
	return fmt.Sprintf("No %s match.\n", what)
}

// writeTable renders header and rows through one tabwriter, each line
// prefixed by indent, or one faint "none" when there are no rows.
func writeTable(b *strings.Builder, indent string, header []string, rows [][]string) {
	if len(rows) == 0 {
		b.WriteString(indent + colors.Faint("none") + "\n")
		return
	}
	w := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, indent+strings.Join(header, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, indent+strings.Join(row, "\t"))
	}
	_ = w.Flush()
}

// formatRate renders a 0..1 ratio as a whole percent, or a dash when the
// server had nothing to divide by.
func formatRate(rate *float64) string {
	if rate == nil {
		return orDash("")
	}
	return strconv.Itoa(int(math.Round(*rate*100))) + "%"
}

// formatDate keeps the day of an RFC 3339 timestamp, as the server wrote it.
func formatDate(ts string) string {
	if ts == "" {
		return orDash(ts)
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Format(time.DateOnly)
}

func formatOptionalInt(n *int) string {
	if n == nil {
		return orDash("")
	}
	return strconv.Itoa(*n)
}

// waiveCategoryLabel spells a waiver reason out for the tables.
func waiveCategoryLabel(category string) string {
	switch category {
	case "false_positive":
		return "false positive"
	case "doesnt_apply":
		return "doesn't apply"
	case "accepted_risk":
		return "accepted risk"
	case "fix_in_followup":
		return "fix in follow-up"
	}
	return strings.ReplaceAll(category, "_", " ")
}

func formatWaiveCounts(c api.WaiveCounts) string {
	return fmt.Sprintf("false positive %d · doesn't apply %d · accepted risk %d · fix in follow-up %d",
		c.FalsePositive, c.DoesntApply, c.AcceptedRisk, c.FixInFollowup)
}

// formatWaivedBy names the user, by email when they have no name; nil means
// the user row is gone.
func formatWaivedBy(u *api.InvariantUser) string {
	switch {
	case u == nil:
		return orDash("")
	case u.FullName != "":
		return u.FullName
	}
	return orDash(u.Email)
}

// formatReviewPR renders the review's PRs as #N,#M, prefixed with the repo
// when the review covers just one.
func formatReviewPR(r *api.InvariantReview) string {
	prs := formatLinkedPRs(r.PullRequests)
	if len(r.PullRequests) == 0 || len(r.Repositories) != 1 {
		return prs
	}
	return r.Repositories[0].Org + "/" + r.Repositories[0].Name + prs
}

func formatJustification(s string) string {
	return orDash(strings.TrimSpace(strings.ReplaceAll(s, "\n", " ")))
}

// truncateTitle cuts a title to about width runes at a word boundary and
// marks the cut.
func truncateTitle(title string, width int) string {
	if utf8.RuneCountInString(title) <= width {
		return title
	}
	runes := []rune(title)
	cut := string(runes[:width])
	if i := strings.LastIndex(cut, " "); i > width/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ") + "…"
}

type invariantSummaryJSON struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Status   string `json:"status"`
}

type waiveCountsJSON struct {
	FalsePositive int `json:"false_positive"`
	DoesntApply   int `json:"doesnt_apply"`
	AcceptedRisk  int `json:"accepted_risk"`
	FixInFollowup int `json:"fix_in_followup"`
}

type insightsJSON struct {
	Since        *string                `json:"since"`
	Status       string                 `json:"status"`
	MostApplied  []insightAppliedJSON   `json:"most_applied"`
	FailureRatio []insightFailureJSON   `json:"failure_ratio"`
	MostWaived   []insightWaivedJSON    `json:"most_waived"`
	WaiveRate    []insightWaiveRateJSON `json:"waive_rate"`
	NeverApplied neverAppliedJSON       `json:"never_applied"`
	Reasons      waiveReasonsJSON       `json:"reasons"`
}

type insightAppliedJSON struct {
	Invariant    invariantSummaryJSON `json:"invariant"`
	TimesApplied int                  `json:"times_applied"`
	TimesFailed  int                  `json:"times_failed"`
	TotalWaives  int                  `json:"total_waives"`
}

type insightFailureJSON struct {
	Invariant    invariantSummaryJSON `json:"invariant"`
	TimesApplied int                  `json:"times_applied"`
	TimesFailed  int                  `json:"times_failed"`
	FailureRate  *float64             `json:"failure_rate"`
}

type insightWaivedJSON struct {
	Invariant invariantSummaryJSON `json:"invariant"`
	Total     int                  `json:"total"`
	Counts    waiveCountsJSON      `json:"counts"`
}

type insightWaiveRateJSON struct {
	Invariant      invariantSummaryJSON `json:"invariant"`
	TimesFailed    int                  `json:"times_failed"`
	WaivedFailures int                  `json:"waived_failures"`
	WaiveRate      *float64             `json:"waive_rate"`
}

type neverAppliedJSON struct {
	Count      int                         `json:"count"`
	Invariants []neverAppliedInvariantJSON `json:"invariants"`
}

type neverAppliedInvariantJSON struct {
	invariantSummaryJSON
	CreatedAt string `json:"created_at"`
}

type waiveReasonsJSON struct {
	Total  int             `json:"total"`
	Counts waiveCountsJSON `json:"counts"`
}

// invariantShowJSON is `show --json`: the invariant with its all-time usage
// (in the embedded row's usage), the per-version rows, the per-repo rows
// when asked for, and a page of waivers.
type invariantShowJSON struct {
	invariantJSON
	ByVersion      []versionUsageJSON `json:"by_version"`
	ByRepo         *[]repoUsageJSON   `json:"by_repo,omitempty"`
	Waivers        []waiverRowJSON    `json:"waivers"`
	WaiversHasMore bool               `json:"waivers_has_more"`
}

type versionUsageJSON struct {
	Version   *int    `json:"version"`
	CreatedAt *string `json:"created_at"`
	invariantUsageJSON
}

type repoUsageJSON struct {
	Repository string `json:"repository"`
	invariantUsageJSON
}

// reviewRefJSON identifies the review a row is about, the way every other
// --json names sessions.
type reviewRefJSON struct {
	sessionRef
	Repositories []string          `json:"repositories"`
	PullRequests []pullRequestJSON `json:"pull_requests"`
}

type userJSON struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}

type waiverJSON struct {
	Category      string    `json:"category"`
	Justification string    `json:"justification"`
	WaivedBy      *userJSON `json:"waived_by"`
	CreatedAt     string    `json:"created_at"`
	Version       *int      `json:"version"`
}

type waiverRowJSON struct {
	ID int `json:"id"`
	waiverJSON
	Invariant invariantSummaryJSON `json:"invariant"`
	Review    reviewRefJSON        `json:"review"`
}

type waiversJSON struct {
	Waivers []waiverRowJSON `json:"waivers"`
	HasMore bool            `json:"has_more"`
}

type reviewsJSON struct {
	Invariant invariantSummaryJSON `json:"invariant"`
	Reviews   []reviewRowJSON      `json:"reviews"`
	HasMore   bool                 `json:"has_more"`
}

type reviewRowJSON struct {
	Review     reviewRefJSON  `json:"review"`
	AppliedAt  string         `json:"applied_at"`
	Version    *int           `json:"version"`
	VerdictRun *reviewRunJSON `json:"verdict_run"`
	Result     *string        `json:"result"`
	Outcome    string         `json:"outcome"`
	Waiver     *waiverJSON    `json:"waiver"`
}

type reviewRunJSON struct {
	RunID       int     `json:"run_id"`
	Status      string  `json:"status"`
	CompletedAt *string `json:"completed_at"`
}

func newInvariantSummaryJSON(s *api.InvariantSummary) invariantSummaryJSON {
	return invariantSummaryJSON{ID: s.ID, Title: s.Title, Category: s.Category, Status: s.Status}
}

func newWaiveCountsJSON(c api.WaiveCounts) waiveCountsJSON {
	return waiveCountsJSON{
		FalsePositive: c.FalsePositive,
		DoesntApply:   c.DoesntApply,
		AcceptedRisk:  c.AcceptedRisk,
		FixInFollowup: c.FixInFollowup,
	}
}

func newInsightsJSON(resp *api.InvariantInsightsResponse) insightsJSON {
	out := insightsJSON{
		Since:        resp.Since,
		Status:       resp.Status,
		MostApplied:  make([]insightAppliedJSON, 0, len(resp.MostApplied)),
		FailureRatio: make([]insightFailureJSON, 0, len(resp.FailureRatio)),
		MostWaived:   make([]insightWaivedJSON, 0, len(resp.MostWaived)),
		WaiveRate:    make([]insightWaiveRateJSON, 0, len(resp.WaiveRate)),
		NeverApplied: neverAppliedJSON{
			Count:      resp.NeverApplied.Count,
			Invariants: make([]neverAppliedInvariantJSON, 0, len(resp.NeverApplied.Invariants)),
		},
		Reasons: waiveReasonsJSON{Total: resp.Reasons.Total, Counts: newWaiveCountsJSON(resp.Reasons.Counts)},
	}
	for i := range resp.MostApplied {
		r := &resp.MostApplied[i]
		out.MostApplied = append(out.MostApplied, insightAppliedJSON{
			Invariant: newInvariantSummaryJSON(&r.Invariant), TimesApplied: r.TimesApplied,
			TimesFailed: r.TimesFailed, TotalWaives: r.TotalWaives,
		})
	}
	for i := range resp.FailureRatio {
		r := &resp.FailureRatio[i]
		out.FailureRatio = append(out.FailureRatio, insightFailureJSON{
			Invariant: newInvariantSummaryJSON(&r.Invariant), TimesApplied: r.TimesApplied,
			TimesFailed: r.TimesFailed, FailureRate: r.FailureRate,
		})
	}
	for i := range resp.MostWaived {
		r := &resp.MostWaived[i]
		out.MostWaived = append(out.MostWaived, insightWaivedJSON{
			Invariant: newInvariantSummaryJSON(&r.Invariant), Total: r.Total, Counts: newWaiveCountsJSON(r.Counts),
		})
	}
	for i := range resp.WaiveRate {
		r := &resp.WaiveRate[i]
		out.WaiveRate = append(out.WaiveRate, insightWaiveRateJSON{
			Invariant: newInvariantSummaryJSON(&r.Invariant), TimesFailed: r.TimesFailed,
			WaivedFailures: r.WaivedFailures, WaiveRate: r.WaiveRate,
		})
	}
	for i := range resp.NeverApplied.Invariants {
		inv := &resp.NeverApplied.Invariants[i]
		out.NeverApplied.Invariants = append(out.NeverApplied.Invariants, neverAppliedInvariantJSON{
			invariantSummaryJSON: newInvariantSummaryJSON(&inv.InvariantSummary), CreatedAt: inv.CreatedAt,
		})
	}
	return out
}

func newInvariantShowJSON(d *api.InvariantDetail, waivers *api.InvariantWaiversResponse) invariantShowJSON {
	out := invariantShowJSON{
		invariantJSON:  newInvariantJSON(&d.Invariant),
		ByVersion:      make([]versionUsageJSON, 0, len(d.ByVersion)),
		Waivers:        newWaiverRowsJSON(waivers.Waivers),
		WaiversHasMore: waivers.HasMore,
	}
	for i := range d.ByVersion {
		v := &d.ByVersion[i]
		out.ByVersion = append(out.ByVersion, versionUsageJSON{
			Version: v.Version, CreatedAt: v.CreatedAt, invariantUsageJSON: newInvariantUsageJSON(&v.InvariantUsage),
		})
	}
	if d.ByRepo != nil {
		repos := make([]repoUsageJSON, 0, len(d.ByRepo))
		for i := range d.ByRepo {
			r := &d.ByRepo[i]
			repos = append(repos, repoUsageJSON{
				Repository:         r.Repository.Org + "/" + r.Repository.Name,
				invariantUsageJSON: newInvariantUsageJSON(&r.InvariantUsage),
			})
		}
		out.ByRepo = &repos
	}
	return out
}

func newReviewRefJSON(r *api.InvariantReview) reviewRefJSON {
	out := reviewRefJSON{
		sessionRef:   newSessionRef(r.RunbookNumber, r.URL),
		Repositories: make([]string, 0, len(r.Repositories)),
		PullRequests: make([]pullRequestJSON, 0, len(r.PullRequests)),
	}
	for _, repo := range r.Repositories {
		out.Repositories = append(out.Repositories, repo.Org+"/"+repo.Name)
	}
	for _, pr := range r.PullRequests {
		out.PullRequests = append(out.PullRequests, pullRequestJSON{Number: pr.Number, URL: pr.URL})
	}
	return out
}

func newWaiverJSON(w *api.InvariantWaiver) waiverJSON {
	out := waiverJSON{
		Category:      w.Category,
		Justification: w.Justification,
		CreatedAt:     w.CreatedAt,
		Version:       w.Version,
	}
	if w.WaivedBy != nil {
		out.WaivedBy = &userJSON{Email: w.WaivedBy.Email, FullName: w.WaivedBy.FullName}
	}
	return out
}

func newWaiverRowsJSON(rows []api.InvariantWaiverRow) []waiverRowJSON {
	out := make([]waiverRowJSON, 0, len(rows))
	for i := range rows {
		w := &rows[i]
		out = append(out, waiverRowJSON{
			ID: w.ID, waiverJSON: newWaiverJSON(&w.InvariantWaiver),
			Invariant: newInvariantSummaryJSON(&w.Invariant), Review: newReviewRefJSON(&w.Review),
		})
	}
	return out
}

func newInvariantWaiversJSON(resp *api.InvariantWaiversResponse) waiversJSON {
	return waiversJSON{Waivers: newWaiverRowsJSON(resp.Waivers), HasMore: resp.HasMore}
}

func newInvariantReviewsJSON(resp *api.InvariantReviewsResponse) reviewsJSON {
	out := reviewsJSON{
		Invariant: newInvariantSummaryJSON(&resp.Invariant),
		Reviews:   make([]reviewRowJSON, 0, len(resp.Reviews)),
		HasMore:   resp.HasMore,
	}
	for i := range resp.Reviews {
		r := &resp.Reviews[i]
		row := reviewRowJSON{
			Review:    newReviewRefJSON(&r.Review),
			AppliedAt: r.AppliedAt,
			Version:   r.Version,
			Result:    r.Result,
			Outcome:   r.Outcome,
		}
		if r.VerdictRun != nil {
			row.VerdictRun = &reviewRunJSON{RunID: r.VerdictRun.ID, Status: r.VerdictRun.Status, CompletedAt: r.VerdictRun.CompletedAt}
		}
		if r.Waiver != nil {
			waiver := newWaiverJSON(r.Waiver)
			row.Waiver = &waiver
		}
		out.Reviews = append(out.Reviews, row)
	}
	return out
}
