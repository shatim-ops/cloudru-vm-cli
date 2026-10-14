package deploy

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

	// keepAliveInterval is how often the connection is probed; after
	// keepAliveMissed unanswered probes it is closed. Zero means 15 s.
	keepAliveInterval time.Duration

	// newStreamSession opens the session ExecStream runs in; nil means a real
	// session on client. Tests substitute a fake.
	newStreamSession func(stdout, stderr io.Writer) (streamSession, error)
}

// streamSession is the part of *ssh.Session that ExecStream uses.
type streamSession interface {
	Start(cmd string) error
	Wait() error
	Signal(sig ssh.Signal) error
	Close() error
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
		every := d.keepAliveInterval
		if every <= 0 {
			every = defaultKeepAlive
		}
		go keepAlive(res.client, every)
		return nil
	}
}

const (
	defaultKeepAlive = 15 * time.Second
	keepAliveMissed  = 3
)

// keepAlive probes the connection the way OpenSSH's ServerAliveInterval
// does and closes it when the server stops answering. Without it a link that
// dies without a RST (NAT timeout, lost Wi-Fi) left ExecStream blocked in
// Wait for hours during `docker compose up` or `logs -f`. Ends when the
// client is closed.
func keepAlive(c *ssh.Client, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	missed := 0
	for range t.C {
		reply := make(chan error, 1)
		go func() {
			_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
			reply <- err
		}()
		select {
		case err := <-reply:
			if err != nil {
				return // closed
			}
			missed = 0
		case <-time.After(every):
			missed++
			if missed >= keepAliveMissed {
				_ = c.Close()
				return
			}
		}
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
	return d.exec(ctx, cmd, nil)
}

// exec is Exec with stdin for the remote command (nil: none).
func (d *SSHDeployer) exec(ctx context.Context, cmd string, stdin []byte) (string, error) {
	if d.client == nil {
		return "", clerrors.New(clerrors.CodeSSHTimeout, "not connected", "Call Connect before Exec.")
	}

	session, err := d.client.NewSession()
	if err != nil {
		return "", clerrors.Wrap(clerrors.CodeSSHTimeout, "failed to create SSH session", "The SSH connection may have been lost.", err)
	}
	defer session.Close()
	if stdin != nil {
		session.Stdin = bytes.NewReader(stdin)
	}

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

// ExecStream runs cmd on the remote host, copying its stdout and stderr to
// the given writers as output arrives. Unlike Exec it has no timeout of its
// own: it runs until the command exits or ctx is cancelled (e.g. `logs -f`
// until Ctrl-C). On cancellation the remote command gets SIGTERM, the session
// is closed and the returned error wraps ctx.Err().
func (d *SSHDeployer) ExecStream(ctx context.Context, cmd string, stdout, stderr io.Writer) error {
	open := d.newStreamSession
	if open == nil {
		if d.client == nil {
			return clerrors.New(clerrors.CodeSSHTimeout, "not connected", "Call Connect before ExecStream.")
		}
		open = d.openStreamSession
	}

	session, err := open(stdout, stderr)
	if err != nil {
		return clerrors.Wrap(clerrors.CodeSSHTimeout, "failed to create SSH session", "The SSH connection may have been lost.", err)
	}
	defer session.Close()

	if err := session.Start(cmd); err != nil {
		return clerrors.Wrap(clerrors.CodeSSHTimeout, "failed to start remote command", "Check the remote host state.", err)
	}

	done := make(chan error, 1)
	go func() { done <- session.Wait() }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		_ = session.Close() // unblocks Wait; without a pty the server may ignore the signal
		<-done
		return clerrors.Wrap(clerrors.CodeSSHTimeout, "command execution cancelled", "The operation was cancelled during command execution.", ctx.Err())
	case err := <-done:
		if err != nil {
			return clerrors.Wrap(clerrors.CodeSSHTimeout, "command execution failed", "Check the command and remote host state.", err)
		}
		return nil
	}
}

func (d *SSHDeployer) openStreamSession(stdout, stderr io.Writer) (streamSession, error) {
	session, err := d.client.NewSession()
	if err != nil {
		return nil, err
	}
	session.Stdout = stdout
	session.Stderr = stderr
	return session, nil
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

// uploadFile writes one file on the remote host. The content goes on stdin,
// not in the command: a command argument is capped by the remote execve
// (128 KiB per argument on Linux) and by the SSH packet size, and is visible
// to every user in ps — compose files carry passwords in environment:.
func (d *SSHDeployer) uploadFile(ctx context.Context, f FileEntry) error {
	dir := parentDir(f.RemotePath)
	cmd := fmt.Sprintf("mkdir -p %s && cat > %s", shellQuote(dir), shellQuote(f.RemotePath))

	_, err := d.exec(ctx, cmd, f.Content)
	if err != nil {
		return clerrors.Wrap(clerrors.CodeSSHTimeout, fmt.Sprintf("failed to upload %s", f.RemotePath), "Check remote path permissions and disk space.", err)
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

	// A host key mismatch is a CLIError from our callback, wrapped by
	// "ssh: handshake failed: %w": check it before the message patterns.
	var cliErr *clerrors.CLIError
	if ok := errorAs(err, &cliErr); ok && cliErr.Code == clerrors.CodeSSHHostKeyChanged {
		return cliErr
	}

	if isTimeoutError(err) {
		return clerrors.Wrap(clerrors.CodeSSHTimeout, "SSH connection timed out", "Check that the host is reachable and the port is open.", err)
	}

	// Only a rejected key is an auth failure. Any other "handshake failed"
	// (EOF, connection reset from an sshd that is still starting) is
	// retryable: deploy stops retrying on SSH_AUTH_FAILED.
	if strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "no supported methods remain") {
		return clerrors.Wrap(clerrors.CodeSSHAuthFailed, "SSH authentication failed", "Check your SSH key and username.", err)
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

// ShellQuote quotes s as one word for the remote POSIX shell.
func ShellQuote(s string) string { return shellQuote(s) }

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
