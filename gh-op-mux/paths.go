package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	envDaemon = "GH_OP_MUX_DAEMON"
	envGH     = "GH_OP_MUX_GH"
	envOP     = "GH_OP_MUX_OP"
	envSock   = "GH_OP_MUX_SOCK"
)

func socketPath() (string, error) {
	if p := os.Getenv(envSock); p != "" {
		return p, nil
	}
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mux.sock"), nil
}

func lockPath() (string, error) {
	sock, err := socketPath()
	if err != nil {
		return "", err
	}
	return sock + ".lock", nil
}

func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "gh-op-mux")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func resolveGH() (string, error) {
	if p := os.Getenv(envGH); p != "" {
		return p, nil
	}
	for _, p := range []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("gh-op-mux: real gh not found (set %s)", envGH)
}

func resolveOP() (string, error) {
	if p := os.Getenv(envOP); p != "" {
		return p, nil
	}
	p, err := exec.LookPath("op")
	if err != nil {
		return "", fmt.Errorf("gh-op-mux: op not found on PATH (set %s)", envOP)
	}
	return p, nil
}
