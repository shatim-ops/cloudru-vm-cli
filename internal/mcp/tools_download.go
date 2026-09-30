package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shatim-ops/cloudru-vm-cli/internal/deploy"
)

// vm_download brings files back from a VM.
//
// It exists for one reason: the factory's vulnerability scan runs ON the build
// VM, writes its reports there, and the cleanup step then wipes the machine
// and powers it off. Without a way to pull the reports first, the scan
// produced nothing that outlived the VM — the build report said "scanner ran"
// and that was the whole record. The ЦКЗ rules require the opposite: every
// verdict rests on a stored scan result, and re-scans compare against it.
//
// Shape follows vm_upload in reverse: transfer over the existing SSH session
// as base64, no SFTP subsystem needed, and a hard size cap because Exec holds
// the whole output in memory and gives up after 120 seconds.
const (
	downloadDefaultMaxBytes = 32 << 20
	downloadHardMaxBytes    = 128 << 20
)

// handleVMDownload reads files off a VM and writes them under out_dir on the
// engine's own machine. Read-only on the VM side, so no confirmation gate;
// the local write goes only into the directory the caller named.
func (s *Server) handleVMDownload(ctx context.Context, args map[string]any) (string, bool) {
	paths := stringSliceArg(args, "paths")
	if len(paths) == 0 {
		return errResult(fmt.Errorf("'paths' is required: files on the VM to bring back")), true
	}
	outDir := stringArg(args, "out_dir")
	if outDir == "" {
		return errResult(fmt.Errorf("'out_dir' is required: a local directory to write into")), true
	}
	for i, p := range paths {
		if err := checkRemotePath(p); err != nil {
			return errResult(fmt.Errorf("paths[%d]: %w", i, err)), true
		}
	}
	maxBytes := intArg(args, "max_bytes", downloadDefaultMaxBytes)
	if maxBytes <= 0 || maxBytes > downloadHardMaxBytes {
		return errResult(fmt.Errorf("max_bytes must be within 1..%d", downloadHardMaxBytes)), true
	}
	st, err := os.Stat(outDir)
	if err != nil || !st.IsDir() {
		return errResult(fmt.Errorf("out_dir %q is not an existing directory", outDir)), true
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

	type got struct {
		Remote string `json:"remote"`
		Local  string `json:"local"`
		Bytes  int    `json:"bytes"`
	}
	var files []got
	for _, p := range paths {
		data, err := downloadOne(ctx, d, p, maxBytes)
		if err != nil {
			return errResult(fmt.Errorf("%s: %w", p, err)), true
		}
		// Only the base name survives: the caller chose out_dir, and a remote
		// path must not be able to steer the write anywhere else.
		local := localNameFor(outDir, p)
		if err := os.WriteFile(local, data, 0o644); err != nil {
			return errResult(fmt.Errorf("writing %s: %w", local, err)), true
		}
		files = append(files, got{Remote: p, Local: local, Bytes: len(data)})
	}
	return toJSON(map[string]any{"status": "downloaded", "vm_id": target.VMID, "files": files}), false
}

// checkRemotePath rejects anything that is not a plain file path. Paths reach
// the VM through a shell; quoting handles the rest, but a path with a newline
// or a traversal component is not a request anyone meant to make.
func checkRemotePath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("empty path")
	case strings.ContainsAny(p, "\n\r\x00"):
		return fmt.Errorf("path %q contains a control character", p)
	case strings.HasSuffix(p, "/"), path.Base(p) == ".", path.Base(p) == "..":
		return fmt.Errorf("path %q does not name a file", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("path %q contains '..'", p)
		}
	}
	return nil
}

// downloadOne fetches a single file as base64 over the SSH session.
//
// Two round trips on purpose. The size is checked BEFORE the content is
// requested: asking for a multi-gigabyte file and then discovering it is too
// big would already have blown the session's memory and its 120-second cap.
// sudo is used because the reports the factory needs live under /var/lib and
// belong to root; the build login has passwordless sudo on a build VM.
func downloadOne(ctx context.Context, d *deploy.SSHDeployer, remote string, maxBytes int) ([]byte, error) {
	q := shellQuoteMCP(remote)
	out, err := d.Exec(ctx, "sudo test -f "+q+" && sudo stat -c %s "+q)
	if err != nil {
		return nil, fmt.Errorf("not a readable file on the VM: %s", strings.TrimSpace(out))
	}
	size, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return nil, fmt.Errorf("unexpected stat output %q", strings.TrimSpace(out))
	}
	if size > maxBytes {
		return nil, fmt.Errorf("file is %d bytes, above the %d-byte limit (raise max_bytes deliberately)", size, maxBytes)
	}
	out, err = d.Exec(ctx, "sudo base64 -w0 < "+q)
	if err != nil {
		return nil, fmt.Errorf("reading: %s", strings.TrimSpace(out))
	}
	return decodeDownload(out, size)
}

// decodeDownload turns the base64 body back into bytes and checks it against
// the size reported a moment earlier. A mismatch means the file changed
// between the two calls or the transfer was cut — either way not the file the
// caller asked for, and a truncated scan report must not pass for a full one.
func decodeDownload(body string, wantSize int) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body))
	if err != nil {
		return nil, fmt.Errorf("transfer corrupted: %w", err)
	}
	if len(data) != wantSize {
		return nil, fmt.Errorf("size mismatch: expected %d bytes, received %d (file changed or transfer cut)", wantSize, len(data))
	}
	return data, nil
}

// localNameFor places a downloaded file: out_dir chosen by the caller, base
// name from the VM, nothing in between.
func localNameFor(outDir, remote string) string {
	return filepath.Join(outDir, path.Base(remote))
}
