package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/stack"
)

// --- stack orchestrator: crank-driven, disk-persisted multi-resource deploys ---
//
// The heavy execution loop lives HERE (server-side), not in the agent's context.
// The agent calls stack_apply once, then polls stack_status; each poll is one
// "crank" that advances the persisted state machine as far as it can and returns
// a COMPACT status. Agent context stays O(1) regardless of stack size — no
// trajectory/context overflow, LLM-agnostic, resumable after context loss.

// callTool invokes an existing MCP tool handler in-process and parses its JSON.
// This reuses every typed operation (pg_create, provision, tg_create, …) with
// zero duplication — the orchestrator drives the same tools the agent would.
func (s *Registry) callTool(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	h, ok := s.handlers[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	text, isErr := h(ctx, args)
	var out map[string]any
	if e := json.Unmarshal([]byte(text), &out); e != nil {
		return nil, fmt.Errorf("%s: unparseable result: %s", name, text)
	}
	if isErr {
		msg, _ := out["error"].(string)
		if msg == "" {
			msg = text
		}
		return out, fmt.Errorf("%s: %s", name, msg)
	}
	return out, nil
}

// substituteRefs replaces "$stepkey" string arg values with the produced output.
func substituteRefs(args map[string]any, outputs map[string]string) map[string]any {
	res := make(map[string]any, len(args))
	for k, v := range args {
		if sv, ok := v.(string); ok && len(sv) > 1 && sv[0] == '$' {
			if id, ok := outputs[sv[1:]]; ok {
				res[k] = id
				continue
			}
		}
		res[k] = v
	}
	return res
}

func depsDone(step *stack.Step, job *stack.Job) bool {
	for _, d := range step.DependsOn {
		found := false
		for _, s := range job.Steps {
			if s.Key == d {
				found = s.Status == "done"
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// findIDByName resolves a resource id from a list tool by matching name. extra
// carries any params the list tool needs (e.g. dns_records needs zone_id).
func (s *Registry) findIDByName(ctx context.Context, tool, field, name string, extra map[string]any) string {
	args := map[string]any{}
	for k, v := range extra {
		args[k] = v
	}
	res, err := s.callTool(ctx, tool, args)
	if err != nil {
		return ""
	}
	arr, _ := res[field].([]any)
	for _, it := range arr {
		if m, ok := it.(map[string]any); ok {
			if n, _ := m["name"].(string); n == name {
				id, _ := m["id"].(string)
				return id
			}
		}
	}
	return ""
}

// maxStepAttempts bounds create retries per step (transient-error tolerance).
const maxStepAttempts = 4

// stackCrankInterval is the pause between cranks inside one stack_status
// call. A variable only so that tests can shorten it.
var stackCrankInterval = 8 * time.Second

// isTerminalFail reports whether a resource status means it will never become
// ready (a failed create) — so the orchestrator stops waiting and fails the job
// instead of polling forever. Covers pg/kafka/redis (FAILED/ERASED) and k8s.
func isTerminalFail(status string) bool {
	u := strings.ToUpper(status)
	return strings.Contains(u, "FAIL") || strings.Contains(u, "ERROR") || strings.Contains(u, "ERASED")
}

// finalizeStep transitions a step to its resolved state given a resource id that
// was either just created or ADOPTED during reconcile (create response failed but
// the resource exists). ReadyKind decides whether it's done or still needs a wait.
func (s *Registry) finalizeStep(ctx context.Context, job *stack.Job, st *stack.Step, id string) {
	st.ResourceID = id
	switch st.ReadyKind {
	case "status":
		st.Status = "waiting" // resource exists; poll its status on later cranks
	case "op":
		job.Outputs[st.Key] = id
		if st.Key == "lb" && id != "" {
			if lb, e := s.callTool(ctx, "lb_get", map[string]any{"id": id}); e == nil {
				if ext, ok := lb["external_address"].(map[string]any); ok {
					if ip, _ := ext["ipv4"].(string); ip != "" {
						job.Outputs["lb_external_ip"] = ip
					}
				}
			}
		}
		st.Status = "done"
	default:
		job.Outputs[st.Key] = id
		st.Status = "done"
	}
}

// advance is ONE crank: poll in-flight async steps and start ready pending steps,
// looping until no further progress is possible this turn. Persists nothing (the
// caller saves). Returns quickly — long async waits (PG READY) span cranks.
func (s *Registry) advance(ctx context.Context, job *stack.Job) {
	for progress := true; progress; {
		progress = false
		for _, st := range job.Steps {
			if ctx.Err() != nil {
				// The call was cancelled: every further tool call would fail
				// on the context, burn an attempt and be reported as a cloud
				// error. The job is saved as it is; the next crank goes on.
				return
			}
			switch st.Status {
			case "done", "error":
				continue

			case "waiting":
				if st.ReadyKind == "op" {
					if st.OpID == "" {
						st.Status = "error"
						job.Error = st.Key + ": missing operation id"
						job.Status = "error"
						return
					}
					op, err := s.callTool(ctx, st.PollTool, map[string]any{"operation_id": st.OpID})
					if err != nil {
						// Transient poll failure — don't kill the job; retry next crank.
						st.Detail = "poll: " + err.Error()
						continue
					}
					if done, _ := op["done"].(bool); done {
						// A completed operation can still carry an error (create
						// failed server-side). Treat that as terminal, don't proceed.
						if oe, ok := op["error"].(map[string]any); ok && oe != nil {
							msg, _ := oe["message"].(string)
							st.Status = "error"
							job.Error = fmt.Sprintf("%s: operation failed: %v", st.Key, msg)
							job.Status = "error"
							return
						}
						id := s.findIDByName(ctx, st.FindTool, st.FindField, st.FindName, st.FindArgs)
						st.ResourceID = id
						job.Outputs[st.Key] = id
						if st.Key == "lb" && id != "" {
							if lb, e := s.callTool(ctx, "lb_get", map[string]any{"id": id}); e == nil {
								if ext, ok := lb["external_address"].(map[string]any); ok {
									if ip, _ := ext["ipv4"].(string); ip != "" {
										job.Outputs["lb_external_ip"] = ip
									}
								}
							}
						}
						st.Status = "done"
						progress = true
					}
				} else if st.ReadyKind == "status" {
					if st.ResourceID == "" {
						st.ResourceID = s.findIDByName(ctx, st.FindTool, st.FindField, st.FindName, st.FindArgs)
					}
					if st.ResourceID != "" {
						cl, err := s.callTool(ctx, st.StatusTool, map[string]any{st.StatusArg: st.ResourceID})
						if err == nil {
							status, _ := cl[st.StatusField].(string)
							if status == "" {
								status, _ = cl["status"].(string)
							}
							switch {
							case status != "" && strings.Contains(status, st.ReadyValue):
								job.Outputs[st.Key] = st.ResourceID
								st.Status = "done"
								progress = true
							case isTerminalFail(status):
								// FAILED/ERROR/ERASED — never reaches READY; fail the
								// job instead of polling forever.
								st.Status = "error"
								job.Error = fmt.Sprintf("%s: cluster reached terminal status %s", st.Key, status)
								job.Status = "error"
								return
							default:
								st.Detail = "status=" + status
							}
						}
					}
				}

			case "pending":
				if !depsDone(st, job) {
					continue
				}
				if st.Interrupted {
					// The previous create was cut off and may have reached the
					// cloud: adopt what it made instead of creating it twice.
					id := st.ResourceID
					if id == "" && st.FindTool != "" {
						id = s.findIDByName(ctx, st.FindTool, st.FindField, st.FindName, st.FindArgs)
					}
					if ctx.Err() != nil {
						return
					}
					st.Interrupted = false
					if id != "" {
						s.finalizeStep(ctx, job, st, id)
						st.Detail = "adopted after an interrupted create"
						progress = true
						continue
					}
				}
				args := substituteRefs(st.Args, job.Outputs)
				// stack_apply was confirmed for the whole stack. Jobs planned
				// before provision and s3_put got their gates lack the flag.
				args["confirmed"] = true
				res, err := s.callTool(ctx, st.Tool, args)
				if err != nil && ctx.Err() != nil {
					// Cancelled mid-create: not a failed attempt. Keep the id
					// the tool reported (provision names the VM it made).
					st.Interrupted = true
					st.ResourceID = firstStr(res, "vm_id", "cluster_id", "id")
					st.Detail = "interrupted: " + err.Error()
					return
				}
				if err != nil {
					st.Attempts++
					st.Detail = err.Error()
					// Reconcile before retrying: a create can fail the RESPONSE yet
					// succeed server-side (e.g. 504 gateway timeout after the backend
					// applied it). If the resource now exists by name, ADOPT it — never
					// blindly retry a create (that would double-provision).
					if st.FindTool != "" {
						if id := s.findIDByName(ctx, st.FindTool, st.FindField, st.FindName, st.FindArgs); id != "" {
							s.finalizeStep(ctx, job, st, id)
							progress = true
							continue
						}
					}
					// Genuinely not created → retry on a later crank, bounded.
					if st.Attempts >= maxStepAttempts {
						st.Status = "error"
						job.Error = fmt.Sprintf("%s: %s (after %d attempts)", st.Key, err.Error(), st.Attempts)
						job.Status = "error"
						return
					}
					continue // stay pending, no progress → retried next stack_status crank
				}
				if st.ReadyKind == "" {
					// synchronous — capture id and finish
					id := firstStr(res, "vm_id", "id", "path")
					if id != "" {
						job.Outputs[st.Key] = id
						st.ResourceID = id
					} else {
						job.Outputs[st.Key] = "ok"
					}
					st.Status = "done"
				} else {
					// async — remember the op and wait on subsequent cranks
					st.OpID = firstStr(res, "operation_id")
					if st.ReadyKind == "status" {
						// some creates return the id directly (k8s: cluster_id)
						st.ResourceID = firstStr(res, "cluster_id", "id")
					}
					st.Status = "waiting"
				}
				progress = true
			}
		}
	}

	// terminal check
	if job.Status == "error" {
		return
	}
	allDone := true
	for _, st := range job.Steps {
		if st.Status != "done" {
			allDone = false
			break
		}
	}
	if allDone {
		job.Status = "done"
	} else {
		job.Status = "running"
	}
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// --- handlers ---

func (s *Registry) handleStackPlan(ctx context.Context, args map[string]any) (string, bool) {
	raw, ok := args["manifest"]
	if !ok {
		return errResult(fmt.Errorf("'manifest' is required (object describing the stack)")), true
	}
	b, _ := json.Marshal(raw)
	var m stack.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return errResult(fmt.Errorf("invalid manifest: %w", err)), true
	}
	if m.Name == "" {
		return errResult(fmt.Errorf("manifest.name is required")), true
	}
	// Validate + normalize the manifest BEFORE planning, so a malformed field
	// fails fast at plan time (nothing created) instead of wedging the stack in
	// "error" mid-deploy after other resources are already live.
	s3Notes, err := s.normalizeManifest(ctx, &m)
	if err != nil {
		return errResult(err), true
	}
	job := stack.Plan(m)
	if err := job.CheckDeps(); err != nil {
		return errResult(fmt.Errorf("invalid manifest: %w", err)), true
	}
	if err := job.Save(); err != nil {
		return errResult(fmt.Errorf("persist plan: %w", err)), true
	}
	steps := make([]string, 0, len(job.Steps))
	for _, st := range job.Steps {
		steps = append(steps, st.Key)
	}
	out := map[string]any{
		"status":  "planned",
		"job_id":  job.ID,
		"steps":   steps,
		"cost":    stack.EstimateCost(m),
		"message": "Stack planned. BILLABLE — show the captain the cost and get «го», then call stack_apply with this job_id and confirmed=true.",
	}
	if len(s3Notes) > 0 {
		out["s3"] = s3Notes
	}
	// Warn if a prior job for this stack name still holds resources — applying now
	// would create duplicates (double billing). Point the captain at stack_destroy.
	if orphans := stack.FindResourcefulJobs(m.Name, job.ID); len(orphans) > 0 {
		out["warning"] = fmt.Sprintf("%d earlier job(s) for stack %q still hold resources — applying THIS plan will create DUPLICATES. Run stack_destroy on them first.", len(orphans), m.Name)
		out["existing_jobs"] = orphans
	}
	return toJSON(out), false
}

// normalizeManifest validates a stack manifest and fills in safe defaults BEFORE
// planning, so a malformed field fails fast at plan time (nothing is created)
// instead of wedging the whole stack in "error" mid-deploy — the failure mode
// that an empty s3 bucket produced (s3_put needs bucket+key → 4 retries → the
// job goes "error" though snat/secret/vm already deployed).
//
// It returns per-object s3 notes (the resolved bucket/key) for transparency, and
// only calls the S3 API when a bucket actually needs defaulting.
func (s *Registry) normalizeManifest(ctx context.Context, m *stack.Manifest) ([]string, error) {
	// --- VM flavor: validate at plan time so a bad name fails fast here rather
	// than mid-apply. A non-existent flavor (e.g. "performance-2-4") otherwise
	// passes planning, then the VM step 500s during apply — and by then earlier
	// steps (a Postgres cluster) are already live, leaving a billable orphan that
	// a re-plan duplicates. Catching it here prevents that whole chain.
	if m.VM != nil && m.VM.Flavor != "" && m.VM.Flavor != "auto" {
		eng, err := s.authedEngine(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("validate vm.flavor: %w", err)
		}
		flavors, err := eng.Cloud.ListFlavors(ctx)
		if err != nil {
			return nil, fmt.Errorf("validate vm.flavor: %w", err)
		}
		found := false
		names := make([]string, 0, len(flavors))
		for _, f := range flavors {
			if f.Name == m.VM.Flavor || f.ID == m.VM.Flavor {
				found = true
				break
			}
			names = append(names, f.Name)
		}
		if !found {
			return nil, fmt.Errorf("vm.flavor %q does not exist in this project. Pick a real one (see list_flavors) — available: %s", m.VM.Flavor, strings.Join(names[:min(len(names), 14)], ", "))
		}
	}

	if len(m.S3) == 0 {
		return nil, nil
	}

	// Resolve the project bucket list only if at least one object omits it.
	var buckets []string
	fetched := false
	fetch := func() error {
		if fetched {
			return nil
		}
		cli, err := s.s3Client()
		if err != nil {
			return fmt.Errorf("resolve default S3 bucket: %w", err)
		}
		bs, err := cli.ListBuckets(ctx)
		if err != nil {
			return fmt.Errorf("resolve default S3 bucket: %w", err)
		}
		for _, b := range bs {
			buckets = append(buckets, b.Name)
		}
		fetched = true
		return nil
	}

	notes := make([]string, 0, len(m.S3))
	for i := range m.S3 {
		if m.S3[i].Key == "" {
			return nil, fmt.Errorf("s3[%d].key is required (the object path inside the bucket, e.g. \"config/app.yaml\")", i)
		}
		if m.S3[i].Bucket == "" {
			if err := fetch(); err != nil {
				return nil, err
			}
			switch len(buckets) {
			case 0:
				return nil, fmt.Errorf("s3[%d].bucket is empty and this project has no S3 bucket — create one (s3_buckets to check) or set s3[%d].bucket", i, i)
			case 1:
				m.S3[i].Bucket = buckets[0]
			default:
				return nil, fmt.Errorf("s3[%d].bucket is empty and this project has %d buckets %v — set s3[%d].bucket explicitly", i, len(buckets), buckets, i)
			}
		}
		notes = append(notes, fmt.Sprintf("%s/%s", m.S3[i].Bucket, m.S3[i].Key))
	}
	return notes, nil
}

func (s *Registry) handleStackApply(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "job_id")
	if id == "" {
		return errResult(fmt.Errorf("'job_id' is required (from stack_plan)")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": "stack_apply provisions BILLABLE resources. Call again with confirmed=true after the captain approved."}), true
	}
	unlock := s.lockJob(id)
	defer unlock()
	job, err := stack.LoadJob(id)
	if err != nil {
		return errResult(err), true
	}
	if job.Status == "destroyed" || job.Status == "destroy_failed" {
		// Its steps are all "done" from the first apply: advancing would
		// report status=done for resources that no longer exist.
		return errResult(fmt.Errorf("job %s is %s: its resources were torn down; run stack_plan again for a new deploy", id, job.Status)), true
	}
	s.advance(ctx, job)
	if e := job.Save(); e != nil {
		return errResult(fmt.Errorf("persist job: %w", e)), true
	}
	out := job.Compact()
	out["note"] = "Async — poll stack_status(job_id) until status=done. Each poll advances the deploy."
	return toJSON(out), job.Status == "error"
}

// lockJob serializes the stack tools on one job and returns the unlock.
// Each crank loads the job, starts the pending steps and saves it; two
// concurrent cranks (two HTTP clients, a poll retried while the first one
// still runs) would both see a step pending and create it twice.
func (s *Registry) lockJob(id string) (unlock func()) {
	s.jobMu.Lock()
	if s.jobLocks == nil {
		s.jobLocks = map[string]*sync.Mutex{}
	}
	mu := s.jobLocks[id]
	if mu == nil {
		mu = &sync.Mutex{}
		s.jobLocks[id] = mu
	}
	s.jobMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

// crankJob loads the job and, while it is in progress, advances and saves
// it, all under the job's lock.
func (s *Registry) crankJob(ctx context.Context, id string) (*stack.Job, error) {
	unlock := s.lockJob(id)
	defer unlock()
	job, err := stack.LoadJob(id)
	if err != nil {
		return nil, err
	}
	if job.Status == "running" || job.Status == "planned" {
		s.advance(ctx, job)
		if e := job.Save(); e != nil {
			return nil, fmt.Errorf("persist job: %w", e)
		}
	}
	return job, nil
}

func (s *Registry) handleStackStatus(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "job_id")
	if id == "" {
		return errResult(fmt.Errorf("'job_id' is required")), true
	}
	// Absorb SOME waiting inside the call (crank every few seconds up to a small
	// budget) so the agent doesn't poll in a tight loop — but keep the budget SHORT.
	// A long internal block is uninterruptible from the gateway's side: an MCP call
	// runs in a separate persistent process, so when the gateway aborts a run
	// (timeout / new input) it just stops awaiting the response — it cannot cancel
	// the in-flight tool. A multi-minute wait then makes the gateway's abort-settle
	// time out → "This operation was aborted" → surface_error → the agent goes
	// silent mid-deploy. Capping at 20s keeps every poll quick and abortable; the
	// agent simply re-polls (cheap: compact O(1) status; empty inter-poll turns are
	// handled by the reasoning-only retry). wait_seconds is clamped, not honored.
	waitBudget := pgInt(args, "wait_seconds", 20)
	if waitBudget > 20 {
		waitBudget = 20
	}
	if waitBudget < 1 {
		waitBudget = 1
	}
	deadline := time.Now().Add(time.Duration(waitBudget) * time.Second)
	for {
		// Reloaded on every crank: another call may have advanced it.
		job, err := s.crankJob(ctx, id)
		if err != nil {
			return errResult(err), true
		}
		if job.Status != "running" && job.Status != "planned" {
			return toJSON(job.Compact()), false // terminal (done/error)
		}
		rem := time.Until(deadline)
		if rem <= 0 {
			return toJSON(job.Compact()), false // budget exhausted — agent polls again
		}
		select {
		case <-ctx.Done():
			return errResult(ctx.Err()), true
		case <-time.After(min(stackCrankInterval, rem)):
		}
	}
}

func (s *Registry) handleStackDestroy(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "job_id")
	if id == "" {
		return errResult(fmt.Errorf("'job_id' is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": "stack_destroy PERMANENTLY deletes all resources in this stack. Call again with confirmed=true."}), true
	}
	unlock := s.lockJob(id)
	defer unlock()
	job, err := stack.LoadJob(id)
	if err != nil {
		return errResult(err), true
	}
	// Reconcile ids from the STEPS before teardown: a resource created but not yet
	// "done" (e.g. a k8s cluster still PROVISIONING) has its id on the step, not in
	// Outputs — without this, destroying mid-deploy would ORPHAN it. Also resolve
	// by-name for async resources whose op hadn't completed.
	for _, st := range job.Steps {
		if job.Outputs[st.Key] != "" {
			continue
		}
		if st.ResourceID != "" {
			job.Outputs[st.Key] = st.ResourceID
		} else if st.FindTool != "" && st.FindName != "" {
			if id := s.findIDByName(ctx, st.FindTool, st.FindField, st.FindName, st.FindArgs); id != "" {
				job.Outputs[st.Key] = id
			}
		}
	}
	// Reverse teardown of what was created. 404/not-found is idempotent success.
	var deleted, failed []string
	del := func(label, tool string, dargs map[string]any) {
		dargs["confirmed"] = true
		if _, e := s.callTool(ctx, tool, dargs); e != nil {
			msg := e.Error()
			if strings.Contains(msg, "404") || strings.Contains(strings.ToLower(msg), "not found") {
				deleted = append(deleted, label+" (already gone)")
			} else {
				failed = append(failed, label+": "+msg)
			}
		} else {
			deleted = append(deleted, label)
		}
	}
	if id := job.Outputs["dns"]; id != "" {
		del("dns", "dns_record_delete", map[string]any{"record_id": id})
	}
	if id := job.Outputs["lb"]; id != "" {
		del("lb", "lb_delete", map[string]any{"id": id})
	}
	if id := job.Outputs["tg"]; id != "" {
		del("tg", "tg_delete", map[string]any{"id": id})
	}
	if id := job.Outputs["k8s"]; id != "" {
		del("k8s", "k8s_delete", map[string]any{"cluster_id": id})
	}
	if id := job.Outputs["kafka"]; id != "" {
		del("kafka", "kafka_delete", map[string]any{"cluster_id": id})
	}
	if id := job.Outputs["redis"]; id != "" {
		del("redis", "redis_delete", map[string]any{"cluster_id": id})
	}
	if id := job.Outputs["snat"]; id != "" {
		del("snat", "snat_delete", map[string]any{"id": id})
	}
	if id := job.Outputs["registry"]; id != "" {
		del("registry", "ar_delete", map[string]any{"id": id})
	}
	if id := job.Outputs["postgres"]; id != "" {
		del("postgres", "pg_delete", map[string]any{"cluster_id": id})
	}
	if id := job.Outputs["vm"]; id != "" {
		del("vm", "destroy", map[string]any{"vm_id": id})
	}
	for i, sc := range job.Manifest.Secrets {
		if job.Outputs[fmt.Sprintf("secret:%d", i)] != "" {
			del("secret", "secret_delete", map[string]any{"path": sc.Path, "force": true})
		}
	}
	for i, s3 := range job.Manifest.S3 {
		if job.Outputs[fmt.Sprintf("s3:%d", i)] != "" {
			del("s3", "s3_rm", map[string]any{"bucket": s3.Bucket, "key": s3.Key})
		}
	}
	// VPC last — its subnets/resources must be gone first.
	if id := job.Outputs["vpc"]; id != "" {
		del("vpc", "vpc_delete", map[string]any{"id": id})
	}
	// A job with a failed deletion still holds resources: "destroyed" would
	// hide it from the duplicate warning of stack_plan. Calling stack_destroy
	// again retries; what is gone already counts as deleted.
	job.Status = "destroyed"
	if len(failed) > 0 {
		job.Status = "destroy_failed"
	}
	if e := job.Save(); e != nil {
		failed = append(failed, "persist job: "+e.Error())
	}
	return toJSON(map[string]any{"status": job.Status, "deleted": deleted, "failed": failed}), len(failed) > 0
}
