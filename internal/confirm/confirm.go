// Package confirm is the single consent model for operations that create
// paid resources or delete data.
//
// It mirrors the MCP contract (status "confirmation_required", then a second
// call with confirmed=true): the CLI shows the same preview and either asks a
// human at a terminal, accepts --yes, or refuses with exit code 3. It never
// treats "nobody could answer" as consent, and never as success.
package confirm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

// Stdin is where answers are read from; StdinIsTTY tells whether a human can
// answer at all. Variables so tests can replace them.
var (
	Stdin      io.Reader = os.Stdin
	StdinIsTTY           = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

// ErrConfirmationRequired matches (via errors.Is) every error Ask returns when
// consent was not given: no terminal to ask, --non-interactive, or a refusal
// at the prompt. Its exit code is 3.
var ErrConfirmationRequired = clerrors.New(clerrors.CodeConfirmationRequired, "confirmation required", "")

// Preview describes what an operation is about to do.
type Preview struct {
	// Action is the operation name, e.g. "deploy" or "destroy".
	Action string `json:"action"`
	// Summary is one line, phrased as what will happen:
	// "create VM web (lowcost10-2-4) in ru.AZ-1".
	Summary string `json:"summary"`
	// Lines are the human-readable details, one per line.
	Lines []string `json:"-"`
	// Details is the machine-readable form of the same details (for --json).
	Details any `json:"details,omitempty"`
}

// Options control how Ask obtains consent.
type Options struct {
	Yes      bool // --yes: consent given in advance
	DryRun   bool // --dry-run: show the preview, do nothing
	NoPrompt bool // never ask, even on a terminal (--non-interactive)
	JSON     bool // --json: machine-readable output

	Out io.Writer // command result (stdout)
	Err io.Writer // previews and prompts (stderr)

	In    io.Reader   // nil means Stdin
	IsTTY func() bool // nil means StdinIsTTY
}

// envelope is the JSON shape shared with the MCP tools.
type envelope struct {
	Status  string  `json:"status"`
	Preview Preview `json:"preview"`
	Hint    string  `json:"hint,omitempty"`
}

const hintYes = "Re-run with --yes to confirm, or with --dry-run to only see the preview."

// Ask shows the preview and reports whether the operation may proceed.
//
//   - DryRun: the preview goes to Out; returns (false, nil) — the caller stops
//     and exits 0.
//   - Yes: the preview goes to Err as one line; returns (true, nil).
//   - no terminal (or NoPrompt): the preview goes to Err (with --json, a
//     {"status":"confirmation_required","preview":{…}} object goes to Out);
//     returns an error matching ErrConfirmationRequired (exit code 3).
//   - terminal: the preview goes to Err followed by "[y/N]"; "y"/"yes" returns
//     (true, nil), anything else an ErrConfirmationRequired error. A cancelled
//     ctx (Ctrl-C at the prompt) returns ctx.Err().
func Ask(ctx context.Context, p Preview, opts Options) (bool, error) {
	out, errOut := opts.Out, opts.Err
	if out == nil {
		out = io.Discard
	}
	if errOut == nil {
		errOut = io.Discard
	}

	if opts.DryRun {
		if opts.JSON {
			writeJSON(out, envelope{Status: "dry_run", Preview: p}, true)
		} else {
			fmt.Fprint(out, render(p, "Dry run: would "))
		}
		return false, nil
	}

	if opts.Yes {
		if opts.JSON {
			writeJSON(errOut, envelope{Status: "confirmed", Preview: p}, false)
		} else {
			fmt.Fprintf(errOut, "Confirmed: %s\n", p.Summary)
		}
		return true, nil
	}

	isTTY := opts.IsTTY
	if isTTY == nil {
		isTTY = StdinIsTTY
	}
	if opts.NoPrompt || !isTTY() {
		why := "stdin is not a terminal"
		if opts.NoPrompt {
			why = "--non-interactive is set"
		}
		if opts.JSON {
			writeJSON(out, envelope{Status: "confirmation_required", Preview: p, Hint: hintYes}, true)
		} else {
			fmt.Fprint(errOut, render(p, "About to "))
		}
		return false, clerrors.New(clerrors.CodeConfirmationRequired,
			fmt.Sprintf("refusing to %s: confirmation required and %s", p.Action, why),
			hintYes)
	}

	fmt.Fprint(errOut, render(p, "About to "))
	fmt.Fprint(errOut, "Proceed? [y/N] ")
	in := opts.In
	if in == nil {
		in = Stdin
	}
	answer, err := readAnswer(ctx, in)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true, nil
	}
	return false, clerrors.New(clerrors.CodeConfirmationRequired,
		fmt.Sprintf("%s cancelled: confirmation not given", p.Action),
		"Answer 'y' at the prompt, or re-run with --yes.")
}

// render formats the preview as a heading line and indented details.
func render(p Preview, lead string) string {
	var b strings.Builder
	b.WriteString(lead)
	b.WriteString(p.Summary)
	b.WriteString(":\n")
	for _, l := range p.Lines {
		b.WriteString("  ")
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

func writeJSON(w io.Writer, v any, indent bool) {
	enc := json.NewEncoder(w)
	if indent {
		enc.SetIndent("", "  ")
	}
	_ = enc.Encode(v)
}

// ReadLine reads one trimmed line from Stdin, giving up when ctx is
// cancelled. For prompts that are a choice, not consent (e.g. init picking a
// zone); consent always goes through Ask.
func ReadLine(ctx context.Context) (string, error) {
	return readAnswer(ctx, Stdin)
}

// readAnswer reads one line from r, giving up when ctx is cancelled
// (Ctrl-C at the prompt).
func readAnswer(ctx context.Context, r io.Reader) (string, error) {
	ch := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(r).ReadString('\n')
		ch <- strings.TrimSpace(line)
	}()
	select {
	case answer := <-ch:
		return answer, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
