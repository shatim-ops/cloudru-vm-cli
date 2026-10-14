package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// opwait: a single, uniform "wait for an async operation" contract shared by every
// *_operation tool. The MCP model stays async by default (one call → current state),
// but a caller that wants the RESULT can pass wait_seconds and get it in ONE call —
// the engine polls with capped exponential backoff instead of the agent hammering
// raw get/list every 2-3s. Single resource → optional capped wait here; multi-step
// orchestration → stack_status owns the waiting. No blocking beyond the cap.

const opWaitMaxSeconds = 300

// opWaitFirstDelay is the first poll interval of waitForOpJSON. A variable only
// so that tests can shorten it; production code never changes it.
var opWaitFirstDelay = 2 * time.Second

// opTerminalStates are the status/state values we treat as "operation finished".
var opTerminalStates = map[string]bool{
	"DONE": true, "READY": true, "ACTIVE": true, "AVAILABLE": true,
	"COMPLETED": true, "COMPLETE": true, "SUCCEEDED": true, "SUCCESS": true,
	"ERROR": true, "FAILED": true, "FAILURE": true, "CANCELLED": true,
	"CANCELED": true, "DELETED": true, "RUNNING": true,
}

// opTerminal reports whether an operation JSON map looks finished: an explicit
// done=true, a terminal status/state string, or a non-null error field.
func opTerminal(m map[string]any) bool {
	if m == nil {
		return false
	}
	if b, ok := m["done"].(bool); ok && b {
		return true
	}
	for _, k := range []string{"status", "state", "phase"} {
		if v, ok := m[k].(string); ok && opTerminalStates[strings.ToUpper(strings.TrimSpace(v))] {
			return true
		}
	}
	if e, ok := m["error"]; ok && e != nil {
		if s, isStr := e.(string); !isStr || strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// opToMap converts a typed operation value to a generic JSON map so waitForOpJSON
// can inspect status fields uniformly across every service's Operation type.
func opToMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

// waitForOpJSON polls fetch() with capped exponential backoff (2s → 15s, capped at
// wait_seconds, hard max opWaitMaxSeconds) until the operation is terminal or the
// budget elapses. Returns the last observed state. Honors ctx cancellation.
func waitForOpJSON(ctx context.Context, waitSeconds int, fetch func(context.Context) (map[string]any, error)) (map[string]any, error) {
	if waitSeconds > opWaitMaxSeconds {
		waitSeconds = opWaitMaxSeconds
	}
	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	delay := opWaitFirstDelay
	var last map[string]any
	for {
		m, err := fetch(ctx)
		if err != nil {
			return last, err
		}
		last = m
		if opTerminal(m) || !time.Now().Before(deadline) {
			return m, nil
		}
		if rem := time.Until(deadline); rem < delay {
			delay = rem
		}
		if delay <= 0 {
			return m, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(delay):
		}
		if delay < 15*time.Second {
			delay += delay / 2 // 2 → 3 → 4.5 → 6.75 → 10 → 15 → 15…
		}
	}
}
