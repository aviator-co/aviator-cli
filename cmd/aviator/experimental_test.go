//go:build experimental

package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestExperimentalCommandsHidden keeps the experimental commands out of help
// and shell completion, which Homebrew builds from `aviator completion`.
func TestExperimentalCommandsHidden(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"__complete", ""}} {
		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		for _, name := range []string{"actor"} {
			if strings.Contains(strings.ToLower(out.String()), name) {
				t.Errorf("%v lists %q:\n%s", args, name, out.String())
			}
		}
	}
	rootCmd.SetOut(nil)
	rootCmd.SetArgs(nil)
}
