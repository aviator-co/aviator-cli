package skill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aviator-co/aviator-cli/internal/adapter"
)

func TestInstallAddsUpdatesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	change, err := install(dir, "acceptance-criteria", []byte("v1"))
	if err != nil {
		t.Fatal(err)
	}
	if change != adapter.ChangeAdded {
		t.Fatalf("first install = %v, want ChangeAdded", change)
	}

	path := filepath.Join(dir, "acceptance-criteria", "SKILL.md")
	if b, _ := os.ReadFile(path); string(b) != "v1" {
		t.Fatalf("content = %q, want v1", b)
	}

	// Same content -> no change.
	change, _ = install(dir, "acceptance-criteria", []byte("v1"))
	if change != adapter.ChangeNone {
		t.Fatalf("re-install same = %v, want ChangeNone", change)
	}

	// New content -> update.
	change, _ = install(dir, "acceptance-criteria", []byte("v2"))
	if change != adapter.ChangeUpdated {
		t.Fatalf("changed content = %v, want ChangeUpdated", change)
	}
	if b, _ := os.ReadFile(path); string(b) != "v2" {
		t.Fatalf("content = %q, want v2", b)
	}
}

func TestUninstallRemovesInstalledSkills(t *testing.T) {
	dir := t.TempDir()
	for _, name := range Names {
		if _, err := install(dir, name, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}

	change, err := Uninstall(dir)
	if err != nil {
		t.Fatal(err)
	}
	if change != adapter.ChangeRemoved {
		t.Fatalf("uninstall = %v, want ChangeRemoved", change)
	}
	for _, name := range Names {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("skill %q still present after uninstall", name)
		}
	}
}

func TestUninstallMissingIsNoOp(t *testing.T) {
	change, err := Uninstall(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if change != adapter.ChangeNone {
		t.Fatalf("uninstall of nothing = %v, want ChangeNone", change)
	}
}
