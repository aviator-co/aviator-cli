package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/dustin/go-humanize/english"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// usageSorts names the --sort values `list --usage` takes.
const usageSorts = "applied, failed, failure-rate, waived"

// reviewOutcomes names the --outcome values `reviews` takes. The server
// validates the value, so nothing is checked client-side.
const reviewOutcomes = "passed, failed, warned, errored, waived, fixed, not_run"

// waiveCategories names the waiver reasons --category takes; the server
// validates these too.
const waiveCategories = "false_positive, doesnt_apply, accepted_risk, fix_in_followup"

// analyticsHelp is the paragraph every analytics command's help ends with.
const analyticsHelp = "Counts are per review session, all-time unless --since. These commands\n" +
	"work with any API token and need a backend with the analytics endpoints;\n" +
	"self-managed installs get them a release after cloud.\n" +
	"\n" +
	"Docs: " + docsInvariants

var invariantsInsightsFlags struct {
	Since  string
	Repo   string
	Limit  int
	Status string
}

var invariantsInsightsCmd = &cobra.Command{
	Use:   "insights",
	Short: "Rank invariants by how often they apply, fail and get waived",
	Long: "Rank the account's invariants by how often reviews apply them, how\n" +
		"often they fail, how often failures get waived and why, plus the ones\n" +
		"no review has applied yet. Covers active invariants unless --status.\n" +
		"\n" + analyticsHelp,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if invariantsInsightsFlags.Limit < 1 {
			return errors.New("--limit must be at least 1")
		}
		since, err := parseSince(invariantsInsightsFlags.Since, time.Now())
		if err != nil {
			return err
		}
		query := api.InvariantInsightsQuery{
			Since:  since.Param,
			Limit:  invariantsInsightsFlags.Limit,
			Status: invariantsInsightsFlags.Status,
		}
		repoLabel := "all repos"
		if invariantsInsightsFlags.Repo != "" {
			repo, err := parseRepo(invariantsInsightsFlags.Repo)
			if err != nil {
				return err
			}
			query.Repo = &repo
			repoLabel = repo.Org + "/" + repo.Name
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.GetInvariantInsights(cmd.Context(), query)
		if err != nil {
			return analyticsError(err)
		}
		if invariantsFlags.JSON {
			return printJSON(newInsightsJSON(resp))
		}
		fmt.Print(formatInvariantInsights(resp, since.Label, repoLabel))
		return nil
	},
}

var invariantsShowFlags struct {
	Since  string
	ByRepo bool
	Page   int
	Limit  int
}

var invariantsShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show one invariant with its usage, per version, and its waives",
	Long: "Show one invariant's fields, how many reviews applied, failed and\n" +
		"waived it, the same per invariant version (and per repository with\n" +
		"--by-repo), and a page of its waives, newest first.\n" +
		"\n" + analyticsHelp,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseInvariantID(args[0])
		if err != nil {
			return err
		}
		if err := validatePageFlags(invariantsShowFlags.Page, invariantsShowFlags.Limit); err != nil {
			return err
		}
		since, err := parseSince(invariantsShowFlags.Since, time.Now())
		if err != nil {
			return err
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		detail, err := client.GetInvariant(cmd.Context(), id, api.GetInvariantQuery{
			Since:  since.Param,
			ByRepo: invariantsShowFlags.ByRepo,
		})
		if err != nil {
			return analyticsError(err)
		}
		waivers, err := client.ListInvariantWaivers(cmd.Context(), id, api.InvariantWaiversQuery{
			Since:   since.Param,
			Page:    invariantsShowFlags.Page,
			PerPage: invariantsShowFlags.Limit,
		})
		if err != nil {
			return analyticsError(err)
		}
		if invariantsFlags.JSON {
			return printJSON(newInvariantShowJSON(detail, waivers))
		}
		fmt.Print(formatInvariantShow(detail, waivers, since.Label))
		return nil
	},
}

var invariantsReviewsFlags struct {
	Outcome string
	Repo    string
	Since   string
	Page    int
	Limit   int
}

var invariantsReviewsCmd = &cobra.Command{
	Use:   "reviews <id>",
	Short: "List the reviews an invariant was applied on, with each outcome",
	Long: "List the reviews an invariant was applied on, newest first, with how\n" +
		"it came out on each: passed, failed, warned, errored, waived, fixed\n" +
		"(failed an earlier run, passed the latest), or not_run.\n" +
		"\n" + analyticsHelp,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseInvariantID(args[0])
		if err != nil {
			return err
		}
		if err := validatePageFlags(invariantsReviewsFlags.Page, invariantsReviewsFlags.Limit); err != nil {
			return err
		}
		since, err := parseSince(invariantsReviewsFlags.Since, time.Now())
		if err != nil {
			return err
		}
		query := api.InvariantReviewsQuery{
			Since:   since.Param,
			Outcome: invariantsReviewsFlags.Outcome,
			Page:    invariantsReviewsFlags.Page,
			PerPage: invariantsReviewsFlags.Limit,
		}
		if invariantsReviewsFlags.Repo != "" {
			repo, err := parseRepo(invariantsReviewsFlags.Repo)
			if err != nil {
				return err
			}
			query.Repo = &repo
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.ListInvariantReviews(cmd.Context(), id, query)
		if err != nil {
			return analyticsError(err)
		}
		// The rows carry no title; the listing's id filter is the cheapest
		// lookup and works on every backend.
		found, err := client.ListInvariants(cmd.Context(), api.ListInvariantsQuery{IDs: []int{id}, PerPage: 1})
		if err != nil {
			return err
		}
		var invariant *api.Invariant
		if len(found.Invariants) > 0 {
			invariant = &found.Invariants[0]
		}
		if invariantsFlags.JSON {
			return printJSON(newInvariantReviewsJSON(invariant, resp))
		}
		fmt.Print(formatInvariantReviews(id, invariant, resp))
		return nil
	},
}

var invariantsWaiversFlags struct {
	Invariant int
	User      string
	Category  string
	Repo      string
	Since     string
	Page      int
	Limit     int
}

var invariantsWaiversCmd = &cobra.Command{
	Use:   "waivers",
	Short: "List invariant waives across the account, newest first",
	Long: "List every waive of an invariant on the account, newest first: who\n" +
		"waived which rule on which review, for what reason. --user me needs a\n" +
		"user token (from `aviator login` or a personal token).\n" +
		"\n" + analyticsHelp,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := validatePageFlags(invariantsWaiversFlags.Page, invariantsWaiversFlags.Limit); err != nil {
			return err
		}
		if invariantsWaiversFlags.Invariant < 0 {
			return errors.New("--invariant must be a positive id")
		}
		since, err := parseSince(invariantsWaiversFlags.Since, time.Now())
		if err != nil {
			return err
		}
		query := api.AccountWaiversQuery{
			Since:       since.Param,
			InvariantID: invariantsWaiversFlags.Invariant,
			User:        invariantsWaiversFlags.User,
			Category:    invariantsWaiversFlags.Category,
			Page:        invariantsWaiversFlags.Page,
			PerPage:     invariantsWaiversFlags.Limit,
		}
		if invariantsWaiversFlags.Repo != "" {
			repo, err := parseRepo(invariantsWaiversFlags.Repo)
			if err != nil {
				return err
			}
			query.Repo = &repo
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.ListAccountWaivers(cmd.Context(), query)
		if err != nil {
			return analyticsError(err)
		}
		if invariantsFlags.JSON {
			return printJSON(newInvariantWaiversJSON(resp))
		}
		fmt.Print(formatAccountWaivers(resp))
		return nil
	},
}

func init() {
	f := invariantsInsightsCmd.Flags()
	registerSinceFlag(f, &invariantsInsightsFlags.Since)
	f.StringVar(&invariantsInsightsFlags.Repo, "repo", "", "only reviews of this owner/repo")
	f.IntVar(&invariantsInsightsFlags.Limit, "limit", 20, "rows per table (max 100)")
	f.StringVar(&invariantsInsightsFlags.Status, "status", "",
		"only invariants with this status ("+invariantStatuses+"; default active)")

	f = invariantsShowCmd.Flags()
	registerSinceFlag(f, &invariantsShowFlags.Since)
	f.BoolVar(&invariantsShowFlags.ByRepo, "by-repo", false, "also break the counts down per repository")
	f.IntVar(&invariantsShowFlags.Page, "page", 1, "page of waives, starting at 1")
	f.IntVar(&invariantsShowFlags.Limit, "limit", 20, "waives per page (max 100)")

	f = invariantsReviewsCmd.Flags()
	f.StringVar(&invariantsReviewsFlags.Outcome, "outcome", "", "only reviews with this outcome ("+reviewOutcomes+")")
	f.StringVar(&invariantsReviewsFlags.Repo, "repo", "", "only reviews of this owner/repo")
	registerSinceFlag(f, &invariantsReviewsFlags.Since)
	f.IntVar(&invariantsReviewsFlags.Page, "page", 1, "page number, starting at 1")
	f.IntVar(&invariantsReviewsFlags.Limit, "limit", 20, "reviews per page (max 100)")

	f = invariantsWaiversCmd.Flags()
	f.IntVar(&invariantsWaiversFlags.Invariant, "invariant", 0, "only waives of this invariant id")
	f.StringVar(&invariantsWaiversFlags.User, "user", "", "only waives by this user: me, or an email on the account")
	f.StringVar(&invariantsWaiversFlags.Category, "category", "", "only waives for this reason ("+waiveCategories+")")
	f.StringVar(&invariantsWaiversFlags.Repo, "repo", "", "only waives on reviews of this owner/repo")
	registerSinceFlag(f, &invariantsWaiversFlags.Since)
	f.IntVar(&invariantsWaiversFlags.Page, "page", 1, "page number, starting at 1")
	f.IntVar(&invariantsWaiversFlags.Limit, "limit", 20, "waives per page (max 100)")
}

// registerSinceFlag binds --since the same way on every analytics command.
func registerSinceFlag(fs *pflag.FlagSet, since *string) {
	fs.StringVar(since, "since", "all", "only reviews since then: <N>d (e.g. 30d), YYYY-MM-DD, or all")
}

// sinceWindow is a parsed --since: the instant the query sends (empty for
// all time) and how the output names the window.
type sinceWindow struct {
	Param string
	Label string
}

// parseSince reads a --since value: <N>d counts back from now, a date starts
// at midnight UTC, and all (or empty) means no window.
func parseSince(s string, now time.Time) (sinceWindow, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "all" {
		return sinceWindow{Label: "all time"}, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return sinceWindow{}, invalidSince(s)
		}
		from := now.UTC().Add(-time.Duration(n) * 24 * time.Hour)
		return sinceWindow{Param: from.Format(time.RFC3339), Label: "last " + english.Plural(n, "day", "")}, nil
	}
	day, err := time.ParseInLocation(time.DateOnly, s, time.UTC)
	if err != nil {
		return sinceWindow{}, invalidSince(s)
	}
	return sinceWindow{Param: day.Format(time.RFC3339), Label: "since " + s}, nil
}

func invalidSince(s string) error {
	return errors.Errorf("invalid --since %q, expected <N>d, YYYY-MM-DD, or all", s)
}

// parseUsageSort maps a --sort value onto the server's name for it.
func parseUsageSort(s string) (string, error) {
	switch s {
	case "", "applied", "failed", "waived":
		return s, nil
	case "failure-rate":
		return "failure_rate", nil
	}
	return "", errors.Errorf("invalid --sort %q, expected one of %s", s, usageSorts)
}

// analyticsError rewrites the bare 404 a backend without the analytics
// routes answers, and the bare 405 its PATCH/DELETE-only /<id> rule gives a
// GET, into one message saying so. A JSON not-found is a real unknown
// invariant and passes through unchanged.
func analyticsError(err error) error {
	var ae *api.Error
	if errors.As(err, &ae) && ae.Code == "" &&
		(ae.Status == http.StatusNotFound || ae.Status == http.StatusMethodNotAllowed) {
		return errors.Errorf("your Aviator backend does not support invariant analytics yet (%s %s returned %d)",
			ae.Method, ae.Path, ae.Status)
	}
	return err
}
