package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// checkedArgs refuses, before the handler, an argument the tool's schema
// does not declare, and an enum value outside the schema's list (matched
// case-insensitively and passed on in the schema's spelling). Unknown
// arguments were silently ignored: destroy {"vm": "orphan-1"} — the name
// disk_attach uses — fell through to "delete the current deployment"
// (release QA, D-mcp 4); dns_zone_create {"type": "Private"} made a public
// zone (D-mcp 7). confirmed is taken by every tool.
func checkedArgs(name string, schema map[string]any, next Handler) Handler {
	props, _ := schema["properties"].(map[string]any)
	return func(ctx context.Context, args map[string]any) (string, bool) {
		var unknown []string
		for k := range args {
			// confirmed on a read-only tool is harmless: agents send it
			// after an approval they got for another call.
			if _, ok := props[k]; !ok && k != "confirmed" {
				unknown = append(unknown, k)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			known := make([]string, 0, len(props))
			for k := range props {
				known = append(known, k)
			}
			sort.Strings(known)
			takes := "no arguments"
			if len(known) > 0 {
				takes = strings.Join(known, ", ")
			}
			return errResult(clierrors.New(clierrors.CodeUsage,
				fmt.Sprintf("%s: unknown argument(s) %s", name, strings.Join(unknown, ", ")),
				fmt.Sprintf("%s takes: %s.", name, takes))), true
		}
		for k, v := range args {
			p, _ := props[k].(map[string]any)
			enum, _ := p["enum"].([]any)
			s, isStr := v.(string)
			if len(enum) == 0 || !isStr || s == "" {
				continue
			}
			canon := ""
			var allowed []string
			for _, e := range enum {
				es, _ := e.(string)
				allowed = append(allowed, es)
				if strings.EqualFold(es, s) {
					canon = es
				}
			}
			if canon == "" {
				return errResult(clierrors.New(clierrors.CodeUsage,
					fmt.Sprintf("%s: %s %q is not one of %s", name, k, s, strings.Join(allowed, ", ")),
					fmt.Sprintf("Pass %s as one of: %s.", k, strings.Join(allowed, ", ")))), true
			}
			args[k] = canon
		}
		return next(ctx, args)
	}
}
