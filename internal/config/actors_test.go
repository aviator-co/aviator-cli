//go:build experimental

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeActors(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "actors.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadActors(t *testing.T) {
	path := writeActors(t, `actors:
  - name: Front End Agent
    role: coding_agent
    description: Reviews front end changes for UX problems or bad page structure.
`)
	actors, err := LoadActors(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := LocalActor{
		Name:        "Front End Agent",
		Role:        "coding_agent",
		Description: "Reviews front end changes for UX problems or bad page structure.",
	}
	if len(actors) != 1 || actors[0] != want {
		t.Errorf("actors = %+v", actors)
	}
}

func TestLoadActorsMissingOrEmpty(t *testing.T) {
	for name, path := range map[string]string{
		"missing": filepath.Join(t.TempDir(), "actors.yaml"),
		"empty":   writeActors(t, ""),
	} {
		actors, err := LoadActors(path)
		if err != nil || len(actors) != 0 {
			t.Errorf("%s: actors = %+v, err = %v", name, actors, err)
		}
	}
}

func TestLoadActorsUnknownKey(t *testing.T) {
	path := writeActors(t, "actors:\n  - Name: Front End Agent\n")
	if _, err := LoadActors(path); err == nil || !strings.Contains(err.Error(), "Name") {
		t.Errorf("err = %v, want it to name the unknown key", err)
	}
}

func TestActorsFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got, _ := ActorsFile(); got != filepath.Join("/xdg", "aviator", "actors.yaml") {
		t.Errorf("with XDG_CONFIG_HOME: %s", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/me")
	if got, _ := ActorsFile(); got != filepath.Join("/home/me", ".config", "aviator", "actors.yaml") {
		t.Errorf("without XDG_CONFIG_HOME: %s", got)
	}
}

func ptr(s string) *string { return &s }

func TestEditActorCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviator", "actors.yaml")
	actor, created, err := EditActor(path, "Front End Agent", ptr("coding_agent"), ptr("Reviews UX."))
	if err != nil || !created {
		t.Fatalf("created = %v, err = %v", created, err)
	}
	if actor != (LocalActor{Name: "Front End Agent", Role: "coding_agent", Description: "Reviews UX."}) {
		t.Errorf("actor = %+v", actor)
	}
	actors, err := LoadActors(path)
	if err != nil || len(actors) != 1 || actors[0] != actor {
		t.Errorf("reloaded = %+v, err = %v", actors, err)
	}
}

// TestEditActorOverride checks that only the passed fields change and that the
// rest of the file, comments included, is left as it was.
func TestEditActorOverride(t *testing.T) {
	path := writeActors(t, `# my actors
actors:
  - name: Front End Agent # the UX one
    role: coding_agent
    description: Reviews UX.
  - name: Backend Agent
    role: coding_agent
`)
	actor, created, err := EditActor(path, "Front End Agent", ptr("review_agent"), nil)
	if err != nil || created {
		t.Fatalf("created = %v, err = %v", created, err)
	}
	if actor != (LocalActor{Name: "Front End Agent", Role: "review_agent", Description: "Reviews UX."}) {
		t.Errorf("actor = %+v", actor)
	}
	data, _ := os.ReadFile(path)
	want := `# my actors
actors:
  - name: Front End Agent # the UX one
    role: review_agent
    description: Reviews UX.
  - name: Backend Agent
    role: coding_agent
`
	if string(data) != want {
		t.Errorf("file =\n%s\nwant\n%s", data, want)
	}
}

func TestEditActorAppends(t *testing.T) {
	path := writeActors(t, "actors:\n  - name: A\n    role: coding_agent\n")
	if _, created, err := EditActor(path, "B", ptr("123"), nil); err != nil || !created {
		t.Fatalf("created = %v, err = %v", created, err)
	}
	actors, err := LoadActors(path)
	if err != nil || len(actors) != 2 || actors[1] != (LocalActor{Name: "B", Role: "123"}) {
		t.Errorf("actors = %+v, err = %v", actors, err)
	}
}

func TestEditActorNullList(t *testing.T) {
	path := writeActors(t, "actors:\n")
	if _, _, err := EditActor(path, "A", ptr("coding_agent"), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actors, err := LoadActors(path); err != nil || len(actors) != 1 {
		t.Errorf("actors = %+v, err = %v", actors, err)
	}
}

func TestEditActorNewNeedsRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actors.yaml")
	if _, _, err := EditActor(path, "A", nil, ptr("d")); err == nil {
		t.Fatal("want an error for a new actor without a role")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file was written: %v", err)
	}
}

func TestEditActorRefusesUnreadableFile(t *testing.T) {
	original := "actors:\n  - Name: A\n"
	path := writeActors(t, original)
	if _, _, err := EditActor(path, "A", ptr("coding_agent"), nil); err == nil {
		t.Fatal("want an error for an unknown key")
	}
	if data, _ := os.ReadFile(path); string(data) != original {
		t.Errorf("file changed to %q", data)
	}
}
