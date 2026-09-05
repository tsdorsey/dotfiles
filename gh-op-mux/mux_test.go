package main_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	muxBin string
	opBin  string
	ghBin  string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gh-op-mux-bins-")
	if err != nil {
		panic(err)
	}
	muxBin = filepath.Join(dir, "gh")
	opBin = filepath.Join(dir, "op")
	ghBin = filepath.Join(dir, "real-gh")
	if err := build("./", muxBin); err != nil {
		panic(err)
	}
	if err := build("./testdata/fakeop", opBin); err != nil {
		panic(err)
	}
	if err := build("./testdata/fakegh", ghBin); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func build(pkg, out string) error {
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type harness struct {
	t      *testing.T
	dir    string
	record string
	sock   string
	env    []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	// macOS unix socket paths cap at 104 bytes; t.TempDir() is often longer.
	sockDir, err := os.MkdirTemp("/tmp", "gom")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	h := &harness{
		t:      t,
		dir:    dir,
		record: filepath.Join(dir, "record"),
		sock:   filepath.Join(sockDir, "s"),
	}
	if err := os.MkdirAll(h.record, 0o700); err != nil {
		t.Fatal(err)
	}
	h.env = []string{
		"PATH=" + dir + ":/usr/bin:/bin",
		"HOME=" + dir,
		"USER=testharness",
		"TMPDIR=" + dir,
		"GH_OP_MUX_GH=" + ghBin,
		"GH_OP_MUX_OP=" + opBin,
		"GH_OP_MUX_SOCK=" + h.sock,
		"GH_OP_MUX_RECORD=" + h.record,
	}
	return h
}

func (h *harness) run(args ...string) *runResult {
	return h.runWith(nil, nil, args...)
}

func (h *harness) runWith(extraEnv []string, stdin io.Reader, args ...string) *runResult {
	h.t.Helper()
	cmd := exec.Command(muxBin, args...)
	cmd.Env = append(append([]string{}, h.env...), extraEnv...)
	cmd.Dir = h.dir
	if stdin == nil {
		stdin = bytes.NewReader(nil)
	}
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			h.t.Fatalf("run: %v", err)
		}
	}
	return &runResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

type runResult struct {
	code   int
	stdout string
	stderr string
}

func (h *harness) read(name string) string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.record, name))
	if err != nil {
		h.t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func (h *harness) exists(name string) bool {
	_, err := os.Stat(filepath.Join(h.record, name))
	return err == nil
}

func TestFailClosedDoesNotExecOp(t *testing.T) {
	h := newHarness(t)
	// Socket path whose parent cannot exist as a directory.
	blocker := filepath.Join(h.dir, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.sock = filepath.Join(blocker, "mux.sock")
	h.env = replaceEnv(h.env, "GH_OP_MUX_SOCK="+h.sock)

	res := h.run("pr", "list")
	if res.code == 0 {
		t.Fatalf("expected failure, got success stdout=%q stderr=%q", res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "gh-op-mux") {
		t.Fatalf("expected mux error, stderr=%q", res.stderr)
	}
	if h.exists("op-argv-lines") {
		t.Fatal("op was invoked despite fail-closed")
	}
}

func TestRequestBecomesOpPluginRunWithCwd(t *testing.T) {
	h := newHarness(t)
	work := filepath.Join(h.dir, "repo")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}

	h.dir = work
	res := h.run("pr", "view", "12")
	if res.code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", res.code, res.stdout, res.stderr)
	}

	argv := h.read("op-argv-lines")
	wantPrefix := "plugin\nrun\n--\n" + ghBin + "\npr\nview\n12\n"
	if argv != wantPrefix {
		t.Fatalf("op argv:\n got %q\nwant %q", argv, wantPrefix)
	}
	cwd := strings.TrimSpace(h.read("op-cwd"))
	want, err := filepath.EvalSymlinks(work)
	if err != nil {
		want = work
	}
	got, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		got = cwd
	}
	if got != want {
		t.Fatalf("cwd=%q want %q", got, want)
	}
}

func TestAllowlistAndStrip(t *testing.T) {
	h := newHarness(t)
	res := h.runWith([]string{
		"GIT_DIR=" + h.dir + "/.git",
		"GH_REPO=owner/repo",
		"GH_TOKEN=secret-token",
		"GITHUB_TOKEN=other-secret",
		"GH_ENTERPRISE_TOKEN=ent-secret",
		"GITHUB_ENTERPRISE_TOKEN=ent2-secret",
		"OP_SESSION=session",
		"OP_SERVICE_ACCOUNT_TOKEN=sat",
		"SECRET_LEAK=should-not-pass",
	}, nil, "api", "user")
	if res.code != 0 {
		t.Fatalf("code=%d stderr=%q", res.code, res.stderr)
	}
	env := h.read("op-env")
	mustHave := []string{"GIT_DIR=" + h.dir + "/.git", "GH_REPO=owner/repo", "HOME=" + h.dir}
	for _, line := range mustHave {
		if !strings.Contains(env, line) {
			t.Fatalf("missing %q in env:\n%s", line, env)
		}
	}
	mustNot := []string{
		"GH_TOKEN=", "GITHUB_TOKEN=", "GH_ENTERPRISE_TOKEN=",
		"GITHUB_ENTERPRISE_TOKEN=", "OP_SESSION=", "OP_SERVICE_ACCOUNT_TOKEN=",
		"SECRET_LEAK=",
	}
	for _, line := range mustNot {
		if strings.Contains(env, line) {
			t.Fatalf("stripped name still present %q in env:\n%s", line, env)
		}
	}
}

func TestDeniedAuthNeverCallsOp(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"login", []string{"auth", "login"}},
		{"refresh", []string{"auth", "refresh"}},
		{"token", []string{"auth", "token"}},
		{"show-token", []string{"auth", "status", "--show-token"}},
		{"show-token-eq", []string{"auth", "status", "--show-token=true"}},
		{"global-token", []string{"-R", "owner/repo", "auth", "token"}},
		{"hostname-token", []string{"--hostname", "github.com", "auth", "token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			res := h.run(tc.args...)
			if res.code == 0 {
				t.Fatalf("expected deny, stderr=%q", res.stderr)
			}
			if !strings.Contains(res.stderr, "gh-op-mux:") {
				t.Fatalf("expected one-line mux reason, stderr=%q", res.stderr)
			}
			if h.exists("op-argv-lines") {
				t.Fatal("op was invoked for a denied command")
			}
		})
	}
}

func TestAuthStatusWithoutShowTokenRuns(t *testing.T) {
	h := newHarness(t)
	res := h.runWith([]string{"GH_FAKE_STDOUT=ok-status"}, nil, "auth", "status")
	if res.code != 0 {
		t.Fatalf("code=%d stderr=%q", res.code, res.stderr)
	}
	if !h.exists("op-argv-lines") {
		t.Fatal("expected op to run for auth status")
	}
	if res.stdout != "ok-status" {
		t.Fatalf("stdout=%q", res.stdout)
	}
}

func TestClientReturnsWhenChildExitsDespiteOpenStdin(t *testing.T) {
	h := newHarness(t)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pr.Close()
		pw.Close()
	})
	done := make(chan *runResult, 1)
	go func() {
		done <- h.runWith([]string{"GH_FAKE_STDOUT=done"}, pr, "pr", "list")
	}()
	select {
	case res := <-done:
		if res.code != 0 {
			t.Fatalf("code=%d stderr=%q", res.code, res.stderr)
		}
		if res.stdout != "done" {
			t.Fatalf("stdout=%q", res.stdout)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client hung waiting for stdin after the child exited")
	}
}

func TestStdioAndExitCode(t *testing.T) {
	h := newHarness(t)
	res := h.runWith([]string{
		"GH_FAKE_ECHO_STDIN=1",
		"GH_FAKE_STDERR=err-bit",
		"GH_FAKE_EXIT=9",
	}, strings.NewReader("hello-stdin"), "api", "--input", "-")
	if res.code != 9 {
		t.Fatalf("code=%d want 9 stderr=%q", res.code, res.stderr)
	}
	if res.stdout != "hello-stdin" {
		t.Fatalf("stdout=%q", res.stdout)
	}
	if res.stderr != "err-bit" {
		t.Fatalf("stderr=%q", res.stderr)
	}
}

func TestTwoClientsOneMux(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	errc := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := filepath.Join(h.dir, "rec", itoa(i))
			_ = os.MkdirAll(rec, 0o700)
			res := h.runWith([]string{
				"GH_OP_MUX_RECORD=" + rec,
				"GH_FAKE_STDOUT=ok" + itoa(i),
			}, nil, "pr", "list")
			if res.code != 0 {
				errc <- res.stderr
				return
			}
			if res.stdout != "ok"+itoa(i) {
				errc <- "stdout " + res.stdout
			}
		}(i)
	}
	wg.Wait()
	close(errc)
	for e := range errc {
		t.Fatal(e)
	}
	pids := muxPIDs(t, h.sock)
	if len(pids) != 1 {
		t.Fatalf("mux pids=%v want 1", pids)
	}
}

func TestColdStartRace(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	errc := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := h.run("issue", "list")
			if res.code != 0 {
				errc <- res.stderr
			}
		}()
	}
	wg.Wait()
	close(errc)
	for e := range errc {
		t.Fatal(e)
	}
	pids := muxPIDs(t, h.sock)
	if len(pids) != 1 {
		t.Fatalf("mux pids=%v want 1", pids)
	}
}

func TestSharedControllingTTY(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	recs := []string{filepath.Join(h.dir, "a"), filepath.Join(h.dir, "b")}
	for _, rec := range recs {
		wg.Add(1)
		go func(rec string) {
			defer wg.Done()
			_ = os.MkdirAll(rec, 0o700)
			res := h.runWith([]string{"GH_OP_MUX_RECORD=" + rec}, nil, "pr", "list")
			if res.code != 0 {
				t.Errorf("stderr=%q", res.stderr)
			}
		}(rec)
	}
	wg.Wait()

	ttyA := strings.TrimSpace(mustRead(t, filepath.Join(recs[0], "op-ctty")))
	ttyB := strings.TrimSpace(mustRead(t, filepath.Join(recs[1], "op-ctty")))
	if ttyA == "" || ttyB == "" || ttyA == "??" || ttyB == "??" {
		t.Fatalf("missing ctty a=%q b=%q", ttyA, ttyB)
	}
	if ttyA != ttyB {
		t.Fatalf("ctty mismatch a=%q b=%q", ttyA, ttyB)
	}
}

func TestSecondRequestReusesMux(t *testing.T) {
	h := newHarness(t)
	if res := h.run("pr", "list"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	first := muxPIDs(t, h.sock)
	if len(first) != 1 {
		t.Fatalf("first pids=%v", first)
	}
	if res := h.run("issue", "list"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	second := muxPIDs(t, h.sock)
	if len(second) != 1 || second[0] != first[0] {
		t.Fatalf("mux restarted: first=%v second=%v", first, second)
	}
}

func TestDisconnectKillsChildNotMux(t *testing.T) {
	h := newHarness(t)
	cmd := exec.Command(muxBin, "api", "slow")
	cmd.Env = append(append([]string{}, h.env...), "GH_FAKE_SLEEP_MS=8000")
	cmd.Dir = h.dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var childPID string
	for time.Now().Before(deadline) {
		if h.exists("gh-pid") {
			childPID = strings.TrimSpace(h.read("gh-pid"))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPID == "" {
		_ = cmd.Process.Kill()
		t.Fatal("child never started")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	dead := false
	for i := 0; i < 40; i++ {
		if !processAlive(childPID) {
			dead = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !dead {
		t.Fatalf("child %s still alive after client disconnect", childPID)
	}
	res := h.run("pr", "list")
	if res.code != 0 {
		t.Fatalf("mux died with the client: %s", res.stderr)
	}
}

func TestSIGINTKillsChildNotMux(t *testing.T) {
	h := newHarness(t)
	cmd := exec.Command(muxBin, "api", "slow")
	cmd.Env = append(append([]string{}, h.env...), "GH_FAKE_SLEEP_MS=8000")
	cmd.Dir = h.dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var childPID string
	for time.Now().Before(deadline) {
		if h.exists("gh-pid") {
			childPID = strings.TrimSpace(h.read("gh-pid"))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPID == "" {
		_ = cmd.Process.Kill()
		t.Fatal("child never started")
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("client did not exit after SIGINT")
	}
	dead := false
	for i := 0; i < 40; i++ {
		if !processAlive(childPID) {
			dead = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !dead {
		t.Fatalf("child %s still alive after client SIGINT", childPID)
	}
	res := h.run("pr", "list")
	if res.code != 0 {
		t.Fatalf("mux died with the client: %s", res.stderr)
	}
}

func TestOpFailureExitCode(t *testing.T) {
	h := newHarness(t)
	res := h.runWith([]string{"GH_FAKE_OP_FAIL=1"}, nil, "pr", "list")
	if res.code != 17 {
		t.Fatalf("code=%d want 17 stderr=%q", res.code, res.stderr)
	}
}

func muxPIDs(t *testing.T, sock string) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var out []byte
	var err error
	for time.Now().Before(deadline) {
		cmd := exec.Command("lsof", "-t", sock)
		out, err = cmd.Output()
		if err == nil && len(bytes.TrimSpace(out)) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		t.Fatalf("lsof %s: %v %q", sock, err, out)
	}
	var pids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			pids = append(pids, line)
		}
	}
	return unique(pids)
}

func processAlive(pid string) bool {
	cmd := exec.Command("kill", "-0", pid)
	return cmd.Run() == nil
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func replaceEnv(env []string, kv string) []string {
	key, _, _ := strings.Cut(kv, "=")
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return append(out, kv)
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func itoa(i int) string {
	return []string{"0", "1", "2", "3"}[i]
}
