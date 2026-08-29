package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/spf13/cobra"
)

var sessionsFlags struct {
	Repo   string
	Branch string
	PR     int
	Status string
	Page   int
	Limit  int
	JSON   bool
}

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List your verify and runbook sessions in a repository",
	Long: "List your verify and runbook sessions in a repository, newest first.\n" +
		"\n" +
		"Check here before running `aviator verify`. Submitting a branch that\n" +
		"already has a session creates a second one instead of updating the first,\n" +
		"and a PR opened from that branch then links to neither. To update the\n" +
		"criteria on a session you already have, use `aviator edit`.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		repo, err := parseRepo(sessionsFlags.Repo)
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}

		var sessions []api.SessionSummary
		hasMore := false
		if sessionsFlags.PR > 0 {
			sessions, err = client.FindSessionsForPullRequest(
				cmd.Context(), repo, sessionsFlags.PR, sessionsFlags.Status)
		} else {
			sessions, hasMore, err = client.ListSessions(cmd.Context(), api.ListSessionsParams{
				Repository:    repo,
				WorkingBranch: sessionsFlags.Branch,
				Status:        sessionsFlags.Status,
				Page:          sessionsFlags.Page,
				Limit:         sessionsFlags.Limit,
			})
		}
		if err != nil {
			return err
		}

		if sessionsFlags.JSON {
			return printJSON(newSessionsJSON(sessions, hasMore))
		}
		if len(sessions) == 0 {
			fmt.Println(noSessionsMessage())
			return nil
		}
		fmt.Print(formatSessions(sessions, hasMore, sessionsFlags.Page))
		return nil
	},
}

func init() {
	f := sessionsCmd.Flags()
	f.StringVar(&sessionsFlags.Repo, "repo", "", "GitHub repo as owner/repo")
	f.StringVar(&sessionsFlags.Branch, "branch", "", "only sessions on this working branch")
	f.IntVar(&sessionsFlags.PR, "pr", 0, "only sessions linked to this PR number")
	f.StringVar(&sessionsFlags.Status, "status", "active", "session status to list (active or archived)")
	f.IntVar(&sessionsFlags.Page, "page", 1, "which page to show")
	f.IntVar(&sessionsFlags.Limit, "limit", 20, "sessions per page")
	f.BoolVar(&sessionsFlags.JSON, "json", false, "print the sessions as a single JSON object")
	sessionsCmd.MarkFlagsMutuallyExclusive("branch", "pr")
	_ = sessionsCmd.MarkFlagRequired("repo")
}

// noSessionsMessage repeats what was searched, so an empty result reads as an
// answer rather than as a command that did nothing. Past the last page it says
// that instead, since "no sessions" there would be a lie.
func noSessionsMessage() string {
	switch {
	case sessionsFlags.Page > 1:
		return fmt.Sprintf("Nothing on page %d, the listing ends before it. Try a lower --page.",
			sessionsFlags.Page)
	case sessionsFlags.Branch != "":
		return fmt.Sprintf("No %s sessions on branch %s.", sessionsFlags.Status, sessionsFlags.Branch)
	case sessionsFlags.PR > 0:
		return fmt.Sprintf("No %s sessions for PR #%d.", sessionsFlags.Status, sessionsFlags.PR)
	default:
		return fmt.Sprintf("No %s sessions in %s.", sessionsFlags.Status, sessionsFlags.Repo)
	}
}

// formatSessions renders one line per session, under a header. What a session
// is about is `aviator show`; this says which ones exist and where they point.
func formatSessions(sessions []api.SessionSummary, hasMore bool, page int) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tBRANCH\tPRS")
	for _, s := range sessions {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			formatRunbookID(s.RunbookNumber),
			orDash(s.WorkingBranch),
			formatLinkedPRs(s.PullRequests),
		)
	}
	_ = w.Flush()
	if hasMore {
		fmt.Fprintf(&b, "\nSee more sessions --page %d\n", max(page, 1)+1)
	}
	return b.String()
}

func formatLinkedPRs(prs []api.LinkedPullRequest) string {
	if len(prs) == 0 {
		return "-"
	}
	numbers := make([]string, len(prs))
	for i, pr := range prs {
		numbers[i] = fmt.Sprintf("#%d", pr.Number)
	}
	return strings.Join(numbers, ",")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// sessionJSON is the --json shape of one session: enough to identify it, match
// it to a branch, put its URL in a PR body, and edit its criteria. It is its
// own struct rather than the raw response so the keys stay put as that grows.
type sessionJSON struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	WorkingBranch  string `json:"working_branch"`
	PullRequests   []int  `json:"pull_requests"`
	RunbookVersion *int   `json:"runbook_version"`
}

type sessionsJSON struct {
	Sessions []sessionJSON `json:"sessions"`
	HasMore  bool          `json:"has_more"`
}

func newSessionsJSON(sessions []api.SessionSummary, hasMore bool) sessionsJSON {
	out := sessionsJSON{Sessions: make([]sessionJSON, 0, len(sessions)), HasMore: hasMore}
	for _, s := range sessions {
		prs := make([]int, 0, len(s.PullRequests))
		for _, pr := range s.PullRequests {
			prs = append(prs, pr.Number)
		}
		out.Sessions = append(out.Sessions, sessionJSON{
			ID:             formatRunbookID(s.RunbookNumber),
			URL:            s.URL,
			WorkingBranch:  s.WorkingBranch,
			PullRequests:   prs,
			RunbookVersion: s.RunbookVersion,
		})
	}
	return out
}
