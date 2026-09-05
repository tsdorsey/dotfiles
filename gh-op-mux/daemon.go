package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func runDaemon() int {
	if err := daemonMain(); err != nil {
		if sock, _ := socketPath(); sock != "" {
			_ = os.WriteFile(sock+".err", []byte(err.Error()+"\n"), 0o600)
		}
		return 1
	}
	return 0
}

func daemonMain() error {
	sock, err := socketPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return err
	}
	_ = os.Remove(sock)
	slave, drainDone, err := ownPTY()
	if err != nil {
		return fmt.Errorf("pty: %w", err)
	}
	defer slave.Close()
	defer func() { <-drainDone }()

	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		ln.Close()
		return err
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go serveConn(conn)
	}
}

func ownPTY() (*os.File, <-chan struct{}, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	fd := int(slave.Fd())
	if err := unix.IoctlSetInt(fd, unix.TIOCSCTTY, 0); err != nil {
		master.Close()
		slave.Close()
		return nil, nil, err
	}
	if ti, err := unix.IoctlGetTermios(fd, unix.TIOCGETA); err == nil {
		ti.Lflag &^= unix.TOSTOP
		_ = unix.IoctlSetTermios(fd, unix.TIOCSETA, ti)
	}
	unix.CloseOnExec(fd)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer master.Close()
		buf := make([]byte, 4096)
		for {
			if _, err := master.Read(buf); err != nil {
				return
			}
		}
	}()
	return slave, done, nil
}

type frameConn struct {
	mu   sync.Mutex
	conn net.Conn
}

func (f *frameConn) write(typ uint8, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return writeFrame(f.conn, typ, payload)
}

func (f *frameConn) read() (uint8, []byte, error) {
	return readFrame(f.conn)
}

func serveConn(raw net.Conn) {
	conn := &frameConn{conn: raw}
	defer raw.Close()

	typ, payload, err := conn.read()
	if err != nil {
		return
	}
	if typ != typRequest {
		_ = conn.write(typError, mustJSON(errorPayload{Error: "gh-op-mux: expected request"}))
		return
	}
	var req request
	if err := json.Unmarshal(payload, &req); err != nil {
		_ = conn.write(typError, mustJSON(errorPayload{Error: "gh-op-mux: bad request"}))
		return
	}
	if reason := deniedReason(req.Argv); reason != "" {
		_ = conn.write(typError, mustJSON(errorPayload{Error: reason}))
		return
	}

	ghPath, err := resolveGH()
	if err != nil {
		_ = conn.write(typError, mustJSON(errorPayload{Error: err.Error()}))
		return
	}
	opPath, err := resolveOP()
	if err != nil {
		_ = conn.write(typError, mustJSON(errorPayload{Error: err.Error()}))
		return
	}

	args := append([]string{"plugin", "run", "--", ghPath}, req.Argv...)
	cmd := exec.Command(opPath, args...)
	cmd.Dir = req.Cwd
	cmd.Env = childEnv(req.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		_ = conn.write(typError, mustJSON(errorPayload{Error: "gh-op-mux: pipe"}))
		return
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		_ = conn.write(typError, mustJSON(errorPayload{Error: "gh-op-mux: pipe"}))
		return
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		stdoutR.Close()
		stdoutW.Close()
		_ = conn.write(typError, mustJSON(errorPayload{Error: "gh-op-mux: pipe"}))
		return
	}
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		stdinR.Close()
		stdinW.Close()
		stdoutR.Close()
		stdoutW.Close()
		stderrR.Close()
		stderrW.Close()
		_ = conn.write(typError, mustJSON(errorPayload{Error: "gh-op-mux: " + err.Error()}))
		return
	}
	stdinR.Close()
	stdoutW.Close()
	stderrW.Close()
	_ = syscall.Kill(cmd.Process.Pid, syscall.SIGCONT)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		copyOut(conn, typStdout, stdoutR)
	}()
	go func() {
		defer wg.Done()
		copyOut(conn, typStderr, stderrR)
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpClient(conn, stdinW, cmd)
	}()

	waitErr := cmd.Wait()
	stdinW.Close()
	wg.Wait()

	code := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			_ = conn.write(typError, mustJSON(errorPayload{Error: "gh-op-mux: " + waitErr.Error()}))
			return
		}
	}
	_ = conn.write(typExit, mustJSON(exitPayload{Code: code}))
	<-done
}

func copyOut(conn *frameConn, typ uint8, r *os.File) {
	defer r.Close()
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if werr := conn.write(typ, buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func pumpClient(conn *frameConn, stdinW *os.File, cmd *exec.Cmd) {
	defer stdinW.Close()
	for {
		typ, payload, err := conn.read()
		if err != nil {
			killGroup(cmd)
			return
		}
		switch typ {
		case typStdin:
			if _, err := stdinW.Write(payload); err != nil {
				killGroup(cmd)
				return
			}
		case typStdinEOF:
			_ = stdinW.Close()
		case typSignal:
			sig := syscall.SIGINT
			if len(payload) > 0 && payload[0] == byte(unix.SIGTERM) {
				sig = syscall.SIGTERM
			}
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, sig)
			}
		}
	}
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	time.AfterFunc(200*time.Millisecond, func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	})
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"error":"gh-op-mux: encode"}`)
	}
	return b
}
