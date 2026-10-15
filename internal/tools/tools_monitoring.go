package tools

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/shatim-ops/evo-cli/internal/monitoring"
)

// monitoringClient builds an authenticated Service Monitoring client.
func (s *Registry) monitoringClient(ctx context.Context) (*monitoring.Client, error) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return monitoring.New(tok.Value, eng.Config.ProjectID, ""), nil
}

// parseSince turns a "since" arg (e.g. "1h", "30m", "2d") into a duration.
// Defaults to 1h when empty/invalid.
func parseSince(arg string) time.Duration {
	if arg == "" {
		return time.Hour
	}
	if d, err := time.ParseDuration(arg); err == nil && d > 0 {
		return d
	}
	// tolerate a plain number of minutes
	if n, err := strconv.Atoi(arg); err == nil && n > 0 {
		return time.Duration(n) * time.Minute
	}
	return time.Hour
}

func (s *Registry) handleMetricsHistory(ctx context.Context, args map[string]any) (string, bool) {
	match := stringArg(args, "match")
	if match == "" {
		return usageResult(fmt.Errorf("'match' is required (a metric name / PromQL selector, e.g. cpu_usage)")), true
	}
	cli, err := s.monitoringClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	since := parseSince(stringArg(args, "since"))
	if since > 7*24*time.Hour {
		since = 7 * 24 * time.Hour // API cap
	}
	end := time.Now()
	start := end.Add(-since)
	series, err := cli.ExportMetrics(ctx, match, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"project": cli.ProjectID(),
		"match":   match,
		"window":  since.String(),
		"count":   len(series),
		"series":  series,
	}), false
}

func (s *Registry) handleMetricsScrape(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.monitoringClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	match := stringArg(args, "match")
	if match == "" {
		match = "{__name__=~\".+\"}"
	}
	text, err := cli.ScrapeMetrics(ctx, match)
	if err != nil {
		return errResult(err), true
	}
	// Prometheus text exposition — return as-is (may be large; caller greps).
	return toJSON(map[string]any{
		"project":    cli.ProjectID(),
		"match":      match,
		"format":     "prometheus-text",
		"exposition": text,
	}), false
}
