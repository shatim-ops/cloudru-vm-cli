package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The command trees (vm, disk, vpc, k8s, pg, s3, secret) are data: each
// command names a registry tool, which of its arguments are positional, and
// how the answer is shown. Every other argument of the tool becomes a flag
// with the type from its schema. The cobra layer only parses flags into
// map[string]any, calls the tool and formats the answer.

// treeGroup is one top-level command.
type treeGroup struct {
	Name  string
	Short string
	Cmds  []treeCmd
}

// treeCmd is one command of a tree.
//
// Args lists the positional arguments as tool argument names:
//
//	"disk_id"     required
//	"id?"         optional
//	"args..."     the rest of the command line, joined with spaces
//	"vm_id|name"  required; goes to vm_id when it is a UUID, to name otherwise
type treeCmd struct {
	Verb    string
	Tool    string
	Args    []string
	Rename  map[string]string // tool argument → flag name, when the default clashes
	Columns []string          // table columns for a list answer (default: picked from the rows)
}

var treeGroups = []treeGroup{
	{Name: "vm", Short: "Virtual machines", Cmds: []treeCmd{
		{Verb: "list", Tool: "vm_list", Columns: []string{"id", "name", "state", "zone", "created_time"}},
		{Verb: "get", Tool: "vm_get", Args: []string{"vm_id|name"}},
		{Verb: "create", Tool: "provision", Args: []string{"name"}},
		{Verb: "delete", Tool: "destroy", Args: []string{"vm_id|name"}},
		{Verb: "zones", Tool: "list_zones"},
		{Verb: "flavors", Tool: "list_flavors"},
	}},
	{Name: "disk", Short: "Disks", Cmds: []treeCmd{
		{Verb: "list", Tool: "disk_list", Columns: []string{"id", "name", "size_gb", "state", "disk_type", "zone", "attached_to"}},
		{Verb: "types", Tool: "list_disk_types"},
		{Verb: "create", Tool: "disk_create", Args: []string{"name"}},
		{Verb: "attach", Tool: "disk_attach", Args: []string{"disk_id"}},
		{Verb: "detach", Tool: "disk_detach", Args: []string{"disk_id"}},
		{Verb: "resize", Tool: "disk_resize", Args: []string{"disk_id"}},
		{Verb: "delete", Tool: "disk_delete", Args: []string{"disk_id"}},
	}},
	{Name: "vpc", Short: "Networks: VPCs, routes, subnets, security groups, floating IPs", Cmds: []treeCmd{
		{Verb: "list", Tool: "vpc_list"},
		{Verb: "get", Tool: "vpc_get", Args: []string{"id"}},
		{Verb: "create", Tool: "vpc_create", Args: []string{"name"}},
		{Verb: "delete", Tool: "vpc_delete", Args: []string{"id"}},
		{Verb: "operation", Tool: "vpc_operation", Args: []string{"operation_id"}},
		{Verb: "routes", Tool: "vpc_routes", Args: []string{"vpc_id"}},
		{Verb: "route-create", Tool: "vpc_route_create", Args: []string{"vpc_id"}},
		{Verb: "route-delete", Tool: "vpc_route_delete", Args: []string{"vpc_id", "route_id"}},
		{Verb: "subnets", Tool: "list_subnets"},
		{Verb: "security-groups", Tool: "list_security_groups"},
		{Verb: "fips", Tool: "list_floating_ips"},
		{Verb: "fip-attach", Tool: "fip_attach", Args: []string{"fip"}},
		{Verb: "fip-release", Tool: "release_floating_ip", Args: []string{"id?"}},
	}},
	{Name: "k8s", Short: "Managed Kubernetes clusters", Cmds: []treeCmd{
		{Verb: "list", Tool: "k8s_list"},
		{Verb: "get", Tool: "k8s_get", Args: []string{"cluster_id"}},
		{Verb: "options", Tool: "k8s_options"},
		{Verb: "plan", Tool: "k8s_plan"},
		{Verb: "create", Tool: "k8s_create"},
		{Verb: "delete", Tool: "k8s_delete", Args: []string{"cluster_id"}},
		{Verb: "kubeconfig", Tool: "k8s_kubeconfig", Args: []string{"cluster_id"}},
		{Verb: "nodepools", Tool: "k8s_nodepools", Args: []string{"cluster_id"}},
		{Verb: "add-nodepool", Tool: "k8s_add_nodepool", Args: []string{"cluster_id"}},
		{Verb: "kubectl", Tool: "k8s_kubectl", Args: []string{"cluster_id", "args..."}},
		{Verb: "apply", Tool: "k8s_apply", Args: []string{"cluster_id"}},
		{Verb: "helm", Tool: "k8s_helm", Args: []string{"cluster_id", "action"}},
	}},
	{Name: "pg", Short: "Managed PostgreSQL clusters, databases, users, backups", Cmds: []treeCmd{
		{Verb: "list", Tool: "pg_list"},
		{Verb: "get", Tool: "pg_get", Args: []string{"cluster_id"}},
		{Verb: "versions", Tool: "pg_versions"},
		{Verb: "specs", Tool: "pg_specs"},
		{Verb: "create", Tool: "pg_create", Args: []string{"name"}},
		{Verb: "delete", Tool: "pg_delete", Args: []string{"cluster_id"}},
		{Verb: "operation", Tool: "pg_operation", Args: []string{"operation_id"}},
		{Verb: "databases", Tool: "pg_databases", Args: []string{"cluster_id"}},
		{Verb: "db-create", Tool: "pg_db_create", Args: []string{"cluster_id", "name"}},
		{Verb: "db-delete", Tool: "pg_db_delete", Args: []string{"cluster_id", "name"}},
		{Verb: "users", Tool: "pg_users", Args: []string{"cluster_id"}},
		{Verb: "user-create", Tool: "pg_user_create", Args: []string{"cluster_id", "name"}},
		{Verb: "user-delete", Tool: "pg_user_delete", Args: []string{"cluster_id", "name"}},
		{Verb: "backups", Tool: "pg_backups", Args: []string{"cluster_id"}},
		{Verb: "backup-create", Tool: "pg_backup_create", Args: []string{"cluster_id", "name"}},
	}},
	{Name: "s3", Short: "Object Storage: buckets and objects", Cmds: []treeCmd{
		{Verb: "buckets", Tool: "s3_buckets"},
		{Verb: "ls", Tool: "s3_ls", Args: []string{"bucket"}},
		{Verb: "get", Tool: "s3_get", Args: []string{"bucket", "key"}},
		{Verb: "put", Tool: "s3_put", Args: []string{"bucket", "key"}},
		{Verb: "rm", Tool: "s3_rm", Args: []string{"bucket", "key"}},
		{Verb: "bucket-create", Tool: "s3_bucket_create", Args: []string{"bucket"},
			Rename: map[string]string{"region": "s3-region"}},
		{Verb: "bucket-delete", Tool: "s3_bucket_delete", Args: []string{"bucket"}},
	}},
	{Name: "secret", Short: "Secret Manager", Cmds: []treeCmd{
		{Verb: "list", Tool: "secret_list"},
		{Verb: "get", Tool: "secret_get", Args: []string{"path"}},
		{Verb: "value", Tool: "secret_value", Args: []string{"path"}},
		{Verb: "create", Tool: "secret_create", Args: []string{"path"}},
		{Verb: "add-version", Tool: "secret_add_version", Args: []string{"path"}},
		{Verb: "delete", Tool: "secret_delete", Args: []string{"path"}},
	}},
}

// rootArgs are tool arguments served by root flags (--project-id,
// --region) through the engine configuration, and confirmed, which is
// given with --yes or at the prompt.
var rootArgs = map[string]bool{"project_id": true, "region": true, "confirmed": true}

// newTreeCmds builds the command trees from treeGroups.
func newTreeCmds() []*cobra.Command {
	reg := newRegistry()
	var out []*cobra.Command
	for _, g := range treeGroups {
		short := g.Short
		if ru, ok := treeGroupShortRU[g.Name]; ok {
			short = i18n.T(g.Short, ru)
		}
		group := asGroup(&cobra.Command{Use: g.Name, Short: short})
		for _, c := range g.Cmds {
			t, ok := reg.Lookup(c.Tool)
			if !ok {
				panic("command tree: no tool " + c.Tool) // caught by TestTrees_MatchRegistry
			}
			group.AddCommand(newTreeLeaf(c, t))
		}
		out = append(out, group)
	}
	return out
}

// positional is one parsed entry of treeCmd.Args.
type positional struct {
	names    []string // one name, or two for "id|name"
	optional bool
	rest     bool
}

func parsePositionals(specs []string) []positional {
	out := make([]positional, 0, len(specs))
	for _, s := range specs {
		p := positional{}
		switch {
		case strings.HasSuffix(s, "..."):
			p.rest, s = true, strings.TrimSuffix(s, "...")
		case strings.HasSuffix(s, "?"):
			p.optional, s = true, strings.TrimSuffix(s, "?")
		}
		p.names = strings.Split(s, "|")
		out = append(out, p)
	}
	return out
}

// flagName is the flag for a tool argument: snake_case → kebab-case.
func (c treeCmd) flagName(arg string) string {
	if n, ok := c.Rename[arg]; ok {
		return n
	}
	return strings.ReplaceAll(arg, "_", "-")
}

// flagArgs are the tool arguments that become flags: everything in the
// schema that is not positional and not served by the root.
func (c treeCmd) flagArgs(t tools.Tool) []param {
	pos := map[string]bool{}
	for _, p := range parsePositionals(c.Args) {
		for _, n := range p.names {
			pos[n] = true
		}
	}
	var out []param
	for _, p := range schemaParams(t) {
		if pos[p.Name] || (rootArgs[p.Name] && c.Rename[p.Name] == "") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func newTreeLeaf(c treeCmd, t tools.Tool) *cobra.Command {
	pos := parsePositionals(c.Args)
	use := []string{c.Verb}
	minArgs, maxArgs := 0, 0
	for _, p := range pos {
		label := strings.ReplaceAll(strings.Join(p.names, "|"), "_", "-")
		switch {
		case p.rest:
			use = append(use, "<"+label+">...")
			maxArgs = -1
		case p.optional:
			use = append(use, "["+label+"]")
			maxArgs++
		default:
			use = append(use, "<"+label+">")
			minArgs++
			maxArgs++
		}
	}
	if len(pos) > 0 && pos[len(pos)-1].rest {
		minArgs++
	}

	short := summary(t.Description, 100)
	long := t.Description + "\n\nRuns the " + t.Name + " tool (see 'evo tools describe " + t.Name + "')."
	if t.Mutating {
		long += "\nAsks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only."
	}
	if i18n.Lang() == i18n.RU {
		if ru, ok := toolShortRU[t.Name]; ok {
			short = ru
		}
		long = short + ".\n\nВызывает инструмент " + t.Name + " (подробнее: 'evo tools describe " + t.Name + "').\n" +
			"Описание инструмента и его флагов — на английском, как в MCP:\n\n" + t.Description
		if t.Mutating {
			long += "\n\nСпрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью."
		}
	}

	cmd := &cobra.Command{
		Use:   strings.Join(use, " "),
		Short: short,
		Long:  long,
		Args: func(cmd *cobra.Command, args []string) error {
			if maxArgs == 0 && len(args) > 0 {
				return fmt.Errorf(i18n.T("%s takes no arguments, got %q", "%s не принимает аргументов, передано %q"), cmd.CommandPath(), args[0])
			}
			if len(args) < minArgs || (maxArgs >= 0 && len(args) > maxArgs) {
				return fmt.Errorf(i18n.T("%s: want %s, got %d argument(s)", "%s: нужно %s, передано аргументов: %d"),
					cmd.CommandPath(), strings.Join(use[1:], " "), len(args))
			}
			return nil
		},
	}

	if maxArgs < 0 {
		// Everything after the fixed arguments belongs to the tool
		// (kubectl get pods -n x): stop parsing flags at the first argument.
		// evo's own flags go before it.
		cmd.Flags().SetInterspersed(false)
		long += i18n.T("\nevo flags (--json, --yes, --dry-run, --profile) go before the arguments: everything after the first one is passed to the tool as is.",
			"\nФлаги evo (--json, --yes, --dry-run, --profile) ставятся перед аргументами: всё после первого аргумента передаётся инструменту как есть.")
		cmd.Long = long
	}

	flagArgs := c.flagArgs(t)
	for _, p := range flagArgs {
		name := c.flagName(p.Name)
		switch p.Type {
		case "integer":
			cmd.Flags().Int(name, 0, p.Description)
		case "number":
			cmd.Flags().Float64(name, 0, p.Description)
		case "boolean":
			cmd.Flags().Bool(name, false, p.Description)
		case "array":
			cmd.Flags().StringArray(name, nil, p.Description+" (repeatable, or comma-separated)")
		default:
			cmd.Flags().String(name, "", p.Description)
		}
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		callArgs, err := treeArgs(cmd, c, t, pos, flagArgs, args)
		if err != nil {
			return err
		}
		text, err := invokeTool(cmd.Context(), cmd, t, callArgs)
		if flagJSON {
			if text != "" {
				fmt.Fprintln(cmd.OutOrStdout(), text)
			}
			return err
		}
		if err != nil || text == "" {
			return err
		}
		return renderAnswer(cmdPrinter(cmd), cmd.OutOrStdout(), text, c.Columns)
	}
	return cmd
}

// evoFlagsKubectlLacks are evo's global flags that kubectl does not have.
// After the first argument they would reach kubectl ("unknown flag"), so they
// are refused with the right order instead. --dry-run and --profile are
// kubectl flags too and pass through.
var evoFlagsKubectlLacks = map[string]bool{"--json": true, "--yes": true, "-y": true}

// evoFlagInRest refuses an evo flag placed in the pass-through tail, up to a
// "--" (what follows it is a command inside the container).
func evoFlagInRest(cmd *cobra.Command, rest []string, firstArg string) error {
	for _, a := range rest {
		if a == "--" {
			return nil
		}
		if evoFlagsKubectlLacks[a] || strings.HasPrefix(a, "--json=") || strings.HasPrefix(a, "--yes=") {
			return clerrors.New(clerrors.CodeUsage,
				i18n.Tf("%s: %s after the arguments would go to the tool", "%s: %s после аргументов достался бы инструменту", cmd.CommandPath(), a),
				i18n.Tf("Put evo flags first: '%s %s <%s> …'.", "Флаги evo ставьте первыми: '%s %s <%s> …'.",
					cmd.CommandPath(), strings.SplitN(a, "=", 2)[0], firstArg))
		}
	}
	return nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// treeArgs turns positionals and changed flags into the tool's arguments.
func treeArgs(cmd *cobra.Command, c treeCmd, t tools.Tool, pos []positional, flagArgs []param, args []string) (map[string]any, error) {
	out := map[string]any{}
	i := 0
	for _, p := range pos {
		if i >= len(args) {
			break
		}
		if args[i] == "" && !p.optional {
			label := strings.ReplaceAll(strings.Join(p.names, "|"), "_", "-")
			return nil, clerrors.New(clerrors.CodeUsage,
				i18n.Tf("%s: <%s> is empty", "%s: <%s> пустой", cmd.CommandPath(), label),
				helpHint(cmd))
		}
		switch {
		case p.rest:
			if err := evoFlagInRest(cmd, args[i:], strings.ReplaceAll(strings.Join(pos[0].names, "|"), "_", "-")); err != nil {
				return nil, err
			}
			out[p.names[0]] = strings.Join(args[i:], " ")
			i = len(args)
		case len(p.names) == 2:
			if uuidPattern.MatchString(args[i]) {
				out[p.names[0]] = args[i]
			} else {
				out[p.names[1]] = args[i]
			}
			i++
		default:
			out[p.names[0]] = args[i]
			i++
		}
	}
	for _, p := range flagArgs {
		name := c.flagName(p.Name)
		if !cmd.Flags().Changed(name) {
			continue
		}
		var raw string
		if p.Type == "array" {
			vals, _ := cmd.Flags().GetStringArray(name)
			raw = strings.Join(vals, ",")
		} else {
			raw = cmd.Flags().Lookup(name).Value.String()
		}
		v, err := coerceArg(p, raw)
		if err != nil {
			return nil, clerrors.New(clerrors.CodeUsage, fmt.Sprintf("--%s: %v", name, err), "")
		}
		out[p.Name] = v
	}
	var missing []string
	for _, p := range schemaParams(t) {
		if _, ok := out[p.Name]; p.Required && !ok {
			missing = append(missing, "--"+c.flagName(p.Name))
		}
	}
	if len(missing) > 0 {
		return nil, clerrors.New(clerrors.CodeUsage,
			i18n.Tf("%s: missing required flag(s): %s", "%s: не заданы обязательные флаги: %s", cmd.CommandPath(), strings.Join(missing, ", ")),
			helpHint(cmd))
	}
	return out, nil
}

func helpHint(cmd *cobra.Command) string {
	return i18n.Tf("Run '%s --help'.", "Справка: '%s --help'.", cmd.CommandPath())
}

// --- answer formatting ---

// renderAnswer shows a tool's JSON answer: a list as a table, an object as
// FIELD/VALUE rows. Anything else is printed as is.
func renderAnswer(p *output.Printer, w io.Writer, text string, columns []string) error {
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		fmt.Fprintln(w, text)
		return nil
	}
	if rows, notes, ok := findList(v); ok {
		renderList(p, w, rows, columns)
		for _, k := range sortedKeys(notes) {
			fmt.Fprintf(w, "%s: %v\n", k, notes[k])
		}
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		fmt.Fprintln(w, text)
		return nil
	}
	// A resource (it has an id or a name) is one FIELD/VALUE table. A
	// composite answer without one (k8s_options: versions, flavors, zones)
	// shows each list of objects as its own table under the fields.
	_, hasID := m["id"]
	_, hasName := m["name"]
	sections := map[string][]any{}
	if !hasID && !hasName {
		for k, x := range m {
			if a, ok := x.([]any); ok && len(a) > 0 {
				if _, obj := a[0].(map[string]any); obj {
					sections[k] = a
				}
			}
		}
	}
	keys := sortedKeys(m)
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		if _, sec := sections[k]; !sec {
			rows = append(rows, []string{k, cell(m[k], 0)})
		}
	}
	if len(rows) > 0 {
		p.Table([]string{i18n.T("FIELD", "ПОЛЕ"), i18n.T("VALUE", "ЗНАЧЕНИЕ")}, rows)
	}
	printed := len(rows) > 0
	for _, k := range keys {
		if a, sec := sections[k]; sec {
			if printed {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s:\n", strings.ToUpper(k))
			renderList(p, w, a, nil)
			printed = true
		}
	}
	return nil
}

// listNoteFields are the text fields a list answer may carry next to its
// list (list_flavors: a note on prices); they are printed under the table.
var listNoteFields = map[string]bool{"note": true, "warning": true, "hint": true}

// findList returns the rows of a list answer: a top-level array, or the
// only array field of an object whose other fields are numbers (count,
// total) or notes. An object with other fields (vm_get: name, private_ip
// and an interfaces array) is a single resource, not a list.
func findList(v any) ([]any, map[string]any, bool) {
	if a, ok := v.([]any); ok {
		return a, nil, true
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil, false
	}
	var list []any
	notes := map[string]any{}
	found := 0
	for k, x := range m {
		switch x := x.(type) {
		case []any:
			list = x
			found++
		case float64:
		case string:
			if !listNoteFields[k] {
				return nil, nil, false
			}
			notes[k] = x
		default:
			return nil, nil, false
		}
	}
	return list, notes, found == 1
}

// preferredColumns come first when the columns are picked from the rows.
var preferredColumns = []string{"id", "name", "path", "key", "status", "state", "phase", "version", "size", "zone"}

const maxAutoColumns = 7

func renderList(p *output.Printer, w io.Writer, rows []any, columns []string) {
	if len(rows) == 0 {
		fmt.Fprintln(w, i18n.T("No items.", "Пусто."))
		return
	}
	if _, isObj := rows[0].(map[string]any); !isObj {
		out := make([][]string, len(rows))
		for i, r := range rows {
			out[i] = []string{cell(r, 0)}
		}
		p.Table([]string{i18n.T("VALUE", "ЗНАЧЕНИЕ")}, out)
		return
	}
	if len(columns) == 0 {
		columns = autoColumns(rows)
	}
	headers := make([]string, len(columns))
	for i, c := range columns {
		headers[i] = strings.ToUpper(c)
	}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		m, _ := r.(map[string]any)
		line := make([]string, len(columns))
		for i, c := range columns {
			line[i] = cell(m[c], 40)
		}
		out = append(out, line)
	}
	p.Table(headers, out)
}

// autoColumns picks up to maxAutoColumns keys with scalar values from the
// first row: the preferred ones first, then the rest by name.
func autoColumns(rows []any) []string {
	first, _ := rows[0].(map[string]any)
	scalar := map[string]bool{}
	for k, v := range first {
		switch v.(type) {
		case map[string]any, []any:
			if named, ok := v.(map[string]any); ok && named["name"] != nil {
				scalar[k] = true // shown by its name
			}
		default:
			scalar[k] = true
		}
	}
	var cols []string
	for _, k := range preferredColumns {
		if scalar[k] {
			cols = append(cols, k)
			delete(scalar, k)
		}
	}
	rest := make([]string, 0, len(scalar))
	for k := range scalar {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	cols = append(cols, rest...)
	if len(cols) > maxAutoColumns {
		cols = cols[:maxAutoColumns]
	}
	return cols
}

// cell renders a value for a table; max > 0 cuts it to that many runes.
func cell(v any, max int) string {
	var s string
	switch x := v.(type) {
	case nil:
		s = ""
	case string:
		s = x
	case bool, float64:
		b, _ := json.Marshal(x)
		s = string(b)
	case map[string]any:
		if n, ok := x["name"].(string); ok && max > 0 {
			s = n
		} else {
			b, _ := json.Marshal(x)
			s = string(b)
		}
	case []any:
		parts := make([]string, 0, len(x))
		simple := true
		for _, e := range x {
			switch e.(type) {
			case map[string]any, []any:
				simple = false
			}
			parts = append(parts, cell(e, 0))
		}
		if simple {
			s = strings.Join(parts, ",")
		} else {
			b, _ := json.Marshal(x)
			s = string(b)
		}
	default:
		b, _ := json.Marshal(x)
		s = string(b)
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); max > 0 && len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return s
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
