package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// Mode determines the output format.
type Mode int

const (
	ModeText  Mode = iota // default TTY
	ModePlain             // no TTY / piped
	ModeJSON              // --json flag
)

// Printer handles all output formatting.
type Printer struct {
	out    io.Writer
	errOut io.Writer
	mode   Mode
	color  bool
}

// New creates a Printer. Detects mode from flags and TTY state.
func New(jsonMode bool, noColor bool) *Printer {
	mode := ModeText
	if jsonMode {
		mode = ModeJSON
	} else if !IsTTY(int(os.Stdout.Fd())) {
		mode = ModePlain
	}
	return &Printer{
		out:    os.Stdout,
		errOut: os.Stderr,
		mode:   mode,
		color:  ColorEnabled(noColor) && mode == ModeText,
	}
}

// NewWithWriter creates a Printer writing to a specific writer (for testing).
func NewWithWriter(w io.Writer, mode Mode, color bool) *Printer {
	return &Printer{
		out:    w,
		errOut: w,
		mode:   mode,
		color:  color,
	}
}

// Step prints a step indicator: "==> [1/5] Message..."
func (p *Printer) Step(n, total int, msg string) {
	if p.mode == ModeJSON {
		return
	}
	prefix := fmt.Sprintf("==> [%d/%d] %s", n, total, msg)
	if p.color {
		fmt.Fprintf(p.out, "%s%s%s%s\n", colorBold, colorBlue, prefix, colorReset)
	} else {
		fmt.Fprintln(p.out, prefix)
	}
}

// StepDone prints step completion: "    detail"
func (p *Printer) StepDone(detail string) {
	if p.mode == ModeJSON {
		return
	}
	if p.color {
		fmt.Fprintf(p.out, "    %s%s%s\n", colorDim, detail, colorReset)
	} else {
		fmt.Fprintf(p.out, "    %s\n", detail)
	}
}

// Success prints a success message in green.
func (p *Printer) Success(msg string) {
	if p.mode == ModeJSON {
		return
	}
	if p.color {
		fmt.Fprintf(p.out, "%s%s%s\n", colorGreen, msg, colorReset)
	} else {
		fmt.Fprintln(p.out, msg)
	}
}

// Warning prints a warning message in yellow.
func (p *Printer) Warning(msg string) {
	if p.mode == ModeJSON {
		return
	}
	if p.color {
		fmt.Fprintf(p.errOut, "%s%sWarning: %s%s\n", colorBold, colorYellow, msg, colorReset)
	} else {
		fmt.Fprintf(p.errOut, "Warning: %s\n", msg)
	}
}

// Errorf prints an error message in red.
func (p *Printer) Errorf(msg string, args ...any) {
	formatted := fmt.Sprintf(msg, args...)
	if p.color {
		fmt.Fprintf(p.errOut, "%s%sError: %s%s\n", colorBold, colorRed, formatted, colorReset)
	} else {
		fmt.Fprintf(p.errOut, "Error: %s\n", formatted)
	}
}

// Hint prints a hint/suggestion in dim text.
func (p *Printer) Hint(msg string) {
	if p.mode == ModeJSON {
		return
	}
	if p.color {
		fmt.Fprintf(p.errOut, "%s%s%s\n", colorDim, msg, colorReset)
	} else {
		fmt.Fprintln(p.errOut, msg)
	}
}

// Table prints data as an aligned table.
func (p *Printer) Table(headers []string, rows [][]string) {
	if p.mode == ModeJSON {
		return
	}
	tw := tabwriter.NewWriter(p.out, 0, 4, 2, ' ', 0)
	if p.color {
		colored := make([]string, len(headers))
		for i, h := range headers {
			colored[i] = colorBold + h + colorReset
		}
		fmt.Fprintln(tw, strings.Join(colored, "\t"))
	} else {
		fmt.Fprintln(tw, strings.Join(headers, "\t"))
	}
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	tw.Flush()
}

// JSON outputs v as JSON to stdout.
func (p *Printer) JSON(v any) error {
	enc := json.NewEncoder(p.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// NDJSON outputs v as a single NDJSON line.
func (p *Printer) NDJSON(v any) error {
	enc := json.NewEncoder(p.out)
	return enc.Encode(v)
}

// IsJSON returns true if in JSON output mode.
func (p *Printer) IsJSON() bool {
	return p.mode == ModeJSON
}

// Writer returns the underlying output writer.
func (p *Printer) Writer() io.Writer {
	return p.out
}
