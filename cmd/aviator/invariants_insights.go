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

// usageSorts names the --sort values `list --usage` takes. The server
// validates the value, so nothing is checked client-side.
const usageSorts = "applied, failed, failure_rate, waived"

// reviewOutcomes names the --outcome values `reviews` takes. The server
// validates the value, so nothing is checked client-side.
const reviewOutcomes = "passed, failed, warned, errored, waived, fixed, not_run"

// waiveCategories names the waiver reasons --category takes; the server
// validates these too.
const waiveCategories = "false_positive, doesnt_apply, accepted_risk, fix_in_followup"

// analyticsHelp is the paragraph every analytics command's help ends with.
const analyticsHelp = "Counts are per review session, all-time unless --since. These commands\n" +
	"work with any API token and need a backend with the analytics endpoints.\n" +
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
		"often they fail, how often failures get waived and why, plus the enabled\n" +
		"ones no review has applied yet. Covers active invariants unless --status.\n" +
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
		seeAll := neverAppliedHint(since, invariantsInsightsFlags.Since, query.Repo, query.Status)
		fmt.Print(formatInvariantInsights(resp, since.Label, repoLabel, seeAll))
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
	Short: "Show one invariant with its usage, per version, and its waivers",
	Long: "Show one invariant's fields, how many reviews applied, failed and\n" +
		"waived it, the same per invariant version (and per repository with\n" +
		"--by-repo), and a page of its waivers, newest first.\n" +
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
		waivers, err := client.ListAccountWaivers(cmd.Context(), api.AccountWaiversQuery{
			Since:       since.Param,
			InvariantID: id,
			Page:        invariantsShowFlags.Page,
			PerPage:     invariantsShowFlags.Limit,
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
		if invariantsFlags.JSON {
			return printJSON(newInvariantReviewsJSON(resp))
		}
		fmt.Print(formatInvariantReviews(resp))
		return nil
	},
}

var invariantsWaiversFlags struct {
	Invariant string
	User      string
	Category  string
	Repo      string
	Since     string
	Page      int
	Limit     int
}

var invariantsWaiversCmd = &cobra.Command{
	Use:   "waivers",
	Short: "List invariant waivers across the account, newest first",
	Long: "List every waiver of an invariant on the account, newest first: who\n" +
		"waived which rule on which review, for what reason. --user me needs a\n" +
		"user token (from `aviator login` or a personal token).\n" +
		"\n" + analyticsHelp,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := validatePageFlags(invariantsWaiversFlags.Page, invariantsWaiversFlags.Limit); err != nil {
			return err
		}
		invariantID := 0
		if invariantsWaiversFlags.Invariant != "" {
			id, err := parseInvariantID(invariantsWaiversFlags.Invariant)
			if err != nil {
				return err
			}
			invariantID = id
		}
		since, err := parseSince(invariantsWaiversFlags.Since, time.Now())
		if err != nil {
			return err
		}
		query := api.AccountWaiversQuery{
			Since:       since.Param,
			InvariantID: invariantID,
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
	registerSinceFlag(f, &invariantsInsightsFlags.Since, sinceReviewsHelp)
	f.StringVar(&invariantsInsightsFlags.Repo, "repo", "", "only reviews of this owner/repo")
	f.IntVar(&invariantsInsightsFlags.Limit, "limit", 20, "rows per table (max 100)")
	f.StringVar(&invariantsInsightsFlags.Status, "status", "",
		"only invariants with this status ("+invariantStatuses+"; default active)")

	f = invariantsShowCmd.Flags()
	registerSinceFlag(f, &invariantsShowFlags.Since,
		"only reviews, and waivers made, since then: "+sinceValuesHelp)
	f.BoolVar(&invariantsShowFlags.ByRepo, "by-repo", false, "also break the counts down per repository")
	f.IntVar(&invariantsShowFlags.Page, "page", 1, "page of waivers, starting at 1")
	f.IntVar(&invariantsShowFlags.Limit, "limit", 20, "waivers per page (max 100)")

	f = invariantsReviewsCmd.Flags()
	f.StringVar(&invariantsReviewsFlags.Outcome, "outcome", "", "only reviews with this outcome ("+reviewOutcomes+")")
	f.StringVar(&invariantsReviewsFlags.Repo, "repo", "", "only reviews of this owner/repo")
	registerSinceFlag(f, &invariantsReviewsFlags.Since, sinceReviewsHelp)
	f.IntVar(&invariantsReviewsFlags.Page, "page", 1, "page number, starting at 1")
	f.IntVar(&invariantsReviewsFlags.Limit, "limit", 20, "reviews per page (max 100)")

	f = invariantsWaiversCmd.Flags()
	f.StringVar(&invariantsWaiversFlags.Invariant, "invariant", "", "only waivers of this invariant (id or #id)")
	f.StringVar(&invariantsWaiversFlags.User, "user", "", "only waivers by this user: me, or an email on the account")
	f.StringVar(&invariantsWaiversFlags.Category, "category", "", "only waivers for this reason ("+waiveCategories+")")
	f.StringVar(&invariantsWaiversFlags.Repo, "repo", "", "only waivers on reviews of this owner/repo")
	registerSinceFlag(f, &invariantsWaiversFlags.Since, "only waivers made since then: "+sinceValuesHelp)
	f.IntVar(&invariantsWaiversFlags.Page, "page", 1, "page number, starting at 1")
	f.IntVar(&invariantsWaiversFlags.Limit, "limit", 20, "waivers per page (max 100)")
}

const (
	sinceValuesHelp  = "<N>d (e.g. 30d), YYYY-MM-DD, or all (default all time)"
	sinceReviewsHelp = "only reviews since then: " + sinceValuesHelp
)

// registerSinceFlag binds --since with the given help on an analytics command.
func registerSinceFlag(fs *pflag.FlagSet, since *string, help string) {
	fs.StringVar(since, "since", "", help)
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
		from := now.UTC().AddDate(0, 0, -n)
		return sinceWindow{Param: from.Format(time.RFC3339), Label: "last " + english.Plural(n, "day", "")}, nil
	}
	day, err := time.ParseInLocation(time.DateOnly, s, time.UTC)
	if err != nil {
		return sinceWindow{}, invalidSince(s)
	}
	return sinceWindow{Param: day.Format(time.RFC3339), Label: "since " + s}, nil
}

// neverAppliedHint is the `list` command that pages through the
// never-applied invariants insights counted, with the same window, repo and
// status.
func neverAppliedHint(since sinceWindow, rawSince string, repo *api.Repository, status string) string {
	hint := "aviator invariants list --usage --never-applied"
	if since.Param != "" {
		hint += " --since " + strings.TrimSpace(rawSince)
	}
	if repo != nil {
		hint += " --repo " + repo.Org + "/" + repo.Name
	}
	if status != "" {
		hint += " --status " + status
	}
	return hint
}

func invalidSince(s string) error {
	return errors.Errorf("invalid --since %q, expected <N>d, YYYY-MM-DD, or all", s)
}

// analyticsError rewrites the bare 404 a backend without the analytics
// routes answers, and the bare 405 its PATCH/DELETE-only /<id> rule gives a
// GET, into one message saying so. A JSON not-found is a real unknown
// invariant and passes through unchanged.
func analyticsError(err error) error {
	var ae *api.Error
	if errors.As(err, &ae) && ae.Code == "" &&
		(ae.Status == http.StatusNotFound || ae.Status == http.StatusMethodNotAllowed) {
		return errors.Errorf("this Aviator backend doesn't support invariant analytics yet; %s %s returned %d",
			ae.Method, ae.Path, ae.Status)
	}
	return err
}
