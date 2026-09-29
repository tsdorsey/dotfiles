package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	record := os.Getenv("GH_OP_MUX_RECORD")
	if record != "" {
		_ = os.MkdirAll(record, 0o700)
		_ = os.WriteFile(filepath.Join(record, "op-cwd"), []byte(mustGetwd()+"\n"), 0o600)
		_ = os.WriteFile(filepath.Join(record, "op-argv"), []byte(strings.Join(os.Args[1:], "\x00")+"\n"), 0o600)
		_ = os.WriteFile(filepath.Join(record, "op-argv-lines"), []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o600)
		_ = os.WriteFile(filepath.Join(record, "op-env"), []byte(strings.Join(sortedEnviron(), "\n")+"\n"), 0o600)
		_ = os.WriteFile(filepath.Join(record, "op-ctty"), []byte(controllingTTY()+"\n"), 0o600)
		_ = os.WriteFile(filepath.Join(record, "op-pid"), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600)
	}

	if os.Getenv("GH_FAKE_OP_FAIL") == "1" {
		os.Exit(17)
	}

	// Expect: plugin run -- <gh> [args...]
	args := os.Args[1:]
	if len(args) < 3 || args[0] != "plugin" || args[1] != "run" || args[2] != "--" {
		fmt.Fprintf(os.Stderr, "fake-op: expected plugin run -- <cmd>, got %q\n", args)
		os.Exit(2)
	}
	cmd := exec.Command(args[3], args[4:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func sortedEnviron() []string {
	env := os.Environ()
	sort.Strings(env)
	return env
}

func controllingTTY() string {
	out, err := exec.Command("/bin/ps", "-o", "tty=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
