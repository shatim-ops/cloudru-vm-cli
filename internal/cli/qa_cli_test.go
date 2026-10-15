package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// commandPaths lists every command of the CLI as its argument path
// ("vm", "get"), groups included.
func commandPaths() [][]string {
	var out [][]string
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			p := append(append([]string{}, path...), sub.Name())
			out = append(out, p)
			walk(sub, p)
		}
	}
	walk(newRootCmd(), nil)
	return out
}

// takesPositionals reports whether a command accepts positional arguments
// at all (tree leaves with <id>, tools call/describe, configure delete).
func takesPositionals(path []string) bool {
	cmd, _, err := newRootCmd().Find(path)
	if err != nil {
		return false
	}
	return strings.Contains(cmd.Use, "<") || strings.Contains(cmd.Use, "[")
}

// assertJSONLines fails unless every non-empty line of s is a JSON value.
func assertJSONLines(t *testing.T, what, s string) {
	t.Helper()
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && !json.Valid([]byte(line)) {
			t.Errorf("%s: not JSON: %q", what, line)
		}
	}
}

// Лишние позиционные аргументы и опечатка в подкоманде — ошибка
// использования (код 1), а не молча проигнорированный аргумент или справка
// с кодом 0: скрипт с «evo vm lst» иначе решит, что всё хорошо.
func TestRun_EveryCommandRejectsStrayArguments(t *testing.T) {
	setupCLI(t)
	paths := commandPaths()
	if len(paths) < 66 {
		t.Fatalf("only %d commands found", len(paths))
	}
	for _, path := range paths {
		if takesPositionals(path) {
			continue
		}
		args := append(append([]string{}, path...), "qa-stray-arg")
		code, stdout, stderr := runCLI(t, "", args...)
		if code != clerrors.ExitGeneral {
			t.Errorf("%v: exit %d, want 1\nstdout: %.200s\nstderr: %.200s", args, code, stdout, stderr)
			continue
		}
		if !strings.Contains(stderr, "qa-stray-arg") {
			t.Errorf("%v: the stray argument is not named: %q", args, stderr)
		}
	}
}

// Каждая команда отвечает на --help кодом 0 и текстом в stdout.
func TestRun_EveryCommandHelp(t *testing.T) {
	setupCLI(t)
	for _, path := range commandPaths() {
		code, stdout, _ := runCLI(t, "", append(path, "--help")...)
		if code != 0 || !strings.Contains(stdout, "Usage:") {
			t.Errorf("%v --help: exit %d, stdout %.100q", path, code, stdout)
		}
	}
}

// Опечатка в подкоманде группы подсказывает правильное имя.
func TestRun_UnknownSubcommandSuggests(t *testing.T) {
	setupCLI(t)
	code, _, stderr := runCLI(t, "", "vm", "lst")
	if code != clerrors.ExitGeneral || !strings.Contains(stderr, `unknown command "lst"`) || !strings.Contains(stderr, "list") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	// Без аргументов группа по-прежнему показывает справку.
	if code, stdout, _ := runCLI(t, "", "vm"); code != 0 || !strings.Contains(stdout, "Available Commands") {
		t.Errorf("vm: exit %d, %q", code, stdout)
	}
}

// С --json ошибка использования — JSON в stderr, где бы ни стоял флаг:
// cobra не успевает разобрать --json, если ошибка раньше него.
func TestRun_UsageErrorIsJSONWhereverTheFlagIs(t *testing.T) {
	setupCLI(t)
	for _, args := range [][]string{
		{"nosuchcmd", "--json"},
		{"--nosuchflag", "--json"},
		{"tools", "list", "--nosuchflag", "--json"},
		{"vm", "lst", "--json"},
		{"version", "extra", "--json"},
		{"--json", "nosuchcmd"},
	} {
		code, stdout, stderr := runCLI(t, "", args...)
		if code != clerrors.ExitGeneral {
			t.Errorf("%v: exit %d", args, code)
		}
		if stdout != "" {
			t.Errorf("%v: stdout %q, want empty", args, stdout)
		}
		var e struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(stderr), &e); err != nil || e.Error.Code != 1 || e.Error.Message == "" {
			t.Errorf("%v: stderr is not a JSON error: %q", args, stderr)
		}
	}
	// После "--" это уже не флаг CLI.
	if _, _, stderr := runCLI(t, "", "nosuchcmd", "--", "--json"); json.Valid([]byte(stderr)) {
		t.Errorf("--json after -- must not switch to JSON: %q", stderr)
	}
}

// С --json каждая команда печатает в stdout ровно один JSON-документ (или
// ничего), а каждая строка stderr — JSON. Раньше status/verify/destroy без
// развёртывания отдавали пустой stdout (jq получал пустой вход), destroy
// при успехе тоже молчал, предупреждения шли в stderr текстом.
func TestRun_JSONModeKeepsBothStreamsParseable(t *testing.T) {
	setupCLI(t)
	// A warning on every command (credentials file readable by others),
	// and no access key: nothing reaches the cloud.
	if err := config.WriteFilePrivate(config.CredentialsPath(), []byte("profiles:\n  default:\n    s3: {region: x}\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config.CredentialsPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range commandPaths() {
		if path[0] == "mcp" {
			continue
		}
		newProcess()
		code, stdout, stderr := runCLI(t, "", append(path, "--json")...)
		if strings.Contains(stdout, "Usage:") {
			continue // a group shows its help
		}
		if s := strings.TrimSpace(stdout); s != "" && !json.Valid([]byte(s)) {
			t.Errorf("%v (exit %d): stdout is not one JSON document: %.300q", path, code, stdout)
		}
		assertJSONLines(t, strings.Join(path, " ")+" stderr", stderr)
	}
	for _, cmd := range []string{"status", "verify", "destroy"} {
		newProcess()
		code, stdout, _ := runCLI(t, "", cmd, "--json")
		if code != 0 || !json.Valid([]byte(stdout)) {
			t.Errorf("%s --json without a deployment: exit %d, stdout %q", cmd, code, stdout)
		}
	}
}

// Сообщение для листа без позиционных аргументов не должно быть «want , got 1».
func TestTrees_NoPositionalsMessage(t *testing.T) {
	setupCLI(t)
	_, _, stderr := runCLI(t, "", "vm", "list", "extra")
	if strings.Contains(stderr, "want ,") || !strings.Contains(stderr, "no arguments") {
		t.Errorf("stderr %q", stderr)
	}
}
