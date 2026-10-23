//go:build experimental

package main

import "testing"

func TestActorsEditRefusesPeople(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, args := range [][]string{
		{"actors", "edit", "--name", "person", "--role", "coding_agent"},
		{"actors", "edit", "--name", "Sam Rivera", "--role", "person"},
	} {
		actorsEditFlags.Name, actorsEditFlags.Role = "", ""
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}
