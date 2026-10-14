package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/bootstrap"
	"github.com/shatim-ops/cloudru-vm-cli/internal/compose"
	"github.com/shatim-ops/cloudru-vm-cli/internal/confirm"
	"github.com/shatim-ops/cloudru-vm-cli/internal/deploy"
	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
	"github.com/shatim-ops/cloudru-vm-cli/internal/sizer"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
	"github.com/shatim-ops/cloudru-vm-cli/internal/verify"
)

const (
	totalSteps        = 5
	sshMaxRetries     = 60
	remoteComposeDir  = "/opt/app"
	remoteComposeFile = "/opt/app/docker-compose.yml"

	// defaultDiskTypeName is looked up in list_disk_types (case-insensitive);
	// its UUID is per tenant.
	defaultDiskTypeName = "ssd"
	// dockerReadyCmd succeeds once the compose plugin exists and the SSH user
	// may talk to the daemon (docker group).
	dockerReadyCmd = "docker compose version && docker info >/dev/null"
)

// Deploy orchestrates the full deploy lifecycle.
func (e *Engine) Deploy(ctx context.Context, opts DeployOptions) (retErr error) {
	// Stage 1: Parse compose file.
	project, err := compose.Parse(opts.ComposeFile)
	if err != nil {
		return err
	}

	statePath := state.DefaultPath(e.StateDir)

	// Stage 2: Size — check existing state.
	stateFile, err := state.Read(statePath)
	if err != nil {
		return err
	}

	existing := stateFile.ActiveDeployment()
	isNewDeploy := existing == nil

	// Skip only a deployment that finished: after an SSH or Docker failure the
	// hash is already recorded, and a plain re-run must retry.
	if !isNewDeploy && existing.ComposeHash == project.Hash && existing.Status == "running" && !opts.Force {
		e.Printer.Success("No changes detected in compose file (hash unchanged). Nothing to deploy.")
		e.Printer.Hint("Use --force to redeploy anyway.")
		return nil
	}
	if !isNewDeploy && opts.DryRun {
		// Updating an existing VM creates nothing billable, so it is not
		// gated; --dry-run still must not touch it.
		_, err := confirm.Ask(ctx, redeployPreview(existing), e.confirmOptions(false, true, false))
		return err
	}

	// The key is needed from here on: image metadata and SSH.
	if err := e.EnsureSSHKey(); err != nil {
		return err
	}

	// Step 1: Auth (must happen before any API calls).
	e.Printer.Step(1, totalSteps, "Authenticating...")
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	if ts, ok := e.Cloud.(interface{ SetToken(string) }); ok {
		ts.SetToken(tok.Value)
	}
	e.Printer.StepDone("IAM token valid")

	// Determine flavor (requires auth for ListFlavors API call).
	var flavorName string
	if isNewDeploy {
		flavorName, err = e.resolveFlavor(ctx, opts, project)
		if err != nil {
			return err
		}
	}

	// Step 2: Analyze.
	e.Printer.Step(2, totalSteps, "Analyzing compose file...")
	e.Printer.StepDone(fmt.Sprintf("%d service(s), %d exposed port(s), hash=%s",
		len(project.Services), len(project.Ports), project.Hash[:12]))

	// Determine region.
	region := opts.Region
	if region == "" {
		region = e.Config.Region
	}

	var rec state.DeploymentRecord
	if isNewDeploy {
		// Step 3: Provision.
		rec, err = e.provisionVM(ctx, opts, stateFile, statePath, project, flavorName, region)
		if err == errDryRun {
			return nil
		}
		if vmID := rec.VMID; vmID != "" {
			defer func() { retErr = e.vmLeftRunning(ctx, retErr, vmID) }()
		}
		if err != nil {
			return err
		}
	} else {
		e.Printer.Step(3, totalSteps, "Reusing existing VM...")
		rec = *existing
		rec.ComposeHash = project.Hash
		rec.ExposedPorts = project.Ports
		rec.UpdatedAt = time.Now()
		e.Printer.StepDone(fmt.Sprintf("VM %s at %s", rec.VMID, rec.VMIP))
	}

	// Step 4: Deploy — SSH connect, upload, compose up.
	e.Printer.Step(4, totalSteps, "Deploying application...")

	if rec.VMIP == "" {
		return clerrors.New(clerrors.CodeVMProvisionFailed,
			"VM has no public IP address",
			"Floating IP assignment failed. Check Cloud.ru console and assign a floating IP manually.")
	}

	maxRetries := sshMaxRetries
	if !isNewDeploy {
		maxRetries = 12 // existing VM should be ready quickly
	}

	sshUserForConnect, err := sshUserOf(&rec)
	if err != nil {
		return err
	}

	if err := e.waitForSSH(ctx, rec.VMIP, sshUserForConnect, rec.SSHHostKeyFingerprint, maxRetries); err != nil {
		rec.Status = "ssh_timeout"
		rec.UpdatedAt = time.Now()
		stateFile.UpsertDeployment(rec)
		_ = state.Write(statePath, stateFile)
		return err
	}
	defer e.Deployer.Close()

	// Store host key fingerprint on first connect.
	if rec.SSHHostKeyFingerprint == "" {
		rec.SSHHostKeyFingerprint = e.Deployer.HostKeyFingerprint()
	}

	// Install Docker if the image has none (Ubuntu/Debian chosen by init).
	if err := e.prepareDocker(ctx, rec.VMIP, sshUserForConnect, rec.SSHHostKeyFingerprint); err != nil {
		if ctx.Err() == nil {
			rec.Status = "docker_failed"
			rec.UpdatedAt = time.Now()
			stateFile.UpsertDeployment(rec)
			_ = state.Write(statePath, stateFile)
		}
		return err
	}
	e.Printer.StepDone("SSH connected, docker compose ready")

	// Upload compose file.
	composeData, err := os.ReadFile(opts.ComposeFile)
	if err != nil {
		return clerrors.Wrap(clerrors.CodeComposeNotFound,
			fmt.Sprintf("cannot read compose file: %s", opts.ComposeFile),
			"Check file permissions.",
			err)
	}

	files := []deploy.FileEntry{
		{RemotePath: remoteComposeFile, Content: composeData},
	}
	if err := e.Deployer.Upload(ctx, files); err != nil {
		return err
	}

	// Run docker compose up. Streamed, not Exec: pulling images can take
	// longer than Exec's 120 s limit. Progress goes to stderr in text mode.
	var progress io.Writer = e.Printer.ErrWriter()
	if e.Printer.IsJSON() {
		progress = io.Discard
	}
	if err := e.Deployer.ExecStream(ctx, fmt.Sprintf("cd %s && docker compose -f docker-compose.yml up -d", remoteComposeDir), progress, progress); err != nil {
		return clerrors.Wrap(clerrors.CodeSSHTimeout,
			"docker compose up failed",
			"Check your compose file and service logs on the VM.",
			err)
	}
	e.Printer.StepDone("Application deployed")

	// Update state to running.
	rec.Status = "running"
	rec.UpdatedAt = time.Now()
	stateFile.UpsertDeployment(rec)
	if err := state.Write(statePath, stateFile); err != nil {
		return err
	}

	// Step 5: Verify.
	e.Printer.Step(5, totalSteps, "Verifying health...")
	if len(project.Ports) > 0 {
		// Give containers a moment to start.
		t := e.timings()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.SettleDelay):
		}
		probe := e.Probe
		if probe == nil {
			probe = verify.Check
		}
		results := probe(ctx, rec.VMIP, project.Ports, t.VerifyTimeout)
		e.printVerifyResults(results, rec.VMIP)
	} else {
		e.Printer.StepDone("No exposed ports to verify")
	}

	e.Printer.Success(fmt.Sprintf("\nDeployment complete! VM IP: %s", rec.VMIP))
	return nil
}

// resolveFlavor determines the VM flavor via auto-sizing or explicit selection.
func (e *Engine) resolveFlavor(ctx context.Context, opts DeployOptions, project *compose.Project) (string, error) {
	flavor := opts.Flavor
	if flavor == "" {
		flavor = e.Config.Flavor
	}

	if flavor != "" && flavor != "auto" {
		return flavor, nil
	}

	// Auto-size: estimate resources, then match against available flavors.
	estimated := sizer.EstimateFromServices(project.Services)

	flavors, err := e.Cloud.ListFlavors(ctx)
	if err != nil {
		return "", err
	}

	// Convert []*Flavor to []Flavor for sizer.Match.
	flavorList := make([]provision.Flavor, len(flavors))
	for i, f := range flavors {
		flavorList[i] = *f
	}

	match := sizer.Match(estimated, flavorList)
	if match == nil {
		return "", clerrors.New(clerrors.CodeConfigInvalid,
			"no suitable flavor found for the workload",
			"Specify a flavor explicitly with --flavor.")
	}

	e.Printer.StepDone(fmt.Sprintf("Recommended flavor: %s (%d vCPU, %d GB RAM)",
		match.Flavor.Name, match.Flavor.VCPUs, match.Flavor.RAMGB))

	return match.Flavor.ID, nil
}

// provisionVM creates a new VM and waits for it to become active.
func (e *Engine) provisionVM(ctx context.Context, opts DeployOptions, stateFile *state.File, statePath string, project *compose.Project, flavorName, region string) (state.DeploymentRecord, error) {
	e.Printer.Step(3, totalSteps, "Provisioning VM...")

	projectID := e.Config.ProjectID
	if projectID == "" {
		return state.DeploymentRecord{}, clerrors.New(clerrors.CodeConfigInvalid,
			"CLOUDRU_PROJECT_ID is required",
			"Set CLOUDRU_PROJECT_ID environment variable or project_id in config file. Find it at https://console.cloud.ru")
	}

	imageID := e.Config.ImageID
	if imageID == "" {
		return state.DeploymentRecord{}, clerrors.New(clerrors.CodeConfigInvalid,
			"CLOUDRU_IMAGE_ID is required",
			"Set CLOUDRU_IMAGE_ID environment variable or image_id in config file. Find available images in Cloud.ru console.")
	}

	projectName := e.Config.ProjectName
	if projectName == "" {
		projectName = filepath.Base(e.StateDir)
	}

	diskSize := e.Config.DiskSize
	if diskSize <= 0 {
		diskSize = 10
	}

	// Fetch image details to discover required metadata field names.
	imgDetail, err := e.Cloud.GetImage(ctx, imageID)
	if err != nil {
		return state.DeploymentRecord{}, fmt.Errorf("get image details: %w", err)
	}

	// Build image_metadata dict: keys are field names from the image definition.
	// Track which mandatory groups are already satisfied so we don't send
	// empty/invalid values for alternative fields in the same group
	// (e.g. public_key vs linux_password in "authentication" group).
	imgMeta := map[string]string{}
	satisfiedGroups := map[string]bool{}

	// First pass: populate fields we know how to fill.
	for _, mf := range imgDetail.MetadataFields {
		switch mf.Name {
		case "ssh_key", "public_key", "ssh_public_key":
			imgMeta[mf.Name] = e.SSHPubKey
			if mf.MandatoryGroup != "" {
				satisfiedGroups[mf.MandatoryGroup] = true
			}
		case "hostname", "instance_name":
			imgMeta[mf.Name] = fmt.Sprintf("cloudru-vm-%s", projectName)
			if mf.MandatoryGroup != "" {
				satisfiedGroups[mf.MandatoryGroup] = true
			}
		case "linux_password":
			// Without it the VM never leaves "creating" (seen live
			// 2026-10-14; MCP vm_create sets it for the same reason). Login
			// is by SSH key: the password is random and not kept.
			imgMeta[mf.Name] = randomPassword(16)
			if mf.MandatoryGroup != "" {
				satisfiedGroups[mf.MandatoryGroup] = true
			}
		case "name", "user", "username", "default_user":
			val := mf.DefaultValue
			if val == "" {
				val = "ubuntu"
			}
			imgMeta[mf.Name] = val
			if mf.MandatoryGroup != "" {
				satisfiedGroups[mf.MandatoryGroup] = true
			}
		}
	}

	// Second pass: fill remaining fields that aren't in an already-satisfied
	// mandatory group (avoids sending empty passwords, etc.).
	for _, mf := range imgDetail.MetadataFields {
		if _, exists := imgMeta[mf.Name]; exists {
			continue
		}
		// Skip fields whose mandatory group is already satisfied by another field.
		if mf.MandatoryGroup != "" && satisfiedGroups[mf.MandatoryGroup] {
			continue
		}
		// Only fields with a real default: empty values and these two flags
		// make the create API answer 500 (same rule as MCP vm_create).
		if mf.Name == "vmm_enabled" || mf.Name == "guest_agent_ready" {
			continue
		}
		if mf.DefaultValue != "" {
			imgMeta[mf.Name] = mf.DefaultValue
		}
	}

	// Determine SSH user from metadata (the "name" field is the login user;
	// Cloud.ru images default it to user1).
	sshUser := ""
	for _, key := range []string{"name", "user", "username", "default_user"} {
		if v, ok := imgMeta[key]; ok && v != "" {
			sshUser = v
			break
		}
	}
	if sshUser == "" {
		return state.DeploymentRecord{}, clerrors.New(clerrors.CodeConfigInvalid,
			fmt.Sprintf("image %s declares no login user", imageID),
			"Choose a Linux image whose metadata has a 'name' field (cloudru-vm list-images), e.g. Ubuntu-24.04.")
	}

	diskType, err := e.resolveDiskType(ctx, projectID, defaultDiskTypeName)
	if err != nil {
		return state.DeploymentRecord{}, err
	}

	network, err := e.resolveNetwork(ctx, projectID, region)
	if err != nil {
		return state.DeploymentRecord{}, err
	}
	flavorAPIName, err := e.flavorAPIName(ctx, flavorName)
	if err != nil {
		return state.DeploymentRecord{}, err
	}

	// The create API takes names, not UUIDs, and an explicit interface:
	// UUIDs or a missing interface make it answer 500.
	req := provision.CreateVMRequest{
		ProjectID:          projectID,
		Name:               fmt.Sprintf("cloudru-vm-%s", projectName),
		FlavorName:         flavorAPIName,
		AvailabilityZoneID: region,
		ImageName:          imgDetail.Name,
		ImageMetadata:      imgMeta,
		Disks: []provision.DiskReq{
			{
				Name:         fmt.Sprintf("cloudru-vm-%s-boot", projectName),
				Size:         diskSize,
				DiskTypeName: diskType.Name,
			},
		},
		Interfaces: []provision.InterfaceReq{
			{
				Type:                     "regular",
				SubnetID:                 network.SubnetID,
				InterfaceSecurityEnabled: true,
				SecurityGroups:           []string{network.SecurityGroupID},
			},
		},
	}

	// The VM is billed from this call on: show what will be created and what
	// it costs, and get consent (or stop here on --dry-run).
	proceed, err := confirm.Ask(ctx, e.deployPreview(ctx, req, imgDetail, network, project.Ports), e.confirmOptions(opts.Yes, opts.DryRun, opts.NonInteractive))
	if err != nil {
		return state.DeploymentRecord{}, err
	}
	if !proceed {
		return state.DeploymentRecord{}, errDryRun
	}

	vm, err := e.Cloud.CreateVM(ctx, req)
	if err != nil {
		return state.DeploymentRecord{}, err
	}

	// Register signal handler after VM creation.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Write initial state immediately.
	rec := state.DeploymentRecord{
		ID:           vm.ID,
		VMID:         vm.ID,
		VMIP:         vm.PublicIP,
		VMFlavor:     flavorName,
		Region:       region,
		SSHUser:      sshUser,
		Status:       "creating",
		ComposeHash:  project.Hash,
		ExposedPorts: project.Ports,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	stateFile.UpsertDeployment(rec)
	if err := state.Write(statePath, stateFile); err != nil {
		return state.DeploymentRecord{}, err
	}

	// Poll until active.
	t := e.timings()
	pollDeadline := time.After(t.VMPollTimeout)
	ticker := time.NewTicker(t.VMPollInterval)
	defer ticker.Stop()

	for {
		select {
		case sig := <-sigCh:
			return rec, e.handleInterrupt(sig.String(), vm.ID, opts.NonInteractive)
		case <-pollDeadline:
			return rec, clerrors.New(clerrors.CodeVMProvisionFailed,
				fmt.Sprintf("VM %s did not become active within %s", vm.ID, t.VMPollTimeout),
				"Check VM status in Cloud.ru console.")
		case <-ticker.C:
			updated, err := e.Cloud.GetVM(ctx, vm.ID)
			if err != nil {
				e.Printer.Warning(fmt.Sprintf("poll error: %v", err))
				continue
			}
			switch updated.Status {
			case "active", "running":
				e.Printer.StepDone(fmt.Sprintf("VM %s active", vm.ID))

				// Assign a floating (public) IP to the VM.
				publicIP, fipID, err := e.assignFloatingIP(ctx, vm.ID, projectID, region)

				// Record the FIP even if it is not confirmed: destroy releases it.
				rec.VMIP = publicIP
				rec.FloatingIPID = fipID
				rec.Status = "running"
				rec.UpdatedAt = time.Now()
				stateFile.UpsertDeployment(rec)
				_ = state.Write(statePath, stateFile)
				if ctx.Err() != nil {
					return rec, ctx.Err()
				}
				if err != nil {
					e.Printer.Warning(fmt.Sprintf("floating IP: %v", err))
					// Continue without public IP — user can assign manually.
				}
				if publicIP != "" {
					e.Printer.StepDone(fmt.Sprintf("Public IP: %s", publicIP))
				}
				e.openPorts(ctx, projectID, network.SecurityGroupID, project.Ports)
				return rec, nil
			case "error":
				return rec, clerrors.New(clerrors.CodeVMProvisionFailed,
					fmt.Sprintf("VM %s entered error state", vm.ID),
					"Check Cloud.ru console for details.")
			}
		case <-ctx.Done():
			// The CLI cancels ctx on the same SIGINT/SIGTERM that sigCh
			// receives, and select picks a ready case at random. Both cases
			// must lead to the same handling, or the VM may be left running
			// without a word.
			return rec, e.handleInterrupt("interrupt", vm.ID, opts.NonInteractive)
		}
	}
}

// AssignFloatingIP creates a floating IP and attaches it to the VM's primary interface.
// It polls until the floating IP is in_use or times out.
func AssignFloatingIP(ctx context.Context, cloud provision.CloudAPI, vmID, projectID, azID string) (string, error) {
	ip, _, err := assignFloatingIP(ctx, cloud, Timings{}.withDefaults(), nil, vmID, projectID, azID)
	return ip, err
}

// assignFloatingIP prints its progress: the waits add up to minutes.
func (e *Engine) assignFloatingIP(ctx context.Context, vmID, projectID, azID string) (ip, fipID string, err error) {
	return assignFloatingIP(ctx, e.Cloud, e.timings(), e.Printer.StepDone, vmID, projectID, azID)
}

// fipProgressEvery is how many polls pass between progress lines.
const fipProgressEvery = 6

// assignFloatingIP creates a floating IP and attaches it to the VM's primary
// interface. It returns the address and the FIP ID; the ID is set as soon as
// the FIP exists, even when the wait for it fails, so the caller can record
// it for destroy. progress (may be nil) receives human-readable status.
func assignFloatingIP(ctx context.Context, cloud provision.CloudAPI, t Timings, progress func(string), vmID, projectID, azID string) (string, string, error) {
	start := time.Now()
	report := func(format string, args ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, args...))
		}
	}
	elapsed := func() time.Duration { return time.Since(start).Round(time.Second) }

	// Poll until the primary interface reaches "created" state.
	// Cloud.ru returns 422 "interface_can_not_be_changed_from_current_state"
	// if the interface is still being provisioned.
	report("Waiting for the VM network interface...")
	var ifaceID string
	for attempt := 0; attempt < ifaceMaxAttempts; attempt++ {
		if attempt > 0 && attempt%fipProgressEvery == 0 {
			report("Still waiting for the network interface (%s)", elapsed())
		}
		vmDetail, err := cloud.GetVMDetail(ctx, vmID)
		if err != nil {
			select {
			case <-ctx.Done():
				return "", "", ctx.Err()
			case <-time.After(t.IfacePollInterval):
				continue
			}
		}

		for _, iface := range vmDetail.Interfaces {
			if iface.Primary && iface.State == "created" {
				ifaceID = iface.ID
				break
			}
		}
		if ifaceID != "" {
			break
		}

		// Also accept primary interface without state check as fallback.
		if attempt >= ifaceFallbackAttempt {
			for _, iface := range vmDetail.Interfaces {
				if iface.Primary {
					ifaceID = iface.ID
					break
				}
			}
			if ifaceID != "" {
				break
			}
		}

		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(t.IfacePollInterval):
		}
	}
	if ifaceID == "" {
		return "", "", fmt.Errorf("no primary interface found on VM %s after %s (or interface not ready)", vmID, elapsed())
	}

	// Create floating IP with retry.
	report("Creating floating IP...")
	var fip *provision.FloatingIP
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		fip, err = cloud.CreateFloatingIP(ctx, provision.CreateFloatingIPRequest{
			Name:               fmt.Sprintf("fip-%s", vmID),
			ProjectID:          projectID,
			InterfaceID:        ifaceID,
			AvailabilityZoneID: azID,
		})
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		report("Creating floating IP failed (%v), retrying", err)
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(t.FIPRetryInterval):
		}
	}
	if err != nil {
		return "", "", fmt.Errorf("create floating IP: %w", err)
	}

	// Poll until floating IP is active.
	fipID := fip.ID
	for attempt := 0; attempt < 12; attempt++ {
		if fip.State == "in_use" || fip.State == "active" {
			return fip.IPAddress, fipID, nil
		}
		if attempt == 0 {
			report("Waiting for floating IP %s to attach...", fip.IPAddress)
		} else if attempt%fipProgressEvery == 0 {
			report("Still waiting for floating IP %s (state %q, %s)", fip.IPAddress, fip.State, elapsed())
		}
		select {
		case <-ctx.Done():
			return fip.IPAddress, fipID, ctx.Err()
		case <-time.After(t.FIPPollInterval):
		}
		updated, err := cloud.GetFloatingIP(ctx, fipID)
		if err != nil {
			continue
		}
		fip = updated
	}

	// Return whatever IP we got even if state isn't confirmed.
	report("Floating IP %s not confirmed after %s; continuing", fip.IPAddress, elapsed())
	return fip.IPAddress, fipID, nil
}

// waitForSSH retries SSH connection until the VM is reachable.
func (e *Engine) waitForSSH(ctx context.Context, ip, user, knownHostKey string, maxRetries int) error {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := e.Deployer.Connect(ctx, ip, 22, user, e.SSHKeyPEM, knownHostKey)
		if err == nil {
			return nil
		}
		lastErr = err

		// Don't retry on auth failures or host key mismatches.
		if cliErr, ok := err.(*clerrors.CLIError); ok {
			if cliErr.Code == clerrors.CodeSSHAuthFailed || cliErr.Code == clerrors.CodeSSHHostKeyChanged {
				return err
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.timings().SSHRetryInterval):
		}
	}
	return clerrors.Wrap(clerrors.CodeSSHTimeout,
		fmt.Sprintf("SSH connection failed after %d attempts", maxRetries),
		"VM may still be booting. Try again in a few minutes.",
		lastErr)
}

// prepareDocker makes Docker with the compose plugin usable by user on the
// VM. Cloud.ru renders cloud-init only from the image's own template, so the
// install runs over SSH (internal/bootstrap); on a VM that already has
// Docker it only checks the docker group and /opt/app. Retries until
// Timings.DockerWaitTimeout: first-boot cloud-init may still hold the apt lock.
func (e *Engine) prepareDocker(ctx context.Context, ip, user, hostKey string) error {
	t := e.timings()
	start := time.Now()
	deadline := start.Add(t.DockerWaitTimeout)
	installCmd := bootstrap.InstallCommand(user)

	e.Printer.StepDone("Preparing Docker on the VM (a fresh VM takes a few minutes)...")
	var lastErr error
	var lastOut string
	for attempt := 1; ; attempt++ {
		var out bytes.Buffer
		attemptCtx, cancel := context.WithDeadline(ctx, deadline)
		err := e.Deployer.ExecStream(attemptCtx, installCmd, &out, &out)
		cancel()
		if err == nil {
			// Group membership is fixed at login: reconnect so the docker
			// group the script may have just added applies.
			err = e.reconnect(ctx, ip, user, hostKey)
			if err == nil {
				_, err = e.Deployer.Exec(ctx, dockerReadyCmd)
			}
			if err == nil {
				return nil
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if isFatalSSHError(err) {
			return err
		}
		lastErr, lastOut = err, out.String()
		if !time.Now().Before(deadline) {
			break
		}
		e.Printer.Warning(fmt.Sprintf("Docker not ready yet (attempt %d, %s elapsed), retrying...",
			attempt, time.Since(start).Round(time.Second)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.DockerPollInterval):
		}
		// A failed attempt may have lost the connection.
		if err := e.reconnect(ctx, ip, user, hostKey); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if isFatalSSHError(err) {
				return err
			}
		}
	}

	hint := "Check that the VM reaches get.docker.com, then re-run 'cloudru-vm deploy'."
	if tail := lastLines(lastOut, 15); tail != "" {
		hint = "Last output of the Docker install:\n" + tail + "\n" + hint
	}
	return clerrors.Wrap(clerrors.CodeSSHTimeout,
		fmt.Sprintf("Docker is not ready on VM %s after %s", ip, t.DockerWaitTimeout),
		hint, lastErr)
}

// reconnect opens a fresh SSH connection to the VM.
func (e *Engine) reconnect(ctx context.Context, ip, user, hostKey string) error {
	_ = e.Deployer.Close()
	return e.Deployer.Connect(ctx, ip, 22, user, e.SSHKeyPEM, hostKey)
}

// isFatalSSHError reports errors that retrying cannot fix.
func isFatalSSHError(err error) bool {
	var cliErr *clerrors.CLIError
	return stderrors.As(err, &cliErr) &&
		(cliErr.Code == clerrors.CodeSSHAuthFailed || cliErr.Code == clerrors.CodeSSHHostKeyChanged)
}

// lastLines returns up to n trailing lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// resolveDiskType finds a disk type by name (case-insensitive) in
// list_disk_types: its UUID is per tenant and must not be hardcoded.
func (e *Engine) resolveDiskType(ctx context.Context, projectID, name string) (*provision.DiskType, error) {
	types, err := e.Cloud.ListDiskTypes(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list disk types: %w", err)
	}
	names := make([]string, 0, len(types))
	for _, dt := range types {
		if strings.EqualFold(dt.Name, name) {
			return dt, nil
		}
		names = append(names, dt.Name)
	}
	return nil, clerrors.New(clerrors.CodeConfigInvalid,
		fmt.Sprintf("disk type %q not found in project", name),
		fmt.Sprintf("Available disk types: %s.", strings.Join(names, ", ")))
}

// sshUserOf returns the login user recorded for the deployment.
func sshUserOf(rec *state.DeploymentRecord) (string, error) {
	if rec.SSHUser == "" {
		return "", clerrors.New(clerrors.CodeStateCorrupted,
			fmt.Sprintf("deployment %s has no ssh_user in state", rec.ID),
			"Set \"ssh_user\" in .cloudru-vm/state.json to the image's login user (Cloud.ru images: user1).")
	}
	return rec.SSHUser, nil
}

// vmLeftRunning turns an interruption after the VM became active (floating
// IP, SSH, Docker, upload, compose up) into the same exit-130 error with a
// hint that handleInterrupt gives while waiting for the VM: without it
// Ctrl-C would leave a paid VM running without a word.
func (e *Engine) vmLeftRunning(ctx context.Context, err error, vmID string) error {
	if err == nil {
		return nil
	}
	if !stderrors.Is(ctx.Err(), context.Canceled) && !stderrors.Is(err, context.Canceled) {
		return err
	}
	var cliErr *clerrors.CLIError
	if stderrors.As(err, &cliErr) && strings.Contains(cliErr.Hint, vmID) {
		return err // handleInterrupt already said what happened to the VM
	}
	e.Printer.Warning(fmt.Sprintf("Interrupted. VM %s left running.", vmID))
	cause := err
	if !stderrors.Is(err, context.Canceled) {
		cause = fmt.Errorf("%w: %w", context.Canceled, err)
	}
	return clerrors.Wrap(clerrors.CodeVMProvisionFailed,
		"deployment interrupted",
		fmt.Sprintf("VM %s was created and is still running. Use 'cloudru-vm destroy --force' to delete it.", vmID),
		cause)
}

// handleInterrupt handles SIGINT/SIGTERM (or a cancelled context) after VM
// creation. The returned error wraps context.Canceled, so the CLI exits 130.
func (e *Engine) handleInterrupt(sig string, vmID string, nonInteractive bool) error {
	if nonInteractive || !confirm.StdinIsTTY() {
		e.Printer.Warning(fmt.Sprintf("Received %s. VM %s left running (non-interactive mode).", sig, vmID))
		return clerrors.Wrap(clerrors.CodeVMProvisionFailed,
			"deployment interrupted",
			fmt.Sprintf("VM %s was created and is still running. Use 'cloudru-vm destroy --force' to delete it.", vmID),
			context.Canceled)
	}

	fmt.Fprintf(e.Printer.ErrWriter(), "\nReceived %s. Delete the VM %s? [y/N] ", sig, vmID)
	var answer string
	fmt.Fscanln(confirm.Stdin, &answer)

	if answer == "y" || answer == "Y" {
		e.Printer.StepDone("Deleting VM...")
		deleteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := e.Cloud.DeleteVM(deleteCtx, vmID); err != nil {
			e.Printer.Warning(fmt.Sprintf("Failed to delete VM: %v", err))
		} else {
			e.Printer.StepDone("VM deleted")
		}
	} else {
		e.Printer.Warning(fmt.Sprintf("VM %s left running.", vmID))
	}

	return clerrors.Wrap(clerrors.CodeVMProvisionFailed,
		"deployment interrupted by signal",
		fmt.Sprintf("VM %s was created. Use 'cloudru-vm destroy' to clean up if needed.", vmID),
		context.Canceled)
}

// printVerifyResults outputs health check results.
func (e *Engine) printVerifyResults(results []verify.ProbeResult, ip string) {
	allHealthy := true
	for _, r := range results {
		if r.Healthy {
			e.Printer.StepDone(fmt.Sprintf(":%d -> %d (%s)", r.Port, r.StatusCode, r.Latency.Round(time.Millisecond)))
		} else {
			allHealthy = false
			msg := fmt.Sprintf(":%d -> UNHEALTHY", r.Port)
			if r.Error != "" {
				msg += fmt.Sprintf(" (%s)", r.Error)
			}
			e.Printer.Warning(msg)
		}
	}
	if !allHealthy {
		e.Printer.Hint("Some health checks failed. Services may still be starting. Re-check with: curl http://" + ip + ":<port>/")
	}
}
