package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/output"
)

func TestHandleError_WrappedCLIErrorKeepsHint(t *testing.T) {
	err := fmt.Errorf("list zones: %w", clerrors.New(clerrors.CodeAuthRequired,
		"Cloud.ru credentials not set", "Set CLOUDRU_KEY_ID and CLOUDRU_SECRET"))

	var buf bytes.Buffer
	handleError(output.NewWithWriter(&buf, output.ModePlain, false), err)

	got := buf.String()
	if !strings.Contains(got, "Error: list zones:") || !strings.Contains(got, "Cloud.ru credentials not set") {
		t.Errorf("message lost: %q", got)
	}
	if !strings.Contains(got, "Set CLOUDRU_KEY_ID and CLOUDRU_SECRET") {
		t.Errorf("hint lost through wrapping: %q", got)
	}
}

func TestHandleError_DirectCLIError(t *testing.T) {
	err := clerrors.New(clerrors.CodeVMNotFound, "vm not found", "check the id")

	var buf bytes.Buffer
	handleError(output.NewWithWriter(&buf, output.ModePlain, false), err)

	if want := "Error: vm not found\ncheck the id\n"; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestHandleError_JSON(t *testing.T) {
	err := fmt.Errorf("destroy: %w", clerrors.New(clerrors.CodeConfirmationRequired, "no tty", "use --force"))

	var buf bytes.Buffer
	handleError(output.NewWithWriter(&buf, output.ModeJSON, false), err)

	var got struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	if jerr := json.Unmarshal(buf.Bytes(), &got); jerr != nil {
		t.Fatalf("not JSON: %v: %q", jerr, buf.String())
	}
	if got.Error.Code != clerrors.ExitConfirmationRequired || got.Error.Hint != "use --force" ||
		!strings.Contains(got.Error.Message, "no tty") {
		t.Errorf("unexpected payload: %+v", got.Error)
	}
}

func TestHandleError_InterruptedKeepsHint(t *testing.T) {
	// As run() sees a deploy interrupted after the VM was created.
	inner := clerrors.Wrap(clerrors.CodeVMProvisionFailed, "deployment interrupted",
		"VM vm-1 was created and is still running. Use 'cloudru-vm destroy --force' to delete it.", context.Canceled)
	err := fmt.Errorf("%w: %w", context.Canceled, inner)

	var buf bytes.Buffer
	handleError(output.NewWithWriter(&buf, output.ModeJSON, false), err)

	if !strings.Contains(buf.String(), `"code":130`) || !strings.Contains(buf.String(), "vm-1") ||
		!strings.Contains(buf.String(), "destroy --force") {
		t.Errorf("interrupt must keep the VM hint: %q", buf.String())
	}
}

func TestRun_UsageErrorsExitOne(t *testing.T) {
	for _, args := range [][]string{{"nosuchcmd"}, {"deploy", "--nosuchflag"}} {
		if got := run(args); got != clerrors.ExitGeneral {
			t.Errorf("run(%v) = %d, want %d", args, got, clerrors.ExitGeneral)
		}
	}
}

func TestRun_MissingCredentialsExitTwo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
	t.Setenv("CLOUDRU_API_KEY", "")

	if got := run([]string{"list-zones"}); got != clerrors.ExitAuth {
		t.Errorf("run(list-zones) = %d, want %d", got, clerrors.ExitAuth)
	}
}
