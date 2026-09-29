package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	record := os.Getenv("GH_OP_MUX_RECORD")
	if record != "" {
		_ = os.MkdirAll(record, 0o700)
		_ = os.WriteFile(filepath.Join(record, "gh-cwd"), []byte(mustGetwd()+"\n"), 0o600)
		_ = os.WriteFile(filepath.Join(record, "gh-argv"), []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o600)
		_ = os.WriteFile(filepath.Join(record, "gh-pid"), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600)
	}

	if n := os.Getenv("GH_FAKE_SLEEP_MS"); n != "" {
		ms, _ := strconv.Atoi(n)
		if ms > 0 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
	}

	if os.Getenv("GH_FAKE_ECHO_STDIN") == "1" {
		b, _ := io.ReadAll(os.Stdin)
		_, _ = os.Stdout.Write(b)
	}

	if msg := os.Getenv("GH_FAKE_STDOUT"); msg != "" {
		_, _ = os.Stdout.WriteString(msg)
	}
	if msg := os.Getenv("GH_FAKE_STDERR"); msg != "" {
		_, _ = os.Stderr.WriteString(msg)
	}

	code := 0
	if n := os.Getenv("GH_FAKE_EXIT"); n != "" {
		code, _ = strconv.Atoi(n)
	}
	os.Exit(code)
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}
