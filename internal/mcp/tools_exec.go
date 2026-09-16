package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/deploy"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

// Running a script on a VM used to have no tool at all.
//
// The engine could create a VM (provision) and deploy a compose project
// (deploy), but anything else — installing packages, baking a Marketplace
// image, wiping a disk before a snapshot — was done by a human over SSH,
// outside the engine. That is the one path the whole design says must not
// exist: "no component talks to the cloud except through the engine". The
// image factory could not be automated at all while this gap was open.
//
// Two properties drive the shape of these tools.
//
// Builds are LONG. A Marketplace image takes 10-20 minutes: dist-upgrade,
// docker pull, product start, health probe. Deployer.Exec caps a command at
// 120 seconds, and an SSH session that dies mid-build takes the build with it.
// So vm_exec does not hold the session open: it starts the script detached
// (setsid, stdin closed, output redirected), returns a run id immediately, and
// vm_exec_status polls for the exit code. The build survives a dropped
// connection, a restarted agent and an expired token.
//
// Scripts are CODE, not arguments. Interpolating an 8 KB bash script with
// heredocs and quotes into a command line is a quoting bug waiting to happen,
// and a shell injection if any part of it came from outside. The script is
// uploaded as a file over SFTP and executed by path; nothing is interpolated.

const (
	// execRoot is where run directories live on the VM: one per run, holding
	// the script, its output and its exit code.
	execRoot = ".cloudru-exec"
	// execPollInterval is how often a waiting vm_exec re-checks the exit code.
	execPollInterval = 10 * time.Second
	// execMaxTail caps how much log a status call returns. A build log can be
	// megabytes; an MCP result that large is unusable for the caller.
	execMaxTail = 400
)

// execTarget is everything needed to reach a VM over SSH.
type execTarget struct {
	VMID  string
	Host  string
	Login string
	Key   []byte
}

// resolveExecTarget finds the VM, its public address and the login to use.
//
// A public address is required: the engine reaches the VM from wherever it
// runs, and a private-only VM is not reachable from there. Saying so plainly
// beats a generic SSH timeout ten minutes later.
func (s *Server) resolveExecTarget(ctx context.Context, args map[string]any) (execTarget, error) {
	ref := stringArg(args, "vm_id")
	if ref == "" {
		ref = stringArg(args, "name")
	}
	if ref == "" {
		return execTarget{}, fmt.Errorf("vm_id or name is required")
	}

	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return execTarget{}, err
	}
	vmID := ref
	if !looksUUID(ref) {
		id, _, e := s.resolveVM(ctx, ref, eng.Config.ProjectID)
		if e != nil {
			return execTarget{}, e
		}
		vmID = id
	}
	det, err := eng.Cloud.GetVMDetail(ctx, vmID)
	if err != nil {
		return execTarget{}, err
	}

	host := ""
	for _, in := range det.Interfaces {
		if in.FloatingIP != nil && in.FloatingIP.IPAddress != "" && (host == "" || in.Primary) {
			host = in.FloatingIP.IPAddress
		}
		if in.Type == "direct_ip" && in.IPAddress != "" && (host == "" || in.Primary) {
			host = in.IPAddress
		}
	}
	if host == "" {
		return execTarget{}, fmt.Errorf(
			"VM %s has no public address; the engine cannot reach it over SSH. "+
				"Attach a floating IP (fip_attach) or run the build from inside the VPC", vmID)
	}

	login := stringArg(args, "login")
	if login == "" {
		login = loginFromState(eng.StateDir, vmID)
	}
	if login == "" {
		return execTarget{}, fmt.Errorf(
			"cannot tell which login to use for VM %s: it is not in local state. Pass 'login' explicitly", vmID)
	}
	if len(eng.SSHKeyPEM) == 0 {
		return execTarget{}, fmt.Errorf("the engine has no SSH key; it cannot reach any VM")
	}
	return execTarget{VMID: vmID, Host: host, Login: login, Key: eng.SSHKeyPEM}, nil
}

// loginFromState returns the SSH login recorded for this VM, if any.
func loginFromState(stateDir, vmID string) string {
	sf, err := state.Read(state.DefaultPath(stateDir))
	if err != nil {
		return ""
	}
	for _, d := range sf.Deployments {
		if d.VMID == vmID || d.ID == vmID {
			return d.SSHUser
		}
	}
	return ""
}

// connect opens an SSH session to the target, retrying briefly.
//
// sshd on a fresh VM lags VM-active by a few seconds, and a build orchestrator
// that gives up on the first refused connection would fail most first runs.
func connectTarget(ctx context.Context, t execTarget) (*deploy.SSHDeployer, error) {
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(8 * time.Second):
			}
		}
		d := deploy.NewSSHDeployer()
		if err := d.Connect(ctx, t.Host, 22, t.Login, t.Key, ""); err != nil {
			last = err
			continue
		}
		return d, nil
	}
	return nil, fmt.Errorf("cannot reach %s@%s over SSH: %w", t.Login, t.Host, last)
}

// handleVMExec uploads a script to a VM and runs it, detached by default.
//
// Mutation, and the widest one the engine has: it runs code the caller wrote,
// as that VM. It is gated on confirmed=true like every other mutating tool.
func (s *Server) handleVMExec(ctx context.Context, args map[string]any) (string, bool) {
	script := stringArg(args, "script")
	if strings.TrimSpace(script) == "" {
		return errResult(fmt.Errorf("'script' is required and must not be empty")), true
	}
	wait := boolArg(args, "wait", false)
	timeout := intArg(args, "timeout_sec", 1800)
	if timeout <= 0 || timeout > 7200 {
		return errResult(fmt.Errorf("timeout_sec must be between 1 and 7200")), true
	}

	// The gate comes before authentication, so an unapproved call never reaches
	// the cloud at all. The preview shows what will run, not just that
	// something will: an operator approving arbitrary code needs to see it.
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": "vm_exec runs a script ON the VM as the login user (the script may use sudo). " +
				"Read the preview, then call again with confirmed=true.",
			"will_run": map[string]any{
				"vm":           firstNonEmptyStr(stringArg(args, "vm_id"), stringArg(args, "name")),
				"script_bytes": len(script),
				"script_head":  headLines(script, 20),
				"detached":     !wait,
				"timeout_sec":  timeout,
			},
		}), true
	}

	target, err := s.resolveExecTarget(ctx, args)
	if err != nil {
		return errResult(err), true
	}
	d, err := connectTarget(ctx, target)
	if err != nil {
		return errResult(err), true
	}
	defer d.Close()

	runID := fmt.Sprintf("run-%d", time.Now().UTC().UnixNano())
	dir := path.Join(execRoot, runID)

	// The script travels as a file, never as part of a command line.
	if err := d.Upload(ctx, []deploy.FileEntry{
		{RemotePath: path.Join(dir, "script.sh"), Content: []byte(script)},
	}); err != nil {
		return errResult(fmt.Errorf("uploading the script: %w", err)), true
	}

	// setsid plus closed stdin and redirected output detach the run from this
	// SSH session: it survives the connection, the agent and the token. The
	// exit code lands in a file, which is the only thing status has to read.
	start := fmt.Sprintf(
		"cd %s && setsid bash -c 'bash script.sh > out.log 2>&1; echo $? > rc' < /dev/null > /dev/null 2>&1 & echo started",
		shellQuoteMCP(dir))
	if _, err := d.Exec(ctx, start); err != nil {
		// The acknowledgement can be lost even though the run is detached and
		// going: setsid survives the session, so a dead SSH channel says
		// nothing about the script. Reporting an error here loses the run id,
		// and the caller marks a step failed while the work completes on the
		// VM unnoticed — seen twice on the first live Gitea build.
		// So: ask the VM whether the run exists before deciding.
		if !runStarted(ctx, target, dir) {
			return errResult(fmt.Errorf("starting the script: %w", err)), true
		}
	}

	if !wait {
		return toJSON(map[string]any{
			"status": "started",
			"run_id": runID,
			"vm_id":  target.VMID,
			"note": "Detached: this returns immediately. Poll vm_exec_status with this run_id " +
				"for the exit code and output.",
		}), false
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		st, err := readRunStatus(ctx, d, runID)
		if err != nil {
			return errResult(err), true
		}
		if st.Done {
			st.VMID = target.VMID
			// A non-zero exit is a failed build, and the caller must not read
			// it as success. It is reported as an error result with the log.
			return toJSON(st), st.ExitCode != 0
		}
		if time.Now().After(deadline) {
			return toJSON(map[string]any{
				"status":  "timeout",
				"run_id":  runID,
				"vm_id":   target.VMID,
				"message": fmt.Sprintf("still running after %ds; it keeps running on the VM", timeout),
				"tail":    st.Tail,
			}), true
		}
		select {
		case <-ctx.Done():
			return errResult(ctx.Err()), true
		case <-time.After(execPollInterval):
		}
	}
}

// runStatus is the state of one detached run.
type runStatus struct {
	Status   string `json:"status"` // running | done
	RunID    string `json:"run_id"`
	VMID     string `json:"vm_id,omitempty"`
	Done     bool   `json:"-"`
	ExitCode int    `json:"exit_code"`
	Tail     string `json:"tail"`
}

// readRunStatus reads the exit code file and the tail of the log.
func readRunStatus(ctx context.Context, d *deploy.SSHDeployer, runID string) (runStatus, error) {
	dir := path.Join(execRoot, runID)
	// One round trip: the marker line separates the exit code from the log, so
	// a status poll costs one SSH session rather than two.
	cmd := fmt.Sprintf("cat %s 2>/dev/null; echo '---CLOUDRU-EXEC---'; tail -n %d %s 2>/dev/null",
		shellQuoteMCP(path.Join(dir, "rc")), execMaxTail, shellQuoteMCP(path.Join(dir, "out.log")))
	out, err := d.Exec(ctx, cmd)
	if err != nil {
		return runStatus{}, fmt.Errorf("reading run %s: %w", runID, err)
	}
	return parseRunOutput(runID, out), nil
}

// parseRunOutput splits the status probe output into exit code and log tail.
//
// Separate from the SSH call so it can be tested without a VM: this is where a
// build is declared finished or not, and getting it wrong means either polling
// forever or calling a running build complete.
func parseRunOutput(runID, out string) runStatus {
	parts := strings.SplitN(out, "---CLOUDRU-EXEC---", 2)
	rc := strings.TrimSpace(parts[0])
	tail := ""
	if len(parts) > 1 {
		tail = strings.TrimLeft(parts[1], "\n")
	}
	st := runStatus{Status: "running", RunID: runID, ExitCode: -1, Tail: tail}
	if rc == "" {
		return st
	}
	var code int
	if _, err := fmt.Sscanf(rc, "%d", &code); err != nil {
		// The file exists but holds something else: treat it as finished with
		// an unknown code rather than polling forever.
		st.Status, st.Done = "done", true
		return st
	}
	st.Status, st.Done, st.ExitCode = "done", true, code
	return st
}

// handleVMExecStatus reports on a run started by vm_exec. Read-only.
func (s *Server) handleVMExecStatus(ctx context.Context, args map[string]any) (string, bool) {
	runID := stringArg(args, "run_id")
	if runID == "" {
		return errResult(fmt.Errorf("'run_id' is required")), true
	}
	if strings.ContainsAny(runID, "/ \t\n'\"\\") {
		// run_id becomes a path on the VM: anything else is a traversal.
		return errResult(fmt.Errorf("run_id %q is not a run identifier", runID)), true
	}
	target, err := s.resolveExecTarget(ctx, args)
	if err != nil {
		return errResult(err), true
	}
	d, err := connectTarget(ctx, target)
	if err != nil {
		return errResult(err), true
	}
	defer d.Close()

	st, err := readRunStatus(ctx, d, runID)
	if err != nil {
		return errResult(err), true
	}
	st.VMID = target.VMID
	return toJSON(st), false
}

// handleVMUpload writes files onto a VM.
//
// Separate from vm_exec because build inputs are not scripts: a recipe puts
// config files, systemd units and a compose file on the machine, and shipping
// them inside a here-document is how quoting bugs get baked into an image.
// Mutation: requires confirmed=true.
func (s *Server) handleVMUpload(ctx context.Context, args map[string]any) (string, bool) {
	raw, ok := args["files"]
	if !ok || raw == nil {
		return errResult(fmt.Errorf("'files' is required: [{path, content, mode?}]")), true
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		return errResult(fmt.Errorf("files: %w", err)), true
	}
	var files []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(blob, &files); err != nil {
		return errResult(fmt.Errorf("files must be an array of {path, content, mode?}: %w", err)), true
	}
	if len(files) == 0 {
		return errResult(fmt.Errorf("'files' is empty")), true
	}
	for i, f := range files {
		if f.Path == "" {
			return errResult(fmt.Errorf("files[%d].path is empty", i)), true
		}
		// Uploads land under the login user's home; an absolute path or a
		// traversal would put them somewhere nobody asked for.
		if strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, "..") {
			return errResult(fmt.Errorf(
				"files[%d].path %q: use a path relative to the login user's home, without '..'; "+
					"to place a file elsewhere, move it with vm_exec so the privilege is explicit", i, f.Path)), true
		}
	}

	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		names := make([]string, 0, len(files))
		total := 0
		for _, f := range files {
			names = append(names, f.Path)
			total += len(f.Content)
		}
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "vm_upload writes files onto the VM. Approve, then call again with confirmed=true.",
			"will_write": map[string]any{
				"vm":    firstNonEmptyStr(stringArg(args, "vm_id"), stringArg(args, "name")),
				"files": names, "total_bytes": total,
			},
		}), true
	}

	target, err := s.resolveExecTarget(ctx, args)
	if err != nil {
		return errResult(err), true
	}
	d, err := connectTarget(ctx, target)
	if err != nil {
		return errResult(err), true
	}
	defer d.Close()

	entries := make([]deploy.FileEntry, 0, len(files))
	for _, f := range files {
		entries = append(entries, deploy.FileEntry{RemotePath: f.Path, Content: []byte(f.Content)})
	}
	if err := d.Upload(ctx, entries); err != nil {
		return errResult(err), true
	}
	// Modes are applied after upload: SFTP writes with a default mode, and a
	// script uploaded without the executable bit is a confusing failure later.
	var chmods []string
	for _, f := range files {
		if f.Mode != "" {
			chmods = append(chmods, fmt.Sprintf("chmod %s %s", shellQuoteMCP(f.Mode), shellQuoteMCP(f.Path)))
		}
	}
	if len(chmods) > 0 {
		if _, err := d.Exec(ctx, strings.Join(chmods, " && ")); err != nil {
			return errResult(fmt.Errorf("setting file modes: %w", err)), true
		}
	}
	return toJSON(map[string]any{"status": "uploaded", "vm_id": target.VMID, "files": len(files)}), false
}

// headLines returns the first n lines of s, for a confirmation preview.
func headLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n… (%d more lines)", len(lines)-n)
	}
	return s
}

// runStarted answers whether a detached run actually got going, for the case
// where the SSH acknowledgement of the start command was lost. The run
// directory alone is not proof — vm_exec creates it when uploading script.sh,
// before starting anything. Proof is out.log or rc: both are created by the
// detached shell itself.
//
// A fresh connection is used on purpose: the one that timed out is suspect.
func runStarted(ctx context.Context, t execTarget, dir string) bool {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	d, err := connectTarget(ctx, t)
	if err != nil {
		return false
	}
	defer d.Close()

	out, err := d.Exec(ctx, fmt.Sprintf(
		"test -e %s/out.log -o -e %s/rc && echo yes || echo no",
		shellQuoteMCP(dir), shellQuoteMCP(dir)))
	if err != nil {
		return false
	}
	return strings.Contains(out, "yes")
}
