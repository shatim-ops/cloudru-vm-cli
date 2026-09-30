package deploy

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

// SSHDeployer implements Deployer using real SSH connections.
type SSHDeployer struct {
	client          *ssh.Client
	hostFingerprint string
	connectTimeout  time.Duration // per-attempt timeout (default 10s)
}

// NewSSHDeployer creates a new SSHDeployer with default settings.
func NewSSHDeployer() *SSHDeployer {
	return &SSHDeployer{
		connectTimeout: 10 * time.Second,
	}
}

// Connect establishes an SSH connection to host:port using Ed25519 key authentication.
// If knownHostKey is empty, the server's key is accepted on first use (TOFU).
// If knownHostKey is set, the server's key must match or the connection is rejected.
func (d *SSHDeployer) Connect(ctx context.Context, host string, port int, user string, keyPEM []byte, knownHostKey string) error {
	signer, err := ssh.ParsePrivateKey(keyPEM)
	if err != nil {
		return clerrors.Wrap(clerrors.CodeSSHAuthFailed, "failed to parse SSH private key", "Check that the key is a valid PEM-encoded Ed25519 private key.", err)
	}

	hostKeyCallback := d.buildHostKeyCallback(knownHostKey)

	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         d.connectTimeout,
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	// Use a goroutine so we can respect ctx cancellation during dial.
	type dialResult struct {
		client *ssh.Client
		err    error
	}
	ch := make(chan dialResult, 1)
	go func() {
		c, dialErr := ssh.Dial("tcp", addr, cfg)
		ch <- dialResult{client: c, err: dialErr}
	}()

	select {
	case <-ctx.Done():
		return clerrors.Wrap(clerrors.CodeSSHTimeout, "SSH connection cancelled", "The operation was cancelled before the connection could be established.", ctx.Err())
	case res := <-ch:
		if res.err != nil {
			return classifyDialError(res.err)
		}
		d.client = res.client
		return nil
	}
}

// HostKeyFingerprint returns the SSH host key fingerprint from the current connection.
func (d *SSHDeployer) HostKeyFingerprint() string {
	return d.hostFingerprint
}

// Upload sends files to the remote host by executing shell commands over SSH.
// Each file is written using a heredoc to avoid requiring an SFTP subsystem.
func (d *SSHDeployer) Upload(ctx context.Context, files []FileEntry) error {
	if d.client == nil {
		return clerrors.New(clerrors.CodeSSHTimeout, "not connected", "Call Connect before Upload.")
	}

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return clerrors.Wrap(clerrors.CodeSSHTimeout, "upload cancelled", "The operation was cancelled during file upload.", err)
		}

		if err := d.uploadFile(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

// Exec runs a command on the remote host and returns combined output.
func (d *SSHDeployer) Exec(ctx context.Context, cmd string) (string, error) {
	if d.client == nil {
		return "", clerrors.New(clerrors.CodeSSHTimeout, "not connected", "Call Connect before Exec.")
	}

	session, err := d.client.NewSession()
	if err != nil {
		return "", clerrors.Wrap(clerrors.CodeSSHTimeout, "failed to create SSH session", "The SSH connection may have been lost.", err)
	}
	defer session.Close()

	type execResult struct {
		output []byte
		err    error
	}
	ch := make(chan execResult, 1)
	go func() {
		out, execErr := session.CombinedOutput(cmd)
		ch <- execResult{output: out, err: execErr}
	}()

	execTimeout := 120 * time.Second
	timer := time.NewTimer(execTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		return "", clerrors.Wrap(clerrors.CodeSSHTimeout, "command execution cancelled", "The operation was cancelled during command execution.", ctx.Err())
	case <-timer.C:
		_ = session.Signal(ssh.SIGTERM)
		return "", clerrors.New(clerrors.CodeSSHTimeout, "command execution timed out", fmt.Sprintf("The command did not complete within %s.", execTimeout))
	case res := <-ch:
		if res.err != nil {
			return string(res.output), clerrors.Wrap(clerrors.CodeSSHTimeout, "command execution failed", "Check the command and remote host state.", res.err)
		}
		return string(res.output), nil
	}
}

// Close terminates the SSH connection.
func (d *SSHDeployer) Close() error {
	if d.client != nil {
		err := d.client.Close()
		d.client = nil
		return err
	}
	return nil
}

// buildHostKeyCallback creates an ssh.HostKeyCallback implementing TOFU or strict checking.
func (d *SSHDeployer) buildHostKeyCallback(knownHostKey string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fingerprint := fingerprintKey(key)
		d.hostFingerprint = fingerprint

		if knownHostKey == "" {
			// TOFU: accept and store the fingerprint.
			return nil
		}

		if fingerprint != knownHostKey {
			return clerrors.New(
				clerrors.CodeSSHHostKeyChanged,
				fmt.Sprintf("host key mismatch for %s: expected %s, got %s", hostname, knownHostKey, fingerprint),
				"The remote host key has changed. This could indicate a MITM attack or server reinstall. Update the stored host key if the change is expected.",
			)
		}
		return nil
	}
}

// uploadFile uploads a single file to the remote host using base64 encoding to
// handle arbitrary binary content safely.
// uploadChunk is how much base64 text goes into a single command line.
//
// A file is written by a shell command, not by SFTP, and the whole command
// travels as one string: the remote shell has to parse it, and a multi-megabyte
// line either dies on argument limits or times out mid-flight. 512 KiB keeps
// every command small and predictable; a build distributive of tens of
// megabytes then costs a few dozen appends instead of one impossible write.
// Found on 19.09.2026: uploading a 5.4 MiB part failed with SSH_TIMEOUT, which
// named neither the size nor the line as the cause.
const uploadChunk = 512 * 1024

func (d *SSHDeployer) uploadFile(ctx context.Context, f FileEntry) error {
	// Ensure parent directory exists, then decode base64 content into the target file.
	dir := parentDir(f.RemotePath)
	encoded := base64.StdEncoding.EncodeToString(f.Content)
	target := shellQuote(f.RemotePath)

	if _, err := d.Exec(ctx, fmt.Sprintf("mkdir -p %s", shellQuote(dir))); err != nil {
		return clerrors.Wrap(clerrors.CodeSSHTimeout, fmt.Sprintf("failed to create directory for %s", f.RemotePath), "Check remote path permissions.", err)
	}

	// The first chunk truncates, the rest append: a half-written file from an
	// interrupted previous attempt must not survive into this one.
	redirect := ">"
	tmp := shellQuote(f.RemotePath + ".b64part")
	for start := 0; start < len(encoded) || start == 0; start += uploadChunk {
		end := start + uploadChunk
		if end > len(encoded) {
			end = len(encoded)
		}
		cmd := fmt.Sprintf("printf '%%s' %s %s %s", shellQuote(encoded[start:end]), redirect, tmp)
		if _, err := d.Exec(ctx, cmd); err != nil {
			return clerrors.Wrap(clerrors.CodeSSHTimeout, fmt.Sprintf("failed to upload %s", f.RemotePath), "Check remote path permissions and disk space.", err)
		}
		redirect = ">>"
		if end == len(encoded) {
			break
		}
	}

	// Decoding is a separate step on purpose: a file that failed to assemble
	// must not end up looking like a valid, merely truncated, upload.
	cmd := fmt.Sprintf("base64 -d < %s > %s && rm -f %s", tmp, target, tmp)
	if _, err := d.Exec(ctx, cmd); err != nil {
		return clerrors.Wrap(clerrors.CodeSSHTimeout, fmt.Sprintf("failed to decode uploaded %s", f.RemotePath), "Check remote disk space.", err)
	}
	return nil
}

// fingerprintKey returns a base64-encoded SHA-256 fingerprint of the public key.
func fingerprintKey(key ssh.PublicKey) string {
	return ssh.FingerprintSHA256(key)
}

// classifyDialError converts an SSH dial error into an appropriate CLIError.
func classifyDialError(err error) *clerrors.CLIError {
	msg := err.Error()

	if isTimeoutError(err) {
		return clerrors.Wrap(clerrors.CodeSSHTimeout, "SSH connection timed out", "Check that the host is reachable and the port is open.", err)
	}

	// Order matters: x/crypto/ssh prefixes an authentication failure with
	// "ssh: handshake failed: ..." as well, so the auth-specific phrases must be
	// tested first. A bare handshake failure is a different event — the host
	// accepted the TCP connection but never completed the SSH protocol — and
	// labelling it "authentication failed" sends the operator to check keys and
	// logins while the real cause is an sshd that is not up yet. That exact
	// mislabel cost two wrong diagnoses on the first live image build.
	if strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "no supported methods remain") {
		return clerrors.Wrap(clerrors.CodeSSHAuthFailed, "SSH authentication failed", "Check your SSH key and username.", err)
	}
	if strings.Contains(msg, "handshake failed") {
		return clerrors.Wrap(clerrors.CodeSSHHandshakeFailed, "SSH handshake failed",
			"The host accepted the connection but did not complete the SSH protocol: sshd may not be up yet (the system may still be booting), or something else is listening on the port. Retry later.", err)
	}

	// Check for host key mismatch errors (already a CLIError from our callback).
	var cliErr *clerrors.CLIError
	if ok := errorAs(err, &cliErr); ok && cliErr.Code == clerrors.CodeSSHHostKeyChanged {
		return cliErr
	}

	return clerrors.Wrap(clerrors.CodeSSHTimeout, "SSH connection failed", "Check host, port, and network connectivity.", err)
}

// isTimeoutError checks whether err is a network timeout.
func isTimeoutError(err error) bool {
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

// errorAs is a thin helper wrapping errors.As to avoid importing errors
// (since the package name conflicts with our clerrors alias).
func errorAs(err error, target interface{}) bool {
	// We implement a manual type walk to avoid the import name collision.
	for err != nil {
		if cliErr, ok := err.(*clerrors.CLIError); ok {
			if t, ok2 := target.(**clerrors.CLIError); ok2 {
				*t = cliErr
				return true
			}
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// shellQuote wraps a string in single quotes, escaping internal single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// parentDir returns the parent directory of a path.
func parentDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "/"
	}
	return path[:idx]
}

// parsePrivateKey is an exported helper for testing key parsing.
func parsePrivateKey(keyPEM []byte) (ssh.Signer, error) {
	return ssh.ParsePrivateKey(keyPEM)
}

// CompareHostKey checks whether a given fingerprint matches the expected value.
// Returns a CLIError with CodeSSHHostKeyChanged on mismatch, nil on match.
func CompareHostKey(expected, actual string) error {
	if expected == "" {
		return nil // TOFU
	}
	if expected != actual {
		return clerrors.New(
			clerrors.CodeSSHHostKeyChanged,
			fmt.Sprintf("host key mismatch: expected %s, got %s", expected, actual),
			"The remote host key has changed. Update the stored host key if the change is expected.",
		)
	}
	return nil
}

// Compile-time check that SSHDeployer implements Deployer.
var _ Deployer = (*SSHDeployer)(nil)
