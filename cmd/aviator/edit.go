package main

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var editFlags struct {
	Intent          string
	Criteria        []string
	CriteriaFile    string
	ExpectedVersion int
	Preview         string
	EvidenceSource  string
	JSON            bool
}

var editCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Update a review or runbook session's intent, acceptance criteria, preview config, or evidence source (e.g. aviator edit r/123)",
	Long: "Update a session's intent, replace its acceptance criteria, switch the\n" +
		"preview config its runs use (--preview), or change where its evidence\n" +
		"comes from (--evidence-source aviator|external), in any combination.\n" +
		"\n" +
		"A preview config choice sticks until changed. Going back to the repo\n" +
		"default means passing that config's name.\n" +
		"\n" +
		"Replacing criteria needs --expected-version, the version `aviator show`\n" +
		"prints; a stale version is refused.\n" +
		"Edits don't start a verification run: follow a criteria edit with\n" +
		"`aviator verify r/<number>`.\n" +
		"\n" +
		"When a criterion is wrong: " + docsResults + "#the-criterion-is-wrong",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		criteria, err := collectCriteria(editFlags.Criteria, editFlags.CriteriaFile)
		if err != nil {
			return err
		}
		if editFlags.Intent == "" && len(criteria) == 0 && editFlags.Preview == "" && editFlags.EvidenceSource == "" {
			return errors.New("pass --intent, --criteria (or --criteria-file), --preview, or --evidence-source")
		}
		if err := checkEvidenceSource(editFlags.EvidenceSource); err != nil {
			return err
		}
		if len(criteria) > 0 && !cmd.Flags().Changed("expected-version") {
			return errors.New("--expected-version is required when replacing criteria")
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.EditVerify(cmd.Context(), runbookNumber, api.EditVerifyRequest{
			Intent:             editFlags.Intent,
			AcceptanceCriteria: criteria,
			ExpectedVersion:    editFlags.ExpectedVersion,
			PreviewConfigName:  editFlags.Preview,
			EvidenceSource:     editFlags.EvidenceSource,
		})
		if err != nil {
			return err
		}
		warnPreviewNotApplied(editFlags.Preview, resp.PreviewConfigName)
		if editFlags.JSON {
			return printJSON(newEditJSON(resp))
		}

		id := formatRunbookID(resp.RunbookNumber)
		fmt.Printf("%s %s updated%s\n", colors.Success("✓"), id, formatVersion(resp.Version))
		if name := deref(resp.PreviewConfigName); editFlags.Preview != "" && name != "" {
			fmt.Printf("  Preview config: %s\n", name)
		}
		if editFlags.EvidenceSource != "" {
			fmt.Printf("  Evidence source: %s\n", resp.EvidenceSource)
		}
		if len(criteria) > 0 {
			fmt.Printf("  %s\n", colors.Faint("Verify with: aviator verify "+id))
		}
		return nil
	},
}

func init() {
	registerCriteriaFlags(editCmd, &editFlags.Criteria, &editFlags.CriteriaFile)
	f := editCmd.Flags()
	f.StringVar(&editFlags.Intent, "intent", "", "new intent for the session")
	f.IntVar(&editFlags.ExpectedVersion, "expected-version", 0,
		"session version you expect to be editing (required with --criteria; guards against stale edits)")
	f.StringVar(&editFlags.Preview, "preview", "",
		"preview config the session's runs use from now on, instead of the repo default; sticks until changed")
	f.StringVar(&editFlags.EvidenceSource, "evidence-source", "",
		"where runtime evidence comes from: aviator, or external for evidence you submit yourself")
	f.BoolVar(&editFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")
}

type editJSON struct {
	sessionRef
	Intent            string  `json:"intent"`
	Version           *int    `json:"version"`
	PreviewConfigName *string `json:"preview_config_name"`
	EvidenceSource    string  `json:"evidence_source"`
}

func newEditJSON(resp *api.VerifySession) editJSON {
	return editJSON{
		sessionRef:        newSessionRef(resp.RunbookNumber, resp.URL),
		Intent:            resp.Intent,
		Version:           resp.Version,
		PreviewConfigName: resp.PreviewConfigName,
		EvidenceSource:    resp.EvidenceSource,
	}
}
