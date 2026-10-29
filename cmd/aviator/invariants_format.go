package main

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/dustin/go-humanize/english"
)

// invariantListUsage is what a --usage listing's footer says: the status
// the counts cover. Nil is the plain listing.
type invariantListUsage struct {
	Status string
}

// formatInvariantList renders one page of invariants as a table.
func formatInvariantList(resp *api.ListInvariantsResponse, usage *invariantListUsage) string {
	if len(resp.Invariants) == 0 {
		return emptyPageMessage(resp.Page, "invariants")
	}
	var b strings.Builder
	b.WriteString(formatInvariantTable(resp.Invariants, usage != nil))
	more := ""
	if resp.HasMore {
		more = fmt.Sprintf("See more invariants --page %d", max(resp.Page, 1)+1)
	}
	switch {
	case usage != nil:
		// The status is in the footer because --usage defaults it to active,
		// which the rows alone would not show.
		fmt.Fprintf(&b, "%s invariants · all time", strings.ToUpper(usage.Status[:1])+usage.Status[1:])
		if more != "" {
			b.WriteString(" · " + more)
		}
		b.WriteString("\n")
	case more != "":
		b.WriteString("\n" + more + "\n")
	}
	return b.String()
}

// formatInvariantTable renders invariants one per row under a header. Title
// is the last column so the one unpadded cell carries the long text, the
// scope column is kept short so one narrowly scoped rule cannot widen the
// page, and a rule proposed by an AI pipeline gets a continuation row under
// its title with the reason, which is what an approver reads before deciding.
// With usage, the provenance columns give way to each row's counts.
func formatInvariantTable(invariants []api.Invariant, usage bool) string {
	var b strings.Builder
	header := []string{"ID", "STATUS", "CATEGORY", "SOURCE", "SCOPE", "OWNER", "TITLE"}
	if usage {
		header = []string{"ID", "STATUS", "CATEGORY", "APPLIED", "FAILED", "RATE", "WAIVED", "TITLE"}
	}
	rows := make([][]string, 0, len(invariants))
	for i := range invariants {
		inv := &invariants[i]
		row := []string{"#" + strconv.Itoa(inv.ID), formatInvariantStatus(inv), inv.Category}
		if usage {
			row = append(row, formatInvariantUsageCells(inv.Usage)...)
		} else {
			row = append(row, inv.Source, formatInvariantScopeShort(inv), formatInvariantOwnerShort(inv))
		}
		rows = append(rows, append(row, inv.Title))
		if reason := strings.TrimSpace(inv.Reason); reason != "" {
			// Same cell count as a data row so tabwriter keeps one column
			// block; the reason lands under TITLE, unpadded.
			reasonRow := make([]string, len(header))
			reasonRow[len(header)-1] = colors.Faint("reason: " + strings.ReplaceAll(reason, "\n", " "))
			rows = append(rows, reasonRow)
		}
	}
	writeTable(&b, "", header, rows)
	return b.String()
}

// formatInvariantUsageCells renders the APPLIED, FAILED, RATE and WAIVED cells.
func formatInvariantUsageCells(u *api.InvariantUsage) []string {
	return []string{
		strconv.Itoa(u.TimesApplied), strconv.Itoa(u.TimesFailed), formatRate(u.FailureRate), strconv.Itoa(u.TotalWaives),
	}
}

// formatInvariantStatus renders the lifecycle status, flagging the active-but-
// switched-off case since the selector skips those too.
func formatInvariantStatus(inv *api.Invariant) string {
	if inv.Status == "active" && !inv.Enabled {
		return "active (disabled)"
	}
	return inv.Status
}

// formatInvariantScopeShort is the bounded form for the table: one repo by
// name, several by count, and a marker when conditions narrow it further. The
// full scope is in the create/edit detail and in --json.
func formatInvariantScopeShort(inv *api.Invariant) string {
	scope := "all repos"
	switch len(inv.Repositories) {
	case 0:
	case 1:
		scope = inv.Repositories[0].Org + "/" + inv.Repositories[0].Name
	default:
		scope = english.Plural(len(inv.Repositories), "repo", "repos")
	}
	if len(inv.Conditions) > 0 {
		scope += " +conditions"
	}
	return scope
}

// formatInvariantOwnerShort is the bounded form for the table: one team by
// name, several by count. "-" is a backend that predates owner teams.
func formatInvariantOwnerShort(inv *api.Invariant) string {
	if inv.OwnerTeams == nil {
		return "-"
	}
	switch teams := *inv.OwnerTeams; len(teams) {
	case 0:
		return "none"
	case 1:
		return "@" + teams[0].Org + "/" + teams[0].Slug
	default:
		return english.Plural(len(teams), "team", "teams")
	}
}

// formatInvariantOwners lists every owner team as @org/slug.
func formatInvariantOwners(teams []api.OwnerTeam) string {
	if len(teams) == 0 {
		return "none"
	}
	names := ownerTeamNames(teams)
	for i := range names {
		names[i] = "@" + names[i]
	}
	return strings.Join(names, ", ")
}

// formatInvariantScope describes in full which repos and files an invariant
// applies to.
func formatInvariantScope(inv *api.Invariant) string {
	scope := "all repos"
	if len(inv.Repositories) > 0 {
		names := make([]string, len(inv.Repositories))
		for i, r := range inv.Repositories {
			names[i] = r.Org + "/" + r.Name
		}
		scope = strings.Join(names, ", ")
	}
	if len(inv.Conditions) > 0 {
		scope += " where " + strings.Join(formatConditions(inv.Conditions), " and ")
	}
	return scope
}

func formatConditions(conditions []api.InvariantCondition) []string {
	out := make([]string, len(conditions))
	for i, c := range conditions {
		op := "="
		if c.Negate {
			op = "!="
		}
		out[i] = c.Type + op + c.Value
	}
	return out
}

// formatInvariantDetail renders an invariant's fields as indented lines, for
// the create and edit confirmations.
func formatInvariantDetail(inv *api.Invariant) string {
	var b strings.Builder
	writeInvariantField(&b, "Title", inv.Title)
	b.WriteString(formatInvariantFields(inv))
	return b.String()
}

// formatInvariantFields renders every field but the title, which `show`
// carries in its heading instead. Multi-line values (a body from
// --body-file, typically) keep their continuation lines aligned under the
// first.
func formatInvariantFields(inv *api.Invariant) string {
	var b strings.Builder
	field := func(name, value string) { writeInvariantField(&b, name, value) }
	field("Category", inv.Category)
	field("Status", fmt.Sprintf("%s (enabled: %t)", inv.Status, inv.Enabled))
	field("Source", inv.Source)
	field("Scope", formatInvariantScope(inv))
	if inv.OwnerTeams != nil {
		field("Owner", formatInvariantOwners(*inv.OwnerTeams))
	}
	if inv.Version != nil {
		field("Version", strconv.Itoa(*inv.Version))
	}
	field("Body", strings.TrimSpace(inv.Body))
	if reason := strings.TrimSpace(inv.Reason); reason != "" {
		field("Reason", reason)
	}
	if len(inv.SourceRefs) > 0 {
		field("Source refs", strings.Join(inv.SourceRefs, ", "))
	}
	return b.String()
}

func writeInvariantField(b *strings.Builder, name, value string) {
	label := "  " + name + ": "
	fmt.Fprintf(b, "%s%s\n", label, indentContinuation(value, len(label)))
}

// indentContinuation pads every line after the first by width columns.
func indentContinuation(s string, width int) string {
	return strings.ReplaceAll(s, "\n", "\n"+strings.Repeat(" ", width))
}

// formatInvariantOwnerTeams renders one page of teams with their invariant
// counts, marking the default, then how many invariants nobody owns.
func formatInvariantOwnerTeams(resp *api.ListInvariantOwnerTeamsResponse) string {
	var b strings.Builder
	switch {
	case len(resp.OwnerTeams) == 0 && resp.Page > 1:
		b.WriteString(emptyPageMessage(resp.Page, "teams"))
	case len(resp.OwnerTeams) == 0:
		b.WriteString(colors.Warning("no teams yet") + ": Aviator has not synced any GitHub teams for this account\n")
	default:
		w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TEAM\tINVARIANTS\tNAME")
		for _, t := range resp.OwnerTeams {
			name := t.Name
			if t.IsDefault {
				name += " " + colors.Faint("(default owner team)")
			}
			fmt.Fprintf(w, "%s/%s\t%d\t%s\n", t.Org, t.Slug, t.InvariantCount, name)
		}
		_ = w.Flush()
		if resp.HasMore {
			fmt.Fprintf(&b, "\nSee more teams --page %d\n", max(resp.Page, 1)+1)
		}
	}
	if resp.UnownedCount > 0 {
		fmt.Fprintf(&b, "\n%s with no owner team (see list --unowned)\n",
			english.Plural(resp.UnownedCount, "invariant", ""))
	}
	return b.String()
}

// formatInvariantCategories renders the categories as slug, name and
// description lines. An empty list means nothing has seeded the account yet,
// so it says where seeding happens instead of printing a bare zero.
func formatInvariantCategories(resp *api.ListInvariantCategoriesResponse) string {
	if len(resp.Categories) == 0 {
		return colors.Warning("no categories yet") +
			": open the Verify invariants settings page or run verify onboarding to seed them\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", colors.Success("✓"), english.Plural(len(resp.Categories), "category", ""))
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SLUG\tNAME\tDESCRIPTION")
	for _, c := range resp.Categories {
		fmt.Fprintf(w, "%s\t%s\t%s\n", c.Slug, c.Name, c.Description)
	}
	_ = w.Flush()
	return b.String()
}

type invariantJSON struct {
	ID           int             `json:"id"`
	Title        string          `json:"title"`
	Body         string          `json:"body"`
	Category     string          `json:"category"`
	Source       string          `json:"source"`
	Status       string          `json:"status"`
	Enabled      bool            `json:"enabled"`
	Reason       string          `json:"reason"`
	SourceRefs   []string        `json:"source_refs"`
	Repositories []string        `json:"repositories"`
	Conditions   []conditionJSON `json:"conditions"`
	// OwnerTeams is null when the backend predates owner teams.
	OwnerTeams *[]string `json:"owner_teams"`
	// Version is null when the invariant has none; Usage is only on
	// `list --usage` and `show`.
	Version    *int                `json:"version"`
	Usage      *invariantUsageJSON `json:"usage,omitempty"`
	CreatedAt  string              `json:"created_at"`
	ModifiedAt string              `json:"modified_at"`
}

// invariantUsageJSON mirrors the API's usage counts, the same on list rows,
// on show and on its by-version and by-repo rows.
type invariantUsageJSON struct {
	TimesApplied     int             `json:"times_applied"`
	TimesFailed      int             `json:"times_failed"`
	FailureRate      *float64        `json:"failure_rate"`
	TotalWaives      int             `json:"total_waives"`
	WaivedFailures   int             `json:"waived_failures"`
	WaiveRate        *float64        `json:"waive_rate"`
	WaivesByCategory waiveCountsJSON `json:"waives_by_category"`
}

func newInvariantUsageJSON(u *api.InvariantUsage) invariantUsageJSON {
	return invariantUsageJSON{
		TimesApplied:     u.TimesApplied,
		TimesFailed:      u.TimesFailed,
		FailureRate:      u.FailureRate,
		TotalWaives:      u.TotalWaives,
		WaivedFailures:   u.WaivedFailures,
		WaiveRate:        u.WaiveRate,
		WaivesByCategory: newWaiveCountsJSON(u.WaivesByCategory),
	}
}

type conditionJSON struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Negate bool   `json:"negate"`
}

// invariantListJSON serves list (with HasMore) and the status commands.
type invariantListJSON struct {
	Invariants []invariantJSON `json:"invariants"`
	HasMore    bool            `json:"has_more"`
}

type invariantDeleteJSON struct {
	ID int `json:"id"`
}

type ownerTeamJSON struct {
	Team           string `json:"team"`
	Name           string `json:"name"`
	InvariantCount int    `json:"invariant_count"`
	IsDefault      bool   `json:"is_default"`
}

type ownerTeamsJSON struct {
	OwnerTeams   []ownerTeamJSON `json:"owner_teams"`
	UnownedCount int             `json:"unowned_count"`
	HasMore      bool            `json:"has_more"`
}

type categoryJSON struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type categoriesJSON struct {
	Categories []categoryJSON `json:"categories"`
}

func newInvariantJSON(inv *api.Invariant) invariantJSON {
	out := invariantJSON{
		ID:           inv.ID,
		Title:        inv.Title,
		Body:         inv.Body,
		Category:     inv.Category,
		Source:       inv.Source,
		Status:       inv.Status,
		Enabled:      inv.Enabled,
		Reason:       inv.Reason,
		SourceRefs:   inv.SourceRefs,
		Repositories: make([]string, 0, len(inv.Repositories)),
		Conditions:   make([]conditionJSON, 0, len(inv.Conditions)),
		CreatedAt:    inv.CreatedAt,
		ModifiedAt:   inv.ModifiedAt,
	}
	if out.SourceRefs == nil {
		out.SourceRefs = []string{}
	}
	for _, r := range inv.Repositories {
		out.Repositories = append(out.Repositories, r.Org+"/"+r.Name)
	}
	for _, c := range inv.Conditions {
		out.Conditions = append(out.Conditions, conditionJSON{Type: c.Type, Value: c.Value, Negate: c.Negate})
	}
	if inv.OwnerTeams != nil {
		names := ownerTeamNames(*inv.OwnerTeams)
		out.OwnerTeams = &names
	}
	out.Version = inv.Version
	if inv.Usage != nil {
		usage := newInvariantUsageJSON(inv.Usage)
		out.Usage = &usage
	}
	return out
}

func newInvariantsJSON(invariants []api.Invariant) []invariantJSON {
	out := make([]invariantJSON, 0, len(invariants))
	for i := range invariants {
		out = append(out, newInvariantJSON(&invariants[i]))
	}
	return out
}

func newOwnerTeamsJSON(resp *api.ListInvariantOwnerTeamsResponse) ownerTeamsJSON {
	out := ownerTeamsJSON{
		OwnerTeams:   make([]ownerTeamJSON, 0, len(resp.OwnerTeams)),
		UnownedCount: resp.UnownedCount,
		HasMore:      resp.HasMore,
	}
	for _, t := range resp.OwnerTeams {
		out.OwnerTeams = append(out.OwnerTeams, ownerTeamJSON{
			Team:           t.Org + "/" + t.Slug,
			Name:           t.Name,
			InvariantCount: t.InvariantCount,
			IsDefault:      t.IsDefault,
		})
	}
	return out
}

func newCategoriesJSON(categories []api.InvariantCategory) categoriesJSON {
	out := categoriesJSON{Categories: make([]categoryJSON, 0, len(categories))}
	for _, c := range categories {
		out.Categories = append(out.Categories, categoryJSON{Slug: c.Slug, Name: c.Name, Description: c.Description})
	}
	return out
}
