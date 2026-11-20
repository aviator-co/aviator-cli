package main

import (
	"fmt"
	"net/http"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/dustin/go-humanize/english"
	"github.com/spf13/cobra"
)

// artifactsFlags holds the flags every artifacts subcommand shares.
var artifactsFlags struct {
	JSON bool
}

var artifactsCmd = &cobra.Command{
	Use:     "artifacts",
	Aliases: []string{"artifact"},
	Short:   "Read and submit the artifacts that describe how a session's change is built",
	Long: "Artifacts are the diagrams and notes a session's review page shows\n" +
		"beside the code, so a reviewer can see how the change is built before\n" +
		"reading the diff. Each one has a Mermaid diagram, a markdown body, or\n" +
		"both, plus the files it's about.\n" +
		"\n" +
		"list and show read the current set, and show takes the id list prints.\n" +
		"submit replaces the whole set from a JSON file. To change one artifact,\n" +
		"read the set with list --json, edit it, put in your own author and the\n" +
		"current head_sha (or drop it), and submit every artifact again (see\n" +
		"submit --help).",
}

var artifactsListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List a session's current artifacts",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, set, err := fetchArtifacts(cmd, args[0])
		if err != nil {
			return err
		}
		if artifactsFlags.JSON {
			return printJSON(newArtifactSetResponseJSON(runbookNumber, set))
		}
		if set == nil {
			fmt.Println(noArtifactsMessage(runbookNumber))
			return nil
		}
		fmt.Print(formatArtifactSet(runbookNumber, set))
		return nil
	},
}

var artifactsShowCmd = &cobra.Command{
	Use:   "show <id> <artifact-id>",
	Short: "Show one artifact in full, with its raw Mermaid source",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, set, err := fetchArtifacts(cmd, args[0])
		if err != nil {
			return err
		}
		if set == nil {
			return errors.New(noArtifactsMessage(runbookNumber))
		}
		a, err := findArtifact(runbookNumber, set, args[1])
		if err != nil {
			return err
		}
		if artifactsFlags.JSON {
			return printJSON(newArtifactShowJSON(runbookNumber, set, a))
		}
		fmt.Print(formatArtifactDetail(set, a))
		return nil
	},
}

func fetchArtifacts(cmd *cobra.Command, arg string) (int, *api.ArtifactSet, error) {
	runbookNumber, err := parseRunbookID(arg)
	if err != nil {
		return 0, nil, err
	}
	client, err := api.NewClient()
	if err != nil {
		return 0, nil, err
	}
	resp, err := client.GetArtifacts(cmd.Context(), runbookNumber)
	if err != nil {
		return 0, nil, artifactsError(err)
	}
	return runbookNumber, resp.ArtifactSet, nil
}

func noArtifactsMessage(runbookNumber int) string {
	return fmt.Sprintf("No artifacts on %s yet. aviator artifacts submit adds them.", formatRunbookID(runbookNumber))
}

// findArtifact looks up the artifact whose id arg is, as a bare number or
// the #<number> form list prints, and lists the ones there are when it isn't
// one of them.
func findArtifact(runbookNumber int, set *api.ArtifactSet, arg string) (*api.Artifact, error) {
	id, err := parseID("artifact", arg)
	if err == nil {
		for i := range set.Artifacts {
			if set.Artifacts[i].ID == id {
				return &set.Artifacts[i], nil
			}
		}
	}
	var b strings.Builder
	if err != nil {
		b.WriteString(err.Error())
	} else {
		fmt.Fprintf(&b, "no artifact #%d", id)
	}
	fmt.Fprintf(&b, ". The artifacts on %s are:", formatRunbookID(runbookNumber))
	for i := range set.Artifacts {
		fmt.Fprintf(&b, "\n  #%d  %s", set.Artifacts[i].ID, set.Artifacts[i].Title)
	}
	return nil, errors.New(b.String())
}

var artifactsSubmitFlags struct {
	File string
}

var artifactsSubmitCmd = &cobra.Command{
	Use:   "submit <id>",
	Short: "Replace a session's artifacts with the set in a JSON file",
	Long: "Replace a session's artifacts with the set in a JSON file (- reads\n" +
		"stdin). The file is sent as is and the server checks it:\n" +
		"\n" +
		"  {\"summary\": \"Signed webhooks are checked before they reach the queue\",\n" +
		"   \"head_sha\": \"abc1234\",\n" +
		"   \"author\": {\"role\": \"coding_agent\", \"model\": \"claude-fable-5-1\",\n" +
		"              \"harness\": \"claude-code\", \"skills\": [\"go-backend\"]},\n" +
		"   \"artifacts\": [\n" +
		"     {\"category\": \"Services\", \"title\": \"Webhook signature gate\",\n" +
		"      \"diagram\": \"flowchart LR\\n  hook[Webhook] --> gate[Verify signature]:::new\\n  gate --> queue[Queue]:::changed\",\n" +
		"      \"body\": \"Requests with a bad signature get a 401 and never reach the queue.\",\n" +
		"      \"references\": [{\"type\": \"file\", \"target\": \"src/webhooks/gate.py:40-48\"}],\n" +
		"      \"pr_numbers\": [123]}\n" +
		"   ]}\n" +
		"\n" +
		"summary (optional, at most 500 characters) is a short primer for the\n" +
		"reviewer on what the artifacts cover; a blank one counts as none.\n" +
		"head_sha (optional, at most 64 characters, not blank) pins file\n" +
		"reference links to the commit the artifacts were written against;\n" +
		"without it they link at the commit of the run being viewed, or the\n" +
		"latest run. author (required) needs a role\n" +
		"(person, coding_agent, review_agent); add model, harness, and skills\n" +
		"when you know them, as for aviator decisions apply. artifacts holds 1 to\n" +
		"30 artifacts. Fields not shown here are rejected, including the id the\n" +
		"server gives each artifact.\n" +
		"\n" +
		"Each artifact needs a category (at most 100 characters), a title (at\n" +
		"most 200), and at least one of diagram and body. category, title, and\n" +
		"summary are trimmed, and a blank diagram or body counts as none. Errors\n" +
		"name an artifact by its position in the list and its title.\n" +
		"\n" +
		"  diagram     Mermaid source, up to 20000 characters. Nodes with the\n" +
		"              classes :::new, :::changed, and :::removed are\n" +
		"              highlighted on the review page.\n" +
		"  body        markdown, up to 20000 characters.\n" +
		"  references  optional, up to 50 files the artifact is about, each\n" +
		"              {\"type\": \"file\", \"target\": ...} where target is path,\n" +
		"              path:40, or path:40-48: at most 500 characters, no\n" +
		"              control characters, line numbers up to 7 digits.\n" +
		"  pr_numbers  optional, up to 50 pull request numbers the artifact\n" +
		"              covers.\n" +
		"\n" +
		"category is free text. Suggested ones are Services, Data model, API,\n" +
		"Frontend, User-facing copy, and Infrastructure.\n" +
		"\n" +
		"A submit replaces the whole set, so send every artifact every time, not\n" +
		"just the ones that changed. Earlier sets stay attached to the\n" +
		"verification runs that started with them. To change one artifact, take\n" +
		"the artifact_set from aviator artifacts list <id> --json, drop its id\n" +
		"and created_at and every artifact's id, edit it, and submit the result.\n" +
		"Replace its author with your own and its head_sha with the commit you\n" +
		"wrote against, or drop head_sha so links follow the run's commit.\n" +
		"Every submit gives each artifact a new id. Needs a user access token\n" +
		"(aviator login or a personal token).",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		body, err := readPayloadFile(artifactsSubmitFlags.File)
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.SubmitArtifacts(cmd.Context(), runbookNumber, body)
		if err != nil {
			return artifactsError(err)
		}
		if artifactsFlags.JSON {
			return printJSON(newArtifactSetResponseJSON(runbookNumber, resp.ArtifactSet))
		}
		count := 0
		if resp.ArtifactSet != nil {
			count = len(resp.ArtifactSet.Artifacts)
		}
		fmt.Printf("%s %s: %s submitted\n", colors.Success("✓"),
			formatRunbookID(runbookNumber), english.Plural(count, "artifact", ""))
		return nil
	},
}

// artifactsError rewrites the bare 404 or 405 a backend without the artifacts
// routes answers into one message saying so. A JSON not-found is a real
// unknown session and passes through unchanged.
func artifactsError(err error) error {
	var ae *api.Error
	if errors.As(err, &ae) && ae.Code == "" &&
		(ae.Status == http.StatusNotFound || ae.Status == http.StatusMethodNotAllowed) {
		return errors.Errorf("this Aviator backend doesn't support artifacts yet; %s %s returned %d",
			ae.Method, ae.Path, ae.Status)
	}
	return err
}

func init() {
	artifactsCmd.PersistentFlags().BoolVar(&artifactsFlags.JSON, "json", false,
		"print the result as a single JSON object instead of the human summary")

	artifactsSubmitCmd.Flags().StringVar(&artifactsSubmitFlags.File, "file", "", "the JSON artifact set file, or - for stdin")
	_ = artifactsSubmitCmd.MarkFlagRequired("file")

	artifactsCmd.AddCommand(
		artifactsListCmd,
		artifactsShowCmd,
		artifactsSubmitCmd,
	)
	rootCmd.AddCommand(artifactsCmd)
}
