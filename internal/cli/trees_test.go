package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The trees must not drift from the registry: every command names an
// existing tool, its positional arguments are arguments of that tool, every
// required argument is reachable, and every tool of the tree groups has a
// command.
func TestTrees_MatchRegistry(t *testing.T) {
	reg := tools.New(nil)
	covered := map[string]bool{}
	for _, g := range treeGroups {
		verbs := map[string]bool{}
		for _, c := range g.Cmds {
			t.Run(g.Name+" "+c.Verb, func(t *testing.T) {
				if verbs[c.Verb] {
					t.Fatalf("duplicate verb")
				}
				verbs[c.Verb] = true
				tl, ok := reg.Lookup(c.Tool)
				if !ok {
					t.Fatalf("no tool %q in the registry", c.Tool)
				}
				covered[c.Tool] = true
				schema := map[string]param{}
				for _, p := range schemaParams(tl) {
					schema[p.Name] = p
				}
				reachable := map[string]bool{}
				for _, p := range parsePositionals(c.Args) {
					for _, n := range p.names {
						if _, ok := schema[n]; !ok {
							t.Errorf("positional %q is not an argument of %s", n, c.Tool)
						}
						reachable[n] = true
					}
				}
				for _, p := range c.flagArgs(tl) {
					reachable[p.Name] = true
				}
				for arg := range c.Rename {
					if _, ok := schema[arg]; !ok {
						t.Errorf("rename of %q: not an argument of %s", arg, c.Tool)
					}
				}
				for _, p := range schema {
					if p.Required && !reachable[p.Name] {
						t.Errorf("required argument %q of %s has neither a positional nor a flag", p.Name, c.Tool)
					}
				}
			})
		}
	}
	for _, g := range []string{"vm", "disk", "vpc", "k8s", "pg", "s3", "secret"} {
		for _, tl := range reg.Tools() {
			if tl.Group == g && !covered[tl.Name] {
				t.Errorf("tool %s (group %s) has no command in the tree", tl.Name, g)
			}
		}
	}
}

// Building the root command defines every flag: a clash with a root flag
// panics here rather than for a user.
func TestTrees_BuildAndHelp(t *testing.T) {
	for _, g := range treeGroups {
		for _, c := range g.Cmds {
			if _, _, code := execCLI(t, g.Name, c.Verb, "--help"); code != 0 {
				t.Errorf("%s %s --help: exit %d", g.Name, c.Verb, code)
			}
		}
	}
}

func TestTrees_ConfirmationAndUsage(t *testing.T) {
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"disk", "delete", "d-1"}, clerrors.ExitConfirmationRequired},
		{[]string{"disk", "delete", "d-1", "--dry-run"}, clerrors.ExitOK},
		{[]string{"disk", "delete", "d-1", "--yes"}, clerrors.ExitAuth}, // past the gate, no credentials
		{[]string{"vm", "delete", "web"}, clerrors.ExitConfirmationRequired},
		{[]string{"s3", "bucket-create", "b1"}, clerrors.ExitConfirmationRequired},
		{[]string{"disk", "create", "d1"}, clerrors.ExitGeneral},                   // --size-gb is required
		{[]string{"disk", "create", "d1", "--size-gb", "x"}, clerrors.ExitGeneral}, // not an int
		{[]string{"disk", "delete"}, clerrors.ExitGeneral},                         // no id
		{[]string{"disk", "delete", "a", "b"}, clerrors.ExitGeneral},               // too many
		{[]string{"vm", "list"}, clerrors.ExitAuth},
	}
	for _, c := range cases {
		if _, errOut, code := execCLI(t, c.args...); code != c.want {
			t.Errorf("%v: exit %d, want %d (%s)", c.args, code, c.want, errOut)
		}
	}
	_, errOut, _ := execCLI(t, "disk", "create", "d1")
	if !strings.Contains(errOut, "--size-gb") {
		t.Errorf("missing flag not named: %s", errOut)
	}
}

// findLeaf returns the built command for "group verb".
func findLeaf(t *testing.T, group, verb string) (*cobra.Command, treeCmd, tools.Tool) {
	t.Helper()
	root := newRootCmd()
	cmd, _, err := root.Find([]string{group, verb})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range treeGroups {
		for _, c := range g.Cmds {
			if g.Name == group && c.Verb == verb {
				tl, _ := tools.New(nil).Lookup(c.Tool)
				return cmd, c, tl
			}
		}
	}
	t.Fatalf("no %s %s", group, verb)
	return nil, treeCmd{}, tools.Tool{}
}

func TestTreeArgs(t *testing.T) {
	parse := func(group, verb string, argv ...string) map[string]any {
		t.Helper()
		cmd, c, tl := findLeaf(t, group, verb)
		if err := cmd.ParseFlags(argv); err != nil {
			t.Fatal(err)
		}
		got, err := treeArgs(cmd, c, tl, parsePositionals(c.Args), c.flagArgs(tl), cmd.Flags().Args())
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := parse("vm", "get", "web"); got["name"] != "web" || got["vm_id"] != nil {
		t.Errorf("vm get by name: %v", got)
	}
	id := "85d1a925-d7a3-408f-b57a-234378f26186"
	if got := parse("vm", "get", id); got["vm_id"] != id {
		t.Errorf("vm get by id: %v", got)
	}
	got := parse("disk", "create", "d1", "--size-gb", "20", "--attach-to", "web")
	if got["name"] != "d1" || got["size_gb"] != float64(20) || got["attach_to"] != "web" {
		t.Errorf("disk create: %v", got)
	}
	got = parse("k8s", "kubectl", "c-1", "get", "pods", "-n", "kube-system")
	if got["cluster_id"] != "c-1" || got["args"] != "get pods -n kube-system" {
		t.Errorf("kubectl: %v", got)
	}
	got = parse("k8s", "helm", "c-1", "install", "--set", "a=1", "--set", "b=2")
	if got["action"] != "install" || len(got["set"].([]any)) != 2 {
		t.Errorf("helm: %v", got)
	}
	got = parse("s3", "bucket-create", "b1", "--s3-region", "ru-central-1")
	if got["region"] != "ru-central-1" {
		t.Errorf("s3 region rename: %v", got)
	}
	if got := parse("vpc", "fip-release"); len(got) != 0 {
		t.Errorf("optional positional: %v", got)
	}
}

func TestRenderAnswer(t *testing.T) {
	render := func(text string, cols []string) string {
		var buf bytes.Buffer
		p := output.NewWithWriter(&buf, output.ModePlain, false)
		if err := renderAnswer(p, &buf, text, cols); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	got := render(`{"disks":[{"id":"d-1","name":"boot","size_gb":40,"zone":"ru.AZ-1","attached_to":["web"]}]}`,
		[]string{"id", "name", "size_gb", "attached_to"})
	if !strings.HasPrefix(got, "ID") || !strings.Contains(got, "d-1") || !strings.Contains(got, "40") || !strings.Contains(got, "web") ||
		strings.Contains(got, "ru.AZ-1") {
		t.Errorf("list with columns:\n%s", got)
	}

	got = render(`{"count":1,"clusters":[{"status":"RUNNING","id":"c-1","name":"pg","extra":{"name":"spec"}}]}`, nil)
	if first := strings.Fields(strings.SplitN(got, "\n", 2)[0]); strings.Join(first, " ") != "ID NAME STATUS EXTRA" {
		t.Errorf("auto columns: %q", first)
	}
	if !strings.Contains(got, "spec") {
		t.Errorf("nested object should show its name:\n%s", got)
	}

	if got := render(`{"clusters":[]}`, nil); got != "No items.\n" {
		t.Errorf("empty list: %q", got)
	}

	got = render(`{"id":"vm-1","name":"web","private_ip":"10.0.0.2","interfaces":[{"id":"i-1"}]}`, nil)
	if !strings.HasPrefix(got, "FIELD") || !strings.Contains(got, "private_ip") || !strings.Contains(got, `[{"id":"i-1"}]`) {
		t.Errorf("object:\n%s", got)
	}

	if got := render(`not json`, nil); got != "not json\n" {
		t.Errorf("not JSON: %q", got)
	}
}
