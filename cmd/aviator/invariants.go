package main

import (
	"fmt"
	"strconv"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/dustin/go-humanize/english"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// invariantStatuses names the server's lifecycle statuses for flag help. The
// server validates the value, so nothing is checked client-side.
const invariantStatuses = "pending, active, rejected"

// invariantSources names the server's provenance values for flag help; the
// server validates these too.
const invariantSources = "manual, ai_generated, ai_generated_docs, ai_generated_pr_comments, template, slack, github_comment"

// conditionFlagHelp documents the --condition syntax shared by create and edit.
const conditionFlagHelp = "narrow when the invariant applies, as <type>=<value> or <type>!=<value> " +
	"(types: file_path_glob, language; repeatable)"

// invariantsFlags holds the flags every invariants subcommand shares.
var invariantsFlags struct {
	JSON bool
}

var invariantsCmd = &cobra.Command{
	Use:     "invariants",
	Aliases: []string{"invariant"},
	Short:   "Manage the account's baseline invariants",
	Long: "Baseline invariants are standing rules that every verification checks\n" +
		"on top of a session's own acceptance criteria. An invariant with no\n" +
		"repositories is account-scoped and applies to every repo; one with\n" +
		"repositories applies to exactly those. Conditions narrow it further by\n" +
		"changed file path or language.\n" +
		"\n" +
		"Listing and categories work with any API token, account-scoped ones\n" +
		"included. Creating, editing, deleting and changing status act as the\n" +
		"caller, so they need a user access token (from `aviator login` or a\n" +
		"personal token) whose user is a maintainer or admin; an account-scoped\n" +
		"token is refused for them.",
}

var invariantsListFlags struct {
	Repo    string
	Status  string
	IDs     []int
	Sources []string
	Page    int
	Limit   int
}

var invariantsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List baseline invariants, newest first",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if invariantsListFlags.Page < 1 {
			return errors.New("--page starts at 1")
		}
		if invariantsListFlags.Limit < 1 {
			return errors.New("--limit must be at least 1")
		}
		query := api.ListInvariantsQuery{
			Status:  invariantsListFlags.Status,
			IDs:     invariantsListFlags.IDs,
			Sources: invariantsListFlags.Sources,
			Page:    invariantsListFlags.Page,
			PerPage: invariantsListFlags.Limit,
		}
		if invariantsListFlags.Repo != "" {
			repo, err := parseRepo(invariantsListFlags.Repo)
			if err != nil {
				return err
			}
			query.Repo = &repo
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		raw, resp, err := client.ListInvariants(cmd.Context(), query)
		if err != nil {
			return err
		}
		if invariantsFlags.JSON {
			return printJSON(raw)
		}
		fmt.Print(formatInvariantList(resp))
		return nil
	},
}

var invariantsCategoriesCmd = &cobra.Command{
	Use:   "categories",
	Short: "List the category slugs that create and edit accept",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		raw, resp, err := client.ListInvariantCategories(cmd.Context())
		if err != nil {
			return err
		}
		if invariantsFlags.JSON {
			return printJSON(raw)
		}
		fmt.Print(formatInvariantCategories(resp))
		return nil
	},
}

var invariantsCreateFlags struct {
	Title      string
	Body       string
	BodyFile   string
	Category   string
	Repos      []string
	Conditions []string
	Disable    bool
}

var invariantsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a baseline invariant",
	Long: "Create a baseline invariant. With no --repo it is account-scoped and\n" +
		"applies to every repository; pass --repo (repeatable) to scope it to\n" +
		"specific ones. The category must be one of the account's category slugs\n" +
		"(see `aviator invariants categories`); an unknown slug is rejected with\n" +
		"the available ones listed.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := collectBody(invariantsCreateFlags.Body, invariantsCreateFlags.BodyFile)
		if err != nil {
			return err
		}
		if body == "" {
			return errors.New("--body (or --body-file) is required")
		}
		repos, err := parseRepos(invariantsCreateFlags.Repos)
		if err != nil {
			return err
		}
		conditions, err := parseConditions(invariantsCreateFlags.Conditions)
		if err != nil {
			return err
		}
		req := api.CreateInvariantRequest{
			Title:        invariantsCreateFlags.Title,
			Body:         body,
			Category:     invariantsCreateFlags.Category,
			Repositories: repos,
			Conditions:   conditions,
		}
		if invariantsCreateFlags.Disable {
			enabled := false
			req.Enabled = &enabled
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		raw, inv, err := client.CreateInvariant(cmd.Context(), req)
		if err != nil {
			return err
		}
		if invariantsFlags.JSON {
			return printJSON(raw)
		}
		fmt.Printf("%s Invariant #%d created\n", colors.Success("✓"), inv.ID)
		fmt.Print(formatInvariantDetail(inv))
		return nil
	},
}

// invariantEditFlags holds the `edit` flag values. Which of them were actually passed
// is read from the flag set, since "not passed" and the zero value mean
// different things for a partial update.
type invariantEditFlags struct {
	Title         string
	Body          string
	BodyFile      string
	Category      string
	Repos         []string
	AccountScoped bool
	Conditions    []string
	NoConditions  bool
	Enable        bool
	Disable       bool
}

var invariantsEditFlags invariantEditFlags

var invariantsEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Update a baseline invariant's fields, scope, or conditions",
	Long: "Update a baseline invariant. Only the flags you pass change; the rest\n" +
		"is left as is. --repo and --condition replace the current set wholesale,\n" +
		"so repeat them to give the full new set. --account-scoped drops every\n" +
		"repository link, and --no-conditions drops every condition.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseInvariantID(args[0])
		if err != nil {
			return err
		}
		req, err := buildUpdateRequest(cmd.Flags(), &invariantsEditFlags)
		if err != nil {
			return err
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		raw, inv, err := client.UpdateInvariant(cmd.Context(), id, req)
		if err != nil {
			return err
		}
		if invariantsFlags.JSON {
			return printJSON(raw)
		}
		fmt.Printf("%s Invariant #%d updated\n", colors.Success("✓"), inv.ID)
		fmt.Print(formatInvariantDetail(inv))
		return nil
	},
}

// registerEditFlags binds the edit flags onto fs. It is separate from init so
// tests can parse a flag set without a cobra command.
func registerEditFlags(fs *pflag.FlagSet, f *invariantEditFlags) {
	fs.StringVar(&f.Title, "title", "", "new title")
	registerBodyFlags(fs, &f.Body, &f.BodyFile, "new rule body")
	fs.StringVar(&f.Category, "category", "", "new category slug")
	fs.StringArrayVar(&f.Repos, "repo", nil,
		"replace the repository scope with these owner/repo entries (repeatable)")
	fs.BoolVar(&f.AccountScoped, "account-scoped", false,
		"drop every repository link so the invariant applies account-wide")
	fs.StringArrayVar(&f.Conditions, "condition", nil,
		"replace the conditions with these (same syntax as create --condition)")
	fs.BoolVar(&f.NoConditions, "no-conditions", false, "drop every condition")
	fs.BoolVar(&f.Enable, "enable", false,
		"switch the invariant on (active invariants only; approve a pending one instead)")
	fs.BoolVar(&f.Disable, "disable", false,
		"switch the invariant off (active invariants only; it stays active but is not applied)")
}

// buildUpdateRequest turns the passed edit flags into a PATCH body. A flag
// that was not passed leaves its field nil so the server keeps the current
// value; --account-scoped and --no-conditions send an empty list to clear.
func buildUpdateRequest(flags *pflag.FlagSet, f *invariantEditFlags) (api.UpdateInvariantRequest, error) {
	var req api.UpdateInvariantRequest
	if flags.Changed("title") {
		req.Title = &f.Title
	}
	if flags.Changed("body") || flags.Changed("body-file") {
		body, err := collectBody(f.Body, f.BodyFile)
		if err != nil {
			return req, err
		}
		req.Body = &body
	}
	if flags.Changed("category") {
		req.Category = &f.Category
	}
	switch {
	case flags.Changed("account-scoped") && f.AccountScoped:
		req.Repositories = &[]api.Repository{}
	case flags.Changed("repo"):
		repos, err := parseRepos(f.Repos)
		if err != nil {
			return req, err
		}
		req.Repositories = &repos
	}
	switch {
	case flags.Changed("no-conditions") && f.NoConditions:
		req.Conditions = &[]api.InvariantCondition{}
	case flags.Changed("condition"):
		conditions, err := parseConditions(f.Conditions)
		if err != nil {
			return req, err
		}
		req.Conditions = &conditions
	}
	switch {
	case flags.Changed("enable"):
		enabled := f.Enable
		req.Enabled = &enabled
	case flags.Changed("disable"):
		enabled := !f.Disable
		req.Enabled = &enabled
	}
	if req == (api.UpdateInvariantRequest{}) {
		return req, errors.New("nothing to change; pass at least one field flag (see --help)")
	}
	return req, nil
}

var invariantsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a baseline invariant and its conditions",
	Long: "Delete a baseline invariant outright. To take one out of service while\n" +
		"keeping a record of it, use `aviator invariants reject` instead.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseInvariantID(args[0])
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		raw, resp, err := client.DeleteInvariant(cmd.Context(), id)
		if err != nil {
			return err
		}
		if invariantsFlags.JSON {
			return printJSON(raw)
		}
		fmt.Printf("%s Invariant #%d deleted\n", colors.Success("✓"), resp.DeletedInvariantID)
		return nil
	},
}

var invariantsApproveCmd = &cobra.Command{
	Use:   "approve <id>...",
	Short: "Mark invariants active so verifications start applying them",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSetInvariantStatus(cmd, "active", args)
	},
}

var invariantsRejectCmd = &cobra.Command{
	Use:   "reject <id>...",
	Short: "Mark invariants rejected; they stop applying but stay on record",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSetInvariantStatus(cmd, "rejected", args)
	},
}

var invariantsSetStatusCmd = &cobra.Command{
	Use:   "set-status <status> <id>...",
	Short: "Set invariants to any status (" + invariantStatuses + ")",
	Long: "Set the lifecycle status of one or more invariants. `approve` and\n" +
		"`reject` cover the common cases; this also restores a rejected\n" +
		"invariant to pending. Enabled is derived from the status: active\n" +
		"enables, anything else disables. If any id is not under your account\n" +
		"the whole batch is refused.",
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSetInvariantStatus(cmd, args[0], args[1:])
	},
}

func runSetInvariantStatus(cmd *cobra.Command, status string, rawIDs []string) error {
	ids := make([]int, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := parseInvariantID(raw)
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	raw, resp, err := client.SetInvariantStatus(cmd.Context(), api.SetInvariantStatusRequest{
		InvariantIDs: ids,
		Status:       status,
	})
	if err != nil {
		return err
	}
	if invariantsFlags.JSON {
		return printJSON(raw)
	}
	fmt.Printf("%s %s now %s\n", colors.Success("✓"),
		english.Plural(len(resp.Invariants), "invariant", ""), status)
	fmt.Print(formatInvariantTable(resp.Invariants))
	return nil
}

func init() {
	invariantsCmd.PersistentFlags().BoolVar(&invariantsFlags.JSON, "json", false,
		"print the raw response as pretty JSON")

	lf := invariantsListCmd.Flags()
	lf.StringVar(&invariantsListFlags.Repo, "repo", "",
		"only invariants that apply to this owner/repo (account-scoped ones included)")
	lf.StringVar(&invariantsListFlags.Status, "status", "", "only this status ("+invariantStatuses+")")
	lf.IntSliceVar(&invariantsListFlags.IDs, "ids", nil, "only these invariant ids (comma-separated)")
	lf.StringSliceVar(&invariantsListFlags.Sources, "source", nil,
		"only these sources (comma-separated: "+invariantSources+")")
	lf.IntVar(&invariantsListFlags.Page, "page", 1, "page number, starting at 1")
	lf.IntVar(&invariantsListFlags.Limit, "limit", 20, "invariants per page (max 100)")

	cf := invariantsCreateCmd.Flags()
	cf.StringVar(&invariantsCreateFlags.Title, "title", "", "short name for the rule")
	registerBodyFlags(cf, &invariantsCreateFlags.Body, &invariantsCreateFlags.BodyFile,
		"the rule itself, as the verifier should read it")
	cf.StringVar(&invariantsCreateFlags.Category, "category", "", "category slug, one of those aviator invariants categories lists")
	cf.StringArrayVar(&invariantsCreateFlags.Repos, "repo", nil,
		"scope to this owner/repo (repeatable; omit for account-wide)")
	cf.StringArrayVar(&invariantsCreateFlags.Conditions, "condition", nil, conditionFlagHelp)
	cf.BoolVar(&invariantsCreateFlags.Disable, "disable", false, "create it switched off")
	_ = invariantsCreateCmd.MarkFlagRequired("title")
	_ = invariantsCreateCmd.MarkFlagRequired("category")
	invariantsCreateCmd.MarkFlagsMutuallyExclusive("body", "body-file")

	registerEditFlags(invariantsEditCmd.Flags(), &invariantsEditFlags)
	invariantsEditCmd.MarkFlagsMutuallyExclusive("body", "body-file")
	invariantsEditCmd.MarkFlagsMutuallyExclusive("repo", "account-scoped")
	invariantsEditCmd.MarkFlagsMutuallyExclusive("condition", "no-conditions")
	invariantsEditCmd.MarkFlagsMutuallyExclusive("enable", "disable")

	invariantsCmd.AddCommand(
		invariantsListCmd,
		invariantsCategoriesCmd,
		invariantsCreateCmd,
		invariantsEditCmd,
		invariantsDeleteCmd,
		invariantsApproveCmd,
		invariantsRejectCmd,
		invariantsSetStatusCmd,
	)
}

// parseInvariantID accepts a bare number or the #<number> form the CLI prints.
func parseInvariantID(arg string) (int, error) {
	s := strings.TrimPrefix(strings.TrimSpace(arg), "#")
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, errors.Errorf("invalid invariant ID %q, expected a positive number", arg)
	}
	return n, nil
}

// parseCondition reads one --condition value: <type>=<value> to require a
// match, <type>!=<value> to require a non-match. The type is not validated
// here; the server rejects unknown ones.
func parseCondition(s string) (api.InvariantCondition, error) {
	i := strings.Index(s, "=")
	if i <= 0 {
		return api.InvariantCondition{}, errors.Errorf(
			"invalid condition %q, expected <type>=<value> or <type>!=<value>", s)
	}
	typ, negate := s[:i], false
	if strings.HasSuffix(typ, "!") {
		typ, negate = strings.TrimSuffix(typ, "!"), true
	}
	typ = strings.TrimSpace(typ)
	value := strings.TrimSpace(s[i+1:])
	if typ == "" || value == "" {
		return api.InvariantCondition{}, errors.Errorf(
			"invalid condition %q, expected <type>=<value> or <type>!=<value>", s)
	}
	return api.InvariantCondition{Type: typ, Value: value, Negate: negate}, nil
}

func parseConditions(entries []string) ([]api.InvariantCondition, error) {
	conditions := make([]api.InvariantCondition, 0, len(entries))
	for _, entry := range entries {
		c, err := parseCondition(entry)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, c)
	}
	return conditions, nil
}
