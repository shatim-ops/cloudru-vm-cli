package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// newRegistry builds the tool registry with the same engine factory as the
// MCP server: credentials, profile, project and region resolve exactly as
// for every other command.
func newRegistry() *tools.Registry {
	return tools.New(func(overrides map[string]any) (*engine.Engine, error) {
		if overrides == nil {
			overrides = map[string]any{}
		}
		return buildEngine(overrides)
	})
}

// cmdPrinter is output.New writing to the command's streams (tests replace
// them with buffers).
func cmdPrinter(cmd *cobra.Command) *output.Printer {
	if cmd.OutOrStdout() == os.Stdout && cmd.ErrOrStderr() == os.Stderr {
		return output.New(flagJSON, flagNoColor)
	}
	mode := output.ModePlain
	if flagJSON {
		mode = output.ModeJSON
	}
	return output.NewWithWriters(cmd.OutOrStdout(), cmd.ErrOrStderr(), mode, false)
}

func newToolsCmd() *cobra.Command {
	cmd := asGroup(&cobra.Command{
		Use:   "tools",
		Short: "List, describe and call any of the evo tools (the same ones the MCP server exposes)",
		Long: `Every operation the MCP server exposes is available here as a tool.

  evo tools list [--group vm]
  evo tools describe vm_get
  evo tools call vm_get --arg name=web

Tools that create paid resources or delete data ask for confirmation the same
way as the other commands: a prompt on a terminal, --yes to confirm in
advance, --dry-run to see the preview only; without a terminal and without
--yes the call stops with exit code 3.`,
	})
	cmd.AddCommand(newToolsListCmd(), newToolsDescribeCmd(), newToolsCallCmd())
	return cmd
}

// --- tools list ---

// toolInfo is the JSON form of a tool in 'tools list' and 'tools describe'.
type toolInfo struct {
	Name        string         `json:"name"`
	Group       string         `json:"group"`
	Mutating    bool           `json:"mutating"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

func newToolsListCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the tools: name, group, whether it changes anything, description",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := cmdPrinter(cmd)
			if group != "" && !knownGroup(group) {
				return clerrors.New(clerrors.CodeUsage, fmt.Sprintf("unknown group %q", group),
					"Groups: "+strings.Join(tools.Groups(), ", "))
			}
			var list []toolInfo
			for _, t := range newRegistry().Tools() {
				if group != "" && t.Group != group {
					continue
				}
				list = append(list, toolInfo{Name: t.Name, Group: t.Group, Mutating: t.Mutating, Description: t.Description})
			}
			if p.IsJSON() {
				return p.JSON(list)
			}
			rows := make([][]string, len(list))
			for i, t := range list {
				mut := "no"
				if t.Mutating {
					mut = "yes"
				}
				rows[i] = []string{t.Name, t.Group, mut, summary(t.Description, 80)}
			}
			p.Table([]string{"NAME", "GROUP", "MUTATING", "DESCRIPTION"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "Show only this group ("+strings.Join(tools.Groups(), ", ")+")")
	_ = cmd.RegisterFlagCompletionFunc("group", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return tools.Groups(), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func knownGroup(g string) bool {
	for _, k := range tools.Groups() {
		if k == g {
			return true
		}
	}
	return false
}

// summary returns the first sentence of s, cut to max runes.
func summary(s string, max int) string {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '.' && s[i+1] == ' ' && !strings.HasSuffix(s[:i], "e.g") && !strings.HasSuffix(s[:i], "i.e") {
			s = s[:i+1]
			break
		}
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

// --- tools describe ---

func newToolsDescribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "describe <name>",
		Short:             "Show a tool's description and arguments",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeToolNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := lookupTool(newRegistry(), args[0])
			if err != nil {
				return err
			}
			p := cmdPrinter(cmd)
			if p.IsJSON() {
				return p.JSON(toolInfo{Name: t.Name, Group: t.Group, Mutating: t.Mutating,
					Description: t.Description, InputSchema: t.InputSchema})
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%s (group %s", t.Name, t.Group)
			if t.Mutating {
				fmt.Fprint(w, ", mutating: asks for confirmation")
			}
			fmt.Fprintf(w, ")\n\n%s\n\n", t.Description)
			params := schemaParams(t)
			if len(params) == 0 {
				fmt.Fprintln(w, "No arguments.")
				return nil
			}
			fmt.Fprintln(w, "Arguments (--arg name=value):")
			rows := make([][]string, len(params))
			for i, a := range params {
				req := ""
				if a.Required {
					req = "required"
				}
				rows[i] = []string{a.Name, a.Type, req, a.Description}
			}
			p.Table([]string{"NAME", "TYPE", "REQUIRED", "DESCRIPTION"}, rows)
			return nil
		},
	}
}

// param is one argument of a tool, from its JSON schema.
type param struct {
	Name        string
	Type        string // string, integer, number, boolean, array, object
	ItemType    string // for arrays
	Required    bool
	Description string
}

// schemaParams lists the tool's arguments, required first, then by name.
// confirmed is left out: consent is given with --yes or at the prompt.
func schemaParams(t tools.Tool) []param {
	props, _ := t.InputSchema["properties"].(map[string]any)
	required := map[string]bool{}
	switch r := t.InputSchema["required"].(type) {
	case []string:
		for _, n := range r {
			required[n] = true
		}
	case []any:
		for _, n := range r {
			if s, ok := n.(string); ok {
				required[s] = true
			}
		}
	}
	var out []param
	for name, v := range props {
		if name == "confirmed" {
			continue
		}
		m, _ := v.(map[string]any)
		a := param{Name: name, Required: required[name]}
		a.Type, _ = m["type"].(string)
		a.Description, _ = m["description"].(string)
		if items, ok := m["items"].(map[string]any); ok {
			a.ItemType, _ = items["type"].(string)
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func lookupTool(reg *tools.Registry, name string) (tools.Tool, error) {
	t, ok := reg.Lookup(name)
	if !ok {
		return tools.Tool{}, clerrors.New(clerrors.CodeUsage, fmt.Sprintf("unknown tool %q", name),
			"Run 'evo tools list' to see the tools.")
	}
	return t, nil
}

func completeToolNames(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, t := range newRegistry().Tools() {
		names = append(names, t.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// --- tools call ---

func newToolsCallCmd() *cobra.Command {
	var (
		argPairs []string
		argsJSON string
		argsFile string
		wait     bool
	)
	cmd := &cobra.Command{
		Use:   "call <name>",
		Short: "Call a tool and print its JSON result",
		Long: `Call a tool and print its JSON result as is.

Arguments come from --args-file, then --args-json, then --arg (later ones
win). --arg values are converted by the tool's schema: integer, number and
boolean are parsed; an array is a comma-separated list (or a JSON array);
an object is JSON. Names and types from the JSON sources are checked
against the schema too; null means "not given".

  evo tools call list_zones
  evo tools call vm_get --arg name=web
  evo tools call open_ports --arg security_group=web-sg --arg ports=80,443 --yes
  evo tools call pg_operation --arg operation_id=OP --wait

Exit codes: 0 success; 3 confirmation not given; 4 the tool reported that
something was not found; 5 any other error reported by the tool.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeToolNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := lookupTool(newRegistry(), args[0])
			if err != nil {
				return err
			}
			callArgs, err := buildToolArgs(t, argsFile, argsJSON, argPairs)
			if err != nil {
				return err
			}
			if wait {
				if err := applyWait(t, callArgs); err != nil {
					return err
				}
			}
			text, err := invokeTool(cmd.Context(), cmd, t, callArgs)
			if text != "" {
				fmt.Fprintln(cmd.OutOrStdout(), text)
			}
			return err
		},
	}
	cmd.Flags().StringArrayVar(&argPairs, "arg", nil, "Tool argument as name=value (repeatable)")
	cmd.Flags().StringVar(&argsJSON, "args-json", "", "Tool arguments as a JSON object")
	cmd.Flags().StringVar(&argsFile, "args-file", "", "Read tool arguments from a JSON file")
	cmd.Flags().BoolVar(&wait, "wait", false, "For tools that take wait_seconds (*_operation, stack_status): wait for the final state, polling with backoff up to the tool's limit")
	return cmd
}

// buildToolArgs merges the argument sources and checks them against the
// tool's schema.
func buildToolArgs(t tools.Tool, file, rawJSON string, pairs []string) (map[string]any, error) {
	args := map[string]any{}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, clerrors.Wrap(clerrors.CodeUsage, "read --args-file: "+err.Error(), "", err)
		}
		if err := mergeJSONArgs(args, data, "--args-file"); err != nil {
			return nil, err
		}
	}
	if rawJSON != "" {
		if err := mergeJSONArgs(args, []byte(rawJSON), "--args-json"); err != nil {
			return nil, err
		}
	}
	params := map[string]param{}
	for _, p := range schemaParams(t) {
		params[p.Name] = p
	}
	// JSON arguments get the same checks as --arg: a misspelt name or a
	// string where the schema wants a number would otherwise reach the
	// handler, which silently takes its default.
	for name, v := range args {
		if name == "confirmed" {
			continue // refused below with a hint
		}
		if v == nil {
			delete(args, name) // null: as if not given
			continue
		}
		p, known := params[name]
		if !known {
			return nil, unknownArgError(t, name)
		}
		if err := checkArgType(p, v); err != nil {
			return nil, clerrors.New(clerrors.CodeUsage, fmt.Sprintf("argument %s: %v", name, err),
				fmt.Sprintf("Run 'evo tools describe %s'.", t.Name))
		}
	}
	for _, pair := range pairs {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || name == "" {
			return nil, clerrors.New(clerrors.CodeUsage, fmt.Sprintf("--arg %q: want name=value", pair), "")
		}
		p, known := params[name]
		if !known {
			return nil, unknownArgError(t, name)
		}
		v, err := coerceArg(p, value)
		if err != nil {
			return nil, clerrors.New(clerrors.CodeUsage, fmt.Sprintf("--arg %s: %v", name, err), "")
		}
		args[name] = v
	}
	if _, ok := args["confirmed"]; ok {
		return nil, clerrors.New(clerrors.CodeUsage, "confirmed is not an argument here",
			"Confirm with --yes or at the prompt; --dry-run shows the preview only.")
	}
	var missing []string
	for _, p := range schemaParams(t) {
		// An empty string is no value: the handler would answer
		// "required" as a tool error (exit 5) instead of a usage error.
		if v, ok := args[p.Name]; p.Required && (!ok || v == "") {
			missing = append(missing, p.Name)
		}
	}
	if len(missing) > 0 {
		return nil, clerrors.New(clerrors.CodeUsage,
			fmt.Sprintf("%s: missing required argument(s): %s", t.Name, strings.Join(missing, ", ")),
			fmt.Sprintf("Run 'evo tools describe %s'.", t.Name))
	}
	return args, nil
}

func mergeJSONArgs(dst map[string]any, data []byte, from string) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return clerrors.New(clerrors.CodeUsage, fmt.Sprintf("%s: not a JSON object: %v", from, err), "")
	}
	for k, v := range m {
		dst[k] = v
	}
	return nil
}

func unknownArgError(t tools.Tool, name string) error {
	var names []string
	for _, p := range schemaParams(t) {
		names = append(names, p.Name)
	}
	takes := "no arguments"
	if len(names) > 0 {
		takes = strings.Join(names, ", ")
	}
	return clerrors.New(clerrors.CodeUsage, fmt.Sprintf("%s has no argument %q", t.Name, name),
		fmt.Sprintf("%s takes: %s. See 'evo tools describe %s'.", t.Name, takes, t.Name))
}

// coerceArg converts a --arg value to the type the schema declares, in the
// form a JSON decoder would produce (numbers as float64, arrays as []any).
func coerceArg(p param, value string) (any, error) {
	switch p.Type {
	case "integer":
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", value)
		}
		return float64(n), nil
	case "number":
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		// NaN and ±Inf parse, but JSON cannot carry them.
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("%q is not a number", value)
		}
		return f, nil
	case "boolean":
		b, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("%q is not true or false", value)
		}
		return b, nil
	case "array":
		if strings.HasPrefix(strings.TrimSpace(value), "[") {
			var a []any
			if err := json.Unmarshal([]byte(value), &a); err != nil {
				return nil, fmt.Errorf("not a JSON array: %v", err)
			}
			if err := checkArgType(p, a); err != nil {
				return nil, err
			}
			return a, nil
		}
		if p.ItemType == "object" || p.ItemType == "array" {
			// JSON items contain commas themselves: one value, one item.
			v, err := coerceArg(param{Type: p.ItemType}, value)
			if err != nil {
				return nil, err
			}
			return []any{v}, nil
		}
		out := []any{}
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			v, err := coerceArg(param{Type: p.ItemType}, part)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case "object":
		var m map[string]any
		if err := json.Unmarshal([]byte(value), &m); err != nil {
			return nil, fmt.Errorf("not a JSON object: %v", err)
		}
		return m, nil
	}
	return value, nil
}

// checkArgType checks a decoded JSON value against the schema type of p.
// An untyped parameter takes anything.
func checkArgType(p param, v any) error {
	ok := true
	switch p.Type {
	case "integer":
		f, isNum := v.(float64)
		ok = isNum && f == math.Trunc(f) && !math.IsInf(f, 0)
	case "number":
		f, isNum := v.(float64)
		ok = isNum && !math.IsNaN(f) && !math.IsInf(f, 0)
	case "boolean":
		_, ok = v.(bool)
	case "string":
		_, ok = v.(string)
	case "object":
		_, ok = v.(map[string]any)
	case "array":
		a, isArr := v.([]any)
		if !isArr {
			ok = false
			break
		}
		for i, item := range a {
			if err := checkArgType(param{Type: p.ItemType}, item); err != nil {
				return fmt.Errorf("item %d: %v", i, err)
			}
		}
	}
	if !ok {
		b, _ := json.Marshal(v)
		return fmt.Errorf("want %s, got %s", article(p.Type), summary(string(b), 40))
	}
	return nil
}

func article(typ string) string {
	switch typ {
	case "integer", "array", "object":
		return "an " + typ
	}
	return "a " + typ
}

// applyWait sets wait_seconds to the longest wait the tool allows, unless
// the caller gave one. The tool polls with capped backoff itself.
func applyWait(t tools.Tool, args map[string]any) error {
	props, _ := t.InputSchema["properties"].(map[string]any)
	if _, ok := props["wait_seconds"]; !ok {
		return clerrors.New(clerrors.CodeUsage, fmt.Sprintf("%s does not support --wait", t.Name),
			"--wait works with tools that take wait_seconds: the *_operation tools and stack_status.")
	}
	if _, ok := args["wait_seconds"]; !ok {
		args["wait_seconds"] = float64(tools.OpWaitMaxSeconds)
	}
	return nil
}

// invokeTool calls the tool. When the tool answers confirmation_required
// (or is mutating but has no gate of its own), consent goes through
// confirm.Ask and, once given, the tool is called again with
// confirmed=true. It returns the tool's final JSON text ("" when the call
// stopped at a --dry-run preview) and an error carrying the exit code.
func invokeTool(ctx context.Context, cmd *cobra.Command, t tools.Tool, args map[string]any) (string, error) {
	opts := confirm.Options{Yes: flagYes, DryRun: flagDryRun, JSON: flagJSON,
		Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}

	if t.Mutating && !takesConfirmed(t) {
		ok, err := confirm.Ask(ctx, confirm.Preview{Action: t.Name, Summary: "call " + t.Name,
			Lines: argLines(args), Details: args}, opts)
		if !ok {
			return "", err
		}
	}

	text, isErr := t.Handler(ctx, args)
	if resp := parseObject(text); resp["status"] == "confirmation_required" {
		ok, err := confirm.Ask(ctx, toolPreview(t.Name, resp), opts)
		if !ok {
			return "", err
		}
		confirmed := make(map[string]any, len(args)+1)
		for k, v := range args {
			confirmed[k] = v
		}
		confirmed["confirmed"] = true
		text, isErr = t.Handler(ctx, confirmed)
	}
	if isErr {
		return text, toolError(t.Name, text)
	}
	return text, nil
}

func takesConfirmed(t tools.Tool) bool {
	props, _ := t.InputSchema["properties"].(map[string]any)
	_, ok := props["confirmed"]
	return ok
}

func parseObject(text string) map[string]any {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return nil
	}
	return m
}

// toolPreview turns a confirmation_required answer into a confirm.Preview:
// the message first, then every other field.
func toolPreview(name string, resp map[string]any) confirm.Preview {
	p := confirm.Preview{Action: name, Summary: "call " + name, Details: resp}
	if msg, ok := resp["message"].(string); ok && msg != "" {
		p.Lines = append(p.Lines, msg)
	}
	rest := map[string]any{}
	for k, v := range resp {
		if k != "status" && k != "message" {
			rest[k] = v
		}
	}
	p.Lines = append(p.Lines, argLines(rest)...)
	return p
}

// argLines renders a map as "key: value" lines in key order.
func argLines(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		var v string
		if s, ok := m[k].(string); ok {
			v = s
		} else {
			b, _ := json.Marshal(m[k])
			v = string(b)
		}
		lines = append(lines, k+": "+v)
	}
	return lines
}

// toolError maps a tool's error answer to an exit code. An error that
// carries a CLI error code ("AUTH_REQUIRED: …") keeps its exit code when it
// is 2, 4 or 5, as for every other command; otherwise 4 when the answer says
// something was not found, and 5 for the rest.
func toolError(name, text string) error {
	msg := text
	if m := parseObject(text); m != nil {
		for _, k := range []string{"error", "message"} {
			if s, ok := m[k].(string); ok && s != "" {
				msg = s
				break
			}
		}
	}
	if prefix, rest, ok := strings.Cut(msg, ": "); ok && isErrorCode(prefix) {
		switch prefix {
		case clerrors.CodeUsage: // the tool refused its arguments; nothing reached the cloud
			return clerrors.New(prefix, name+": "+rest, "Check the arguments: 'evo tools describe "+name+"'.")
		case clerrors.CodeConfigInvalid: // the message names what to set
			return clerrors.New(prefix, name+": "+rest, "")
		}
		switch clerrors.ExitCode(clerrors.New(prefix, "", "")) {
		case clerrors.ExitAuth:
			return clerrors.New(prefix, name+": "+rest,
				"Run 'evo configure', or set CLOUDRU_KEY_ID and CLOUDRU_SECRET.")
		case clerrors.ExitNotFound, clerrors.ExitAPI:
			return clerrors.New(prefix, name+": "+rest, "")
		}
	}
	code := clerrors.CodeAPIError
	if strings.Contains(strings.ToLower(text), "not found") {
		code = clerrors.CodeNotFound
	}
	return clerrors.New(code, name+": "+msg, "")
}

// isErrorCode reports whether s looks like a CLI error code (AUTH_REQUIRED).
func isErrorCode(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && r != '_' {
			return false
		}
	}
	return true
}

// Root flags that pick the project and the zone for every command that
// talks to the cloud. A command's own flag of the same name (deploy,
// configure) takes precedence; so does an argument a tool passes.
var (
	flagProjectID string
	flagRegion    string
)

func addScopeFlags(root *cobra.Command) {
	root.PersistentFlags().StringVar(&flagProjectID, "project-id", "", "Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)")
	root.PersistentFlags().StringVar(&flagRegion, "region", "", "Availability zone UUID (default: from the profile or $CLOUDRU_REGION)")
}

// applyScopeFlags adds --project-id and --region to the engine overrides
// unless the caller already set them.
func applyScopeFlags(overrides map[string]any) {
	if _, ok := overrides["ProjectID"]; !ok && flagProjectID != "" {
		overrides["ProjectID"] = flagProjectID
	}
	if _, ok := overrides["Region"]; !ok && flagRegion != "" {
		overrides["Region"] = flagRegion
	}
}
