package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/dustin/go-humanize/english"
)

// formatInvariantList renders one page of invariants as a table.
func formatInvariantList(resp *api.ListInvariantsResponse) string {
	if len(resp.Invariants) == 0 {
		if resp.Page > 1 {
			return fmt.Sprintf("Nothing on page %d, the listing ends before it. Try a lower --page.\n", resp.Page)
		}
		return "No invariants match.\n"
	}
	var b strings.Builder
	b.WriteString(formatInvariantTable(resp.Invariants))
	if resp.HasMore {
		fmt.Fprintf(&b, "\nSee more invariants --page %d\n", max(resp.Page, 1)+1)
	}
	return b.String()
}

// formatInvariantTable renders invariants one per row under a header. Title
// is the last column so the one unpadded cell carries the long text, the
// scope column is kept short so one narrowly scoped rule cannot widen the
// page, and a rule proposed by an AI pipeline gets a continuation row under
// its title with the reason, which is what an approver reads before deciding.
func formatInvariantTable(invariants []api.Invariant) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tCATEGORY\tSOURCE\tSCOPE\tTITLE")
	for i := range invariants {
		inv := &invariants[i]
		fmt.Fprintf(w, "#%d\t%s\t%s\t%s\t%s\t%s\n",
			inv.ID, formatInvariantStatus(inv), inv.Category, inv.Source,
			formatInvariantScopeShort(inv), inv.Title)
		if reason := strings.TrimSpace(inv.Reason); reason != "" {
			// Same cell count as a data row so tabwriter keeps one column
			// block; the reason lands under TITLE, unpadded.
			fmt.Fprintf(w, "\t\t\t\t\t%s\n", colors.Faint("reason: "+strings.ReplaceAll(reason, "\n", " ")))
		}
	}
	_ = w.Flush()
	return b.String()
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
// the create and edit confirmations. Multi-line values (a body from
// --body-file, typically) keep their continuation lines aligned under the
// first.
func formatInvariantDetail(inv *api.Invariant) string {
	var b strings.Builder
	field := func(name, value string) {
		label := "  " + name + ": "
		fmt.Fprintf(&b, "%s%s\n", label, indentContinuation(value, len(label)))
	}
	field("Title", inv.Title)
	field("Category", inv.Category)
	field("Status", fmt.Sprintf("%s (enabled: %t)", inv.Status, inv.Enabled))
	field("Source", inv.Source)
	field("Scope", formatInvariantScope(inv))
	field("Body", strings.TrimSpace(inv.Body))
	if reason := strings.TrimSpace(inv.Reason); reason != "" {
		field("Reason", reason)
	}
	if len(inv.SourceRefs) > 0 {
		field("Source refs", strings.Join(inv.SourceRefs, ", "))
	}
	return b.String()
}

// indentContinuation pads every line after the first by width columns.
func indentContinuation(s string, width int) string {
	return strings.ReplaceAll(s, "\n", "\n"+strings.Repeat(" ", width))
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
