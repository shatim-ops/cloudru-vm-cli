package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Every example names a real command, and every --flag in it exists on the
// command it runs: a copied example must not fail (the guide copies them).
func TestCommandExamples_AreValid(t *testing.T) {
	root := newRootCmd()
	for path, ex := range commandExamples {
		cmd, _, err := root.Find(strings.Fields(path)[1:])
		if err != nil || cmd.CommandPath() != path {
			t.Errorf("example for unknown command %q", path)
			continue
		}
		if cmd.Example != ex {
			t.Errorf("%s: example not applied", path)
		}
		for _, line := range strings.Split(ex, "\n") {
			checkExampleLine(t, root, line)
		}
	}
}

func checkExampleLine(t *testing.T, root *cobra.Command, line string) {
	t.Helper()
	// Only the evo part of a pipeline or a chain: "evo deploy --yes && evo verify".
	for _, part := range strings.FieldsFunc(line, func(r rune) bool { return r == '|' || r == '&' || r == '>' }) {
		f := strings.Fields(part)
		// The last "evo": in "claude mcp add … evo -- evo mcp" the first one
		// is the server's name.
		i := len(f) - 1
		for i >= 0 && f[i] != "evo" {
			i--
		}
		if i < 0 {
			continue
		}
		cmd, rest, err := root.Find(f[i+1:])
		if err != nil {
			t.Errorf("%q: %v", line, err)
			continue
		}
		for _, a := range rest {
			if !strings.HasPrefix(a, "--") {
				continue
			}
			name := strings.TrimPrefix(strings.SplitN(a, "=", 2)[0], "--")
			if cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil && root.PersistentFlags().Lookup(name) == nil {
				t.Errorf("%q: %s has no flag --%s", line, cmd.CommandPath(), name)
			}
		}
	}
}
