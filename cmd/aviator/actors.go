//go:build experimental

package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

// personActor is the built-in actor for the signed-in user. It's sent as
// {"role": "person"} with no name, and the server names it after the Aviator
// user behind the token, so no one's name lives in the actors file.
const personActor = "person"

// actorsFlags holds the flags every actors subcommand shares.
var actorsFlags struct {
	JSON bool
}

var actorsCmd = &cobra.Command{
	Use:     "actors",
	Aliases: []string{"actor"},
	Short:   "List your actors and edit the local actors file",
	// Experimental: left out of help and completion, so only people who know
	// the command is there find it.
	Hidden: true,
	Long: "Actors are the agents and people that take part in decisions. Your\n" +
		"remote actors live in Aviator. You can also describe actors locally in\n" +
		"~/.config/aviator/actors.yaml (or $XDG_CONFIG_HOME/aviator/actors.yaml),\n" +
		"by hand or with `aviator actors edit`. The CLI never sends that file to\n" +
		"the API; the server reconciles it with the remote actors.\n" +
		"\n" +
		"People aren't kept in the file. The built-in actor \"" + personActor + "\" is always\n" +
		"you, and the server records it under your Aviator name.\n" +
		"\n" +
		"Listing remote actors acts as the caller, so it needs a user access\n" +
		"token (from `aviator login` or a personal token).",
}

var actorsListFlags struct {
	Local bool
}

var actorsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your actors",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if actorsListFlags.Local {
			return listLocalActors()
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.ListActors(cmd.Context())
		if err != nil {
			return err
		}
		if actorsFlags.JSON {
			return printJSON(actorListJSON{Actors: newActorsJSON(resp.Actors)})
		}
		fmt.Print(formatActorTable(resp.Actors))
		return nil
	},
}

func listLocalActors() error {
	path, err := config.ActorsFile()
	if err != nil {
		return err
	}
	actors, err := config.LoadActors(path)
	if err != nil {
		return err
	}
	if actorsFlags.JSON {
		return printJSON(localActorListJSON{File: path, Actors: newLocalActorsJSON(actors)})
	}
	fmt.Print(formatLocalActorTable(path, actors))
	return nil
}

var actorsEditFlags struct {
	Name        string
	Role        string
	Description string
}

var actorsEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Add an actor to the local actors file, or override one",
	Long: "Add the actor called --name to the local actors file, or override the\n" +
		"one already there. Only the flags you pass change; a new actor needs\n" +
		"--role. The file is edited in place, so its comments and ordering are\n" +
		"kept. Nothing is sent to the API.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		flags := cmd.Flags()
		if !flags.Changed("role") && !flags.Changed("description") {
			return errors.New("pass --role, --description, or both")
		}
		if actorsEditFlags.Name == personActor {
			return errors.Errorf("%q is built in: it's always you, under your Aviator name", personActor)
		}
		var role, description *string
		if flags.Changed("role") {
			switch strings.TrimSpace(actorsEditFlags.Role) {
			case "":
				return errors.New("--role can't be empty")
			case "person":
				return errors.Errorf("people aren't kept in the actors file; write as yourself with --actor %s",
					personActor)
			}
			role = &actorsEditFlags.Role
		}
		if flags.Changed("description") {
			description = &actorsEditFlags.Description
		}

		path, err := config.ActorsFile()
		if err != nil {
			return err
		}
		actor, created, err := config.EditActor(path, actorsEditFlags.Name, role, description)
		if err != nil {
			return err
		}
		if actorsFlags.JSON {
			return printJSON(actorEditJSON{File: path, Created: created, Actor: localActorJSON(actor)})
		}
		verb := "updated in"
		if created {
			verb = "added to"
		}
		fmt.Printf("%s Actor %q %s %s\n", colors.Success("✓"), actor.Name, verb, path)
		fmt.Printf("  role: %s\n", actor.Role)
		if actor.Description != "" {
			fmt.Printf("  description: %s\n", actor.Description)
		}
		return nil
	},
}

type actorJSON struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type actorListJSON struct {
	Actors []actorJSON `json:"actors"`
}

type actorEditJSON struct {
	File    string         `json:"file"`
	Created bool           `json:"created"`
	Actor   localActorJSON `json:"actor"`
}

type localActorJSON struct {
	Name        string `json:"name"`
	Role        string `json:"role"`
	Description string `json:"description"`
}

type localActorListJSON struct {
	File   string           `json:"file"`
	Actors []localActorJSON `json:"actors"`
}

func newActorJSON(a *api.Actor) actorJSON {
	return actorJSON{ID: a.ID, Name: a.Name, Role: a.Role}
}

func newActorsJSON(actors []api.Actor) []actorJSON {
	out := make([]actorJSON, 0, len(actors))
	for i := range actors {
		out = append(out, newActorJSON(&actors[i]))
	}
	return out
}

func newLocalActorsJSON(actors []config.LocalActor) []localActorJSON {
	out := make([]localActorJSON, 0, len(actors))
	for _, a := range actors {
		out = append(out, localActorJSON(a))
	}
	return out
}

// formatActorTable renders remote actors one per row under a header.
func formatActorTable(actors []api.Actor) string {
	if len(actors) == 0 {
		return "No actors yet.\n"
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tROLE\tNAME")
	for _, a := range actors {
		fmt.Fprintf(w, "#%d\t%s\t%s\n", a.ID, a.Role, a.Name)
	}
	_ = w.Flush()
	return b.String()
}

// formatLocalActorTable renders the local actors file, naming the file so an
// empty listing says where to add them.
func formatLocalActorTable(path string, actors []config.LocalActor) string {
	if len(actors) == 0 {
		return fmt.Sprintf("No local actors in %s.\n", path)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", colors.Faint(path))
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ROLE\tNAME\tDESCRIPTION")
	for _, a := range actors {
		fmt.Fprintf(w, "%s\t%s\t%s\n", a.Role, a.Name, strings.ReplaceAll(strings.TrimSpace(a.Description), "\n", " "))
	}
	_ = w.Flush()
	return b.String()
}

func init() {
	actorsCmd.PersistentFlags().BoolVar(&actorsFlags.JSON, "json", false,
		"print the result as a single JSON object instead of the human summary")

	actorsListCmd.Flags().BoolVar(&actorsListFlags.Local, "local", false,
		"list the actors in the local actors file instead of your remote ones")

	ef := actorsEditCmd.Flags()
	ef.StringVar(&actorsEditFlags.Name, "name", "", "the actor to add or override, matched exactly")
	ef.StringVar(&actorsEditFlags.Role, "role", "", "the actor's role (for example, coding_agent)")
	ef.StringVar(&actorsEditFlags.Description, "description", "", "what the actor is used for")
	_ = actorsEditCmd.MarkFlagRequired("name")

	actorsCmd.AddCommand(actorsListCmd, actorsEditCmd)
	rootCmd.AddCommand(actorsCmd)
}
