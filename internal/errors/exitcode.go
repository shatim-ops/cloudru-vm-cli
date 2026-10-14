package errors

import (
	"context"
	stderrors "errors"
)

// Process exit codes of the CLI. Scripts rely on them, so the values are
// part of the public contract: never renumber, only add.
const (
	ExitOK                   = 0   // success
	ExitGeneral              = 1   // any other error, including usage errors
	ExitAuth                 = 2   // authentication or configuration problem
	ExitConfirmationRequired = 3   // confirmation not given (or no TTY to ask)
	ExitNotFound             = 4   // requested object does not exist
	ExitAPI                  = 5   // Cloud.ru API returned an error (4xx/5xx other than auth)
	ExitInterrupted          = 130 // interrupted by the user (SIGINT/SIGTERM)
)

// ExitCodesHelp documents the exit codes for the root command's --help.
const ExitCodesHelp = `Exit codes:
  0    success
  1    general error (including unknown command or flag)
  2    authentication or configuration error
  3    confirmation required (not given, or stdin is not a terminal; use --force)
  4    object not found
  5    Cloud.ru API error (HTTP 4xx/5xx other than authentication)
  130  interrupted by the user`

// ExitCode maps an error to the process exit code. It looks through
// wrapping (fmt.Errorf("...: %w")) to find a *CLIError or a context
// cancellation.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	if stderrors.Is(err, context.Canceled) {
		return ExitInterrupted
	}
	var cliErr *CLIError
	if !stderrors.As(err, &cliErr) {
		return ExitGeneral
	}
	switch cliErr.Code {
	case CodeAuthRequired, CodeAuthExpired, CodeAuthFailed, CodeConfigInvalid:
		return ExitAuth
	case CodeConfirmationRequired:
		return ExitConfirmationRequired
	case CodeVMNotFound:
		return ExitNotFound
	case CodeAPIError, CodeAPIRateLimited:
		return ExitAPI
	}
	return ExitGeneral
}
