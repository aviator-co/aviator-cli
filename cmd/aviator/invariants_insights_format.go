package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
)

// emptyCell is what an absent value prints as in the analytics tables.
const emptyCell = "—"

// waiverTitleWidth is how much of a title the account-wide waivers table
// shows, since it sits mid-row with several columns after it.
const waiverTitleWidth = 40

// formatInvariantInsights renders the ranking tables under one header that
// says what they cover.
func formatInvariantInsights(resp *api.InvariantInsightsResponse, window, repo string) string {
	var b strings.Builder
	status := "all"
	if resp.Status != "" {
		status = resp.Status
	}
	fmt.Fprintf(&b, "Invariant insights · %s invariants · %s · %s\n", status, window, repo)

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
		b.WriteString("See all: aviator invariants list --usage --never-applied\n")
	}

	fmt.Fprintf(&b, "\nWaiver reasons: %d · %s\n", resp.Reasons.Total, formatWaiveCounts(resp.Reasons.Counts))
	return b.String()
}

// formatInvariantShow renders one invariant's fields, its counts over the
// window, the per-version and per-repo breakdowns, and a page of waives.
func formatInvariantShow(d *api.InvariantDetail, waivers *api.InvariantWaiversResponse, window string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n", d.ID, d.Title)
	b.WriteString(formatInvariantFields(&d.Invariant))

	u := d.Usage
	fmt.Fprintf(&b, "\nUsage · %s\n", window)
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

	fmt.Fprintf(&b, "\nWaives (%d)\n", u.TotalWaives)
	rows = rows[:0]
	for i := range waivers.Waivers {
		w := &waivers.Waivers[i]
		rows = append(rows, []string{
			formatDate(w.CreatedAt), formatRunbookID(w.Review.ReviewNumber), formatReviewPR(&w.Review),
			waiveCategoryLabel(w.Category), formatWaivedBy(w.WaivedBy), formatJustification(w.Justification),
		})
	}
	writeTable(&b, "  ", []string{"WHEN", "REVIEW", "PR", "REASON", "BY", "JUSTIFICATION"}, rows)
	if waivers.HasMore {
		fmt.Fprintf(&b, "  See more waives --page %d\n", max(waivers.Page, 1)+1)
	}
	return b.String()
}

// formatInvariantReviews renders the reviews an invariant was applied on
// under its title; a nil invariant (gone since the rows were fetched) leaves
// just the id.
func formatInvariantReviews(id int, inv *api.Invariant, resp *api.InvariantReviewsResponse) string {
	var b strings.Builder
	if inv != nil {
		fmt.Fprintf(&b, "#%d %s\n", inv.ID, inv.Title)
	} else {
		fmt.Fprintf(&b, "#%d\n", id)
	}
	if len(resp.Reviews) == 0 {
		b.WriteString(emptyPageMessage(resp.Page, "reviews"))
		return b.String()
	}
	rows := make([][]string, 0, len(resp.Reviews))
	for i := range resp.Reviews {
		r := &resp.Reviews[i]
		rows = append(rows, []string{
			formatDate(r.AppliedAt), formatRunbookID(r.Review.ReviewNumber), formatReviewPR(&r.Review),
			r.Outcome, formatOptionalInt(r.Version), formatReviewNote(r),
		})
	}
	writeTable(&b, "", []string{"WHEN", "REVIEW", "PR", "OUTCOME", "VERSION", "NOTE"}, rows)
	if resp.HasMore {
		fmt.Fprintf(&b, "See more --page %d\n", max(resp.Page, 1)+1)
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
		if r.LatestRun == nil {
			return "failed earlier run"
		}
		return fmt.Sprintf("failed earlier run, passed run %d", r.LatestRun.ID)
	}
	return ""
}

// formatAccountWaivers renders the account-wide waives, one per row.
func formatAccountWaivers(resp *api.InvariantWaiversResponse) string {
	if len(resp.Waivers) == 0 {
		return emptyPageMessage(resp.Page, "waives")
	}
	var b strings.Builder
	rows := make([][]string, 0, len(resp.Waivers))
	for i := range resp.Waivers {
		w := &resp.Waivers[i]
		invariant := emptyCell
		if w.Invariant != nil {
			invariant = fmt.Sprintf("#%d %s", w.Invariant.ID, truncateTitle(w.Invariant.Title, waiverTitleWidth))
		}
		rows = append(rows, []string{
			formatDate(w.CreatedAt), invariant, formatRunbookID(w.Review.ReviewNumber), formatReviewPR(&w.Review),
			formatWaivedBy(w.WaivedBy), waiveCategoryLabel(w.Category), formatJustification(w.Justification),
		})
	}
	writeTable(&b, "", []string{"WHEN", "INVARIANT", "REVIEW", "PR", "BY", "REASON", "JUSTIFICATION"}, rows)
	if resp.HasMore {
		fmt.Fprintf(&b, "See more --page %d\n", max(resp.Page, 1)+1)
	}
	return b.String()
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

// formatRate renders a 0..1 ratio as a whole percent, or the empty cell when
// the server had nothing to divide by.
func formatRate(rate *float64) string {
	if rate == nil {
		return emptyCell
	}
	return strconv.Itoa(int(math.Round(*rate*100))) + "%"
}

// formatDate keeps the day of an RFC 3339 timestamp, as the server wrote it.
func formatDate(ts string) string {
	if ts == "" {
		return emptyCell
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Format(time.DateOnly)
}

func formatOptionalInt(n *int) string {
	if n == nil {
		return emptyCell
	}
	return strconv.Itoa(*n)
}

// waiveCategoryLabel is the reason as `aviator dismiss` names it in prose.
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

// formatWaivedBy names the user, by email when they have no name, and marks
// a deleted one.
func formatWaivedBy(u *api.InvariantUser) string {
	switch {
	case u == nil:
		return emptyCell
	case u.FullName != "":
		return u.FullName
	case u.Email != "":
		return u.Email
	}
	return emptyCell
}

// formatReviewPR renders the review's first PR as owner/repo#N, taking the
// repo from the PR's own URL since a review can span several.
func formatReviewPR(r *api.InvariantReview) string {
	if len(r.PullRequests) == 0 {
		return emptyCell
	}
	pr := r.PullRequests[0]
	_, path, _ := strings.Cut(pr.URL, "://")
	if parts := strings.Split(path, "/"); len(parts) >= 5 && parts[3] == "pull" {
		return fmt.Sprintf("%s/%s#%d", parts[1], parts[2], pr.Number)
	}
	if len(r.Repositories) > 0 {
		return fmt.Sprintf("%s/%s#%d", r.Repositories[0].Org, r.Repositories[0].Name, pr.Number)
	}
	return fmt.Sprintf("#%d", pr.Number)
}

func formatJustification(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return emptyCell
	}
	return s
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

// invariantShowJSON is `show --json`: the invariant with its counts, the
// per-version rows, the per-repo rows when asked for, and a page of waives.
type invariantShowJSON struct {
	invariantJSON
	Usage          usageDetailJSON    `json:"usage"`
	ByVersion      []versionUsageJSON `json:"by_version"`
	ByRepo         *[]repoUsageJSON   `json:"by_repo,omitempty"`
	Waivers        []waiverRowJSON    `json:"waivers"`
	WaiversHasMore bool               `json:"waivers_has_more"`
}

type usageDetailJSON struct {
	Since            *string         `json:"since"`
	TimesApplied     int             `json:"times_applied"`
	TimesFailed      int             `json:"times_failed"`
	FailureRate      *float64        `json:"failure_rate"`
	TotalWaives      int             `json:"total_waives"`
	WaivedFailures   int             `json:"waived_failures"`
	WaiveRate        *float64        `json:"waive_rate"`
	WaivesByCategory waiveCountsJSON `json:"waives_by_category"`
}

type versionUsageJSON struct {
	Version          *int            `json:"version"`
	CreatedAt        *string         `json:"created_at"`
	TimesApplied     int             `json:"times_applied"`
	TimesFailed      int             `json:"times_failed"`
	FailureRate      *float64        `json:"failure_rate"`
	TotalWaives      int             `json:"total_waives"`
	WaivesByCategory waiveCountsJSON `json:"waives_by_category"`
}

type repoUsageJSON struct {
	Repository   string   `json:"repository"`
	TimesApplied int      `json:"times_applied"`
	TimesFailed  int      `json:"times_failed"`
	FailureRate  *float64 `json:"failure_rate"`
	TotalWaives  int      `json:"total_waives"`
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
	Invariant *invariantSummaryJSON `json:"invariant,omitempty"`
	Review    reviewRefJSON         `json:"review"`
}

type waiversJSON struct {
	Waivers []waiverRowJSON `json:"waivers"`
	HasMore bool            `json:"has_more"`
}

type reviewsJSON struct {
	Invariant *invariantSummaryJSON `json:"invariant"`
	Reviews   []reviewRowJSON       `json:"reviews"`
	HasMore   bool                  `json:"has_more"`
}

type reviewRowJSON struct {
	Review    reviewRefJSON  `json:"review"`
	AppliedAt string         `json:"applied_at"`
	Version   *int           `json:"version"`
	LatestRun *reviewRunJSON `json:"latest_run"`
	Result    *string        `json:"result"`
	Outcome   string         `json:"outcome"`
	Waiver    *waiverJSON    `json:"waiver"`
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
	u := d.Usage
	out := invariantShowJSON{
		invariantJSON: newInvariantJSON(&d.Invariant),
		Usage: usageDetailJSON{
			Since: u.Since, TimesApplied: u.TimesApplied, TimesFailed: u.TimesFailed, FailureRate: u.FailureRate,
			TotalWaives: u.TotalWaives, WaivedFailures: u.WaivedFailures, WaiveRate: u.WaiveRate,
			WaivesByCategory: newWaiveCountsJSON(u.WaivesByCategory),
		},
		ByVersion:      make([]versionUsageJSON, 0, len(d.ByVersion)),
		Waivers:        newWaiverRowsJSON(waivers.Waivers),
		WaiversHasMore: waivers.HasMore,
	}
	for i := range d.ByVersion {
		v := &d.ByVersion[i]
		out.ByVersion = append(out.ByVersion, versionUsageJSON{
			Version: v.Version, CreatedAt: v.CreatedAt, TimesApplied: v.TimesApplied, TimesFailed: v.TimesFailed,
			FailureRate: v.FailureRate, TotalWaives: v.TotalWaives, WaivesByCategory: newWaiveCountsJSON(v.WaivesByCategory),
		})
	}
	if d.ByRepo != nil {
		repos := make([]repoUsageJSON, 0, len(d.ByRepo))
		for i := range d.ByRepo {
			r := &d.ByRepo[i]
			repos = append(repos, repoUsageJSON{
				Repository: r.Repository.Org + "/" + r.Repository.Name, TimesApplied: r.TimesApplied,
				TimesFailed: r.TimesFailed, FailureRate: r.FailureRate, TotalWaives: r.TotalWaives,
			})
		}
		out.ByRepo = &repos
	}
	return out
}

func newReviewRefJSON(r *api.InvariantReview) reviewRefJSON {
	out := reviewRefJSON{
		sessionRef:   newSessionRef(r.ReviewNumber, r.URL),
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
		row := waiverRowJSON{ID: w.ID, waiverJSON: newWaiverJSON(&w.InvariantWaiver), Review: newReviewRefJSON(&w.Review)}
		if w.Invariant != nil {
			summary := newInvariantSummaryJSON(w.Invariant)
			row.Invariant = &summary
		}
		out = append(out, row)
	}
	return out
}

func newInvariantWaiversJSON(resp *api.InvariantWaiversResponse) waiversJSON {
	return waiversJSON{Waivers: newWaiverRowsJSON(resp.Waivers), HasMore: resp.HasMore}
}

func newInvariantReviewsJSON(inv *api.Invariant, resp *api.InvariantReviewsResponse) reviewsJSON {
	out := reviewsJSON{Reviews: make([]reviewRowJSON, 0, len(resp.Reviews)), HasMore: resp.HasMore}
	if inv != nil {
		out.Invariant = &invariantSummaryJSON{ID: inv.ID, Title: inv.Title, Category: inv.Category, Status: inv.Status}
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
		if r.LatestRun != nil {
			row.LatestRun = &reviewRunJSON{RunID: r.LatestRun.ID, Status: r.LatestRun.Status, CompletedAt: r.LatestRun.CompletedAt}
		}
		if r.Waiver != nil {
			waiver := newWaiverJSON(r.Waiver)
			row.Waiver = &waiver
		}
		out.Reviews = append(out.Reviews, row)
	}
	return out
}
