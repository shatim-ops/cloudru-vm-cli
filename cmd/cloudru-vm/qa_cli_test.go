package main

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
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
// с кодом 0: скрипт с «cloudru-vm vm lst» иначе решит, что всё хорошо.
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

// Сообщение для листа без позиционных аргументов не должно быть «want , got 1».
func TestTrees_NoPositionalsMessage(t *testing.T) {
	setupCLI(t)
	_, _, stderr := runCLI(t, "", "vm", "list", "extra")
	if strings.Contains(stderr, "want ,") || !strings.Contains(stderr, "no arguments") {
		t.Errorf("stderr %q", stderr)
	}
}
