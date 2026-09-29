package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type request struct {
	Argv []string          `json:"argv"`
	Cwd  string            `json:"cwd"`
	Env  map[string]string `json:"env"`
}

type exitPayload struct {
	Code int `json:"code"`
}

type errorPayload struct {
	Error string `json:"error"`
}

func runClient(argv []string) int {
	if reason := deniedReason(argv); reason != "" {
		fmt.Fprintln(os.Stderr, reason)
		return 2
	}

	conn, err := ensureMux()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer conn.Close()

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gh-op-mux:", err)
		return 1
	}
	payload, err := json.Marshal(request{Argv: argv, Cwd: cwd, Env: collectClientEnv()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gh-op-mux:", err)
		return 1
	}
	if err := writeFrame(conn, typRequest, payload); err != nil {
		fmt.Fprintln(os.Stderr, "gh-op-mux:", err)
		return 1
	}

	var writeMu sync.Mutex
	write := func(typ uint8, payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return writeFrame(conn, typ, payload)
	}

	go func() {
		_ = copyStdin(write)
	}()

	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		for sig := range sigCh {
			n := unix.SIGINT
			if sig == syscall.SIGTERM {
				n = unix.SIGTERM
			}
			_ = write(typSignal, []byte{byte(n)})
		}
	}()

	code, runErr := readClientResult(conn)
	_ = conn.Close()
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		if code == 0 {
			return 1
		}
	}
	return code
}

func copyStdin(write func(uint8, []byte) error) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			if werr := write(typStdin, buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			return write(typStdinEOF, nil)
		}
		if err != nil {
			return err
		}
	}
}

func readClientResult(conn net.Conn) (int, error) {
	for {
		typ, payload, err := readFrame(conn)
		if err != nil {
			if err == io.EOF {
				return 1, fmt.Errorf("gh-op-mux: multiplexer closed the connection")
			}
			return 1, fmt.Errorf("gh-op-mux: %w", err)
		}
		switch typ {
		case typStdout:
			if _, err := os.Stdout.Write(payload); err != nil {
				return 1, err
			}
		case typStderr:
			if _, err := os.Stderr.Write(payload); err != nil {
				return 1, err
			}
		case typExit:
			var ex exitPayload
			if err := json.Unmarshal(payload, &ex); err != nil {
				return 1, fmt.Errorf("gh-op-mux: bad exit frame")
			}
			return ex.Code, nil
		case typError:
			var ep errorPayload
			if err := json.Unmarshal(payload, &ep); err != nil {
				return 1, fmt.Errorf("gh-op-mux: %s", payload)
			}
			return 1, fmt.Errorf("%s", ep.Error)
		}
	}
}

func ensureMux() (net.Conn, error) {
	sock, err := socketPath()
	if err != nil {
		return nil, fmt.Errorf("gh-op-mux: %w", err)
	}
	if c, err := net.Dial("unix", sock); err == nil {
		return c, nil
	}

	lockFile, err := acquireLock()
	if err != nil {
		return nil, fmt.Errorf("gh-op-mux: %w", err)
	}
	defer lockFile.Close()

	if c, err := net.Dial("unix", sock); err == nil {
		return c, nil
	}
	_ = os.Remove(sock)

	if err := startDaemon(); err != nil {
		return nil, fmt.Errorf("gh-op-mux: %w", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", sock); err == nil {
			return c, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, fmt.Errorf("gh-op-mux: could not start or reach the multiplexer")
}

func acquireLock() (*os.File, error) {
	p, err := lockPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func startDaemon() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self)
	cmd.Env = daemonEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func daemonEnv() []string {
	keep := []string{"PATH", "HOME", "USER", "TMPDIR", envGH, envOP, envSock}
	var env []string
	for _, k := range keep {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	env = append(env, envDaemon+"=1")
	return env
}
