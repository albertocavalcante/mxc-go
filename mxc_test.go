package mxc

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func needSandbox(t *testing.T) {
	t.Helper()
	ps, err := PlatformSupportInfo()
	if err != nil || !ps.IsSupported {
		t.Skipf("no supported sandbox backend: %+v %v", ps, err)
	}
}

func sh(cmd string) Request {
	return Request{
		Process:    Workload{CommandLine: cmd, Timeout: 20 * time.Second},
		Filesystem: &Filesystem{ReadwritePaths: []string{os.TempDir()}},
	}
}

func TestRequestJSON(t *testing.T) {
	r := Request{
		Containment: ContainmentSeatbelt,
		Process:     Workload{CommandLine: "true", Env: []string{}, Timeout: 1500 * time.Millisecond},
		Network:     &Network{Egress: &NetworkEgress{Default: Deny}},
		UI:          &UI{Disable: Bool(false)},
		Extra:       map[string]json.RawMessage{"lifecycle": json.RawMessage(`{"destroyOnExit":true}`)},
	}
	js, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	if m["version"] != "1.0.0" || m["containment"] != "seatbelt" {
		t.Fatalf("bad header: %s", js)
	}
	p := m["process"].(map[string]any)
	if env, ok := p["env"].([]any); !ok || len(env) != 0 {
		t.Fatalf("explicit empty env must be preserved: %s", js)
	}
	if p["timeout"].(float64) != 1500 {
		t.Fatalf("timeout ms: %s", js)
	}
	if m["ui"].(map[string]any)["disable"] != false {
		t.Fatalf("explicit false must be preserved: %s", js)
	}
	if _, ok := m["lifecycle"]; !ok {
		t.Fatalf("extra not merged: %s", js)
	}
	// nil Env must be omitted
	js2, _ := Request{Process: Workload{CommandLine: "x"}}.JSON()
	if strings.Contains(js2, `"env"`) {
		t.Fatalf("nil env must be omitted: %s", js2)
	}
}

func TestNativeAndDiscovery(t *testing.T) {
	if v := NativeVersion(); v == "" {
		t.Fatal("empty native version")
	}
	if _, err := PlatformSupportInfo(); err != nil {
		t.Fatal(err)
	}
	if _, err := AvailableBackends(); err != nil {
		t.Fatal(err)
	}
}

func TestRunCapture(t *testing.T) {
	needSandbox(t)
	res, err := Run(context.Background(), sh("echo out; echo err 1>&2; exit 3"))
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || res.TimedOut {
		t.Fatalf("exit=%d timedOut=%v", res.ExitCode, res.TimedOut)
	}
	if string(res.Stdout) != "out\n" || string(res.Stderr) != "err\n" {
		t.Fatalf("stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}
}

func TestRunEnforcesFilesystemPolicy(t *testing.T) {
	needSandbox(t)
	if runtime.GOOS != "darwin" {
		t.Skip("path is darwin specific")
	}
	home, _ := os.UserHomeDir()
	target := home + "/mxc_go_should_fail"
	defer os.Remove(target)
	res, err := Run(context.Background(), sh("touch "+target+" 2>/dev/null; echo rc=$?"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(res.Stdout)) == "rc=0" {
		t.Fatal("write outside policy unexpectedly succeeded")
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatal("file was created outside sandbox policy")
	}
}

func TestRunTimeout(t *testing.T) {
	needSandbox(t)
	req := sh("sleep 30")
	req.Process.Timeout = 500 * time.Millisecond
	start := time.Now()
	res, err := Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || time.Since(start) > 10*time.Second {
		t.Fatalf("timedOut=%v after %v", res.TimedOut, time.Since(start))
	}
}

func TestRunErrors(t *testing.T) {
	_, err := RunJSON(context.Background(), `{"version":"9.9.9","process":{"commandLine":"x"}}`)
	if !HasCode(err, CodeMalformedRequest) {
		t.Fatalf("want malformed_request, got %v", err)
	}
	_, err = RunJSON(context.Background(), `not json`)
	if err == nil {
		t.Fatal("want error")
	}
	// Same error through the spawn path.
	_, err = SpawnJSON(context.Background(), `{"version":"9.9.9"}`)
	if !HasCode(err, CodeMalformedRequest) {
		t.Fatalf("spawn: want malformed_request, got %v", err)
	}
}

func TestSpawnStreamingStdin(t *testing.T) {
	needSandbox(t)
	p, err := Spawn(context.Background(), sh("cat"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := io.WriteString(p.Stdin(), "hello\nworld\n"); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(p.Stdout())
	var lines []string
	for len(lines) < 2 && sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if strings.Join(lines, ",") != "hello,world" {
		t.Fatalf("lines=%v", lines)
	}
	p.Stdin().Close() // EOF -> cat exits
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := p.Wait(ctx)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestOutputConcurrentDrain(t *testing.T) {
	needSandbox(t)
	// Enough output on both pipes to deadlock a sequential reader.
	p, err := Spawn(context.Background(), sh("yes out | head -c 300000; yes err | head -c 300000 1>&2"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	res, err := p.Output(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stdout) != 300000 || len(res.Stderr) != 300000 {
		t.Fatalf("stdout=%d stderr=%d", len(res.Stdout), len(res.Stderr))
	}
}

func TestContextCancelKills(t *testing.T) {
	needSandbox(t)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, sh("sleep 30"))
	if err != context.DeadlineExceeded {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatalf("cancel took %v", time.Since(start))
	}
}

func TestCloseUnblocksRead(t *testing.T) {
	needSandbox(t)
	p, err := Spawn(context.Background(), sh("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		io.Copy(io.Discard, p.Stdout())
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	p.Close() // kills tree + unblocks reader; must not hang or crash
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("reader still blocked after Close")
	}
}

func TestPTY(t *testing.T) {
	needSandbox(t)
	p, err := SpawnPTY(context.Background(), sh("[ -t 0 ] && [ -t 1 ] && echo istty; stty size; read x; stty size"), WithPTYSize(30, 100))
	if err != nil {
		t.Skipf("pty unsupported here: %v", err)
	}
	defer p.Close()
	br := bufio.NewReader(p.Stdout())
	var got []string
	for len(got) < 2 {
		l, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v (got %q)", err, got)
		}
		got = append(got, strings.TrimSpace(l))
	}
	if got[0] != "istty" || got[1] != "30 100" {
		t.Fatalf("pty output=%q", got)
	}
	if err := p.Resize(40, 120); err != nil {
		t.Fatal(err)
	}
	io.WriteString(p.Stdin(), "\n")
	rest, _ := io.ReadAll(br)
	if !strings.Contains(string(rest), "40 120") {
		t.Fatalf("resize not observed: %q", rest)
	}
}

func TestLifecycleUnavailableOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	_, err := Provision(context.Background(), ProvisionRequest{Containment: ContainmentWSLC})
	var e *Error
	if err == nil {
		t.Fatal("expected error on non-windows host")
	}
	if !asErr(err, &e) {
		t.Fatalf("want *Error, got %T %v", err, err)
	}
	t.Logf("provision error: %v", e)
	if err := Start(context.Background(), "wslc:0123456789abcdef0123456789abcdef"); err == nil {
		t.Fatal("expected error")
	}
}

func asErr(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

func TestRunCancellableCtxClosesStdin(t *testing.T) {
	needSandbox(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	res, err := Run(ctx, sh("cat; echo done"))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Run hung %v (stdin not closed)", time.Since(start))
	}
	if !strings.Contains(string(res.Stdout), "done") {
		t.Fatalf("stdout=%q", res.Stdout)
	}
}

func TestCloseWithBlockedStdinWrite(t *testing.T) {
	needSandbox(t)
	p, err := Spawn(context.Background(), sh("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = p.Stdin().Write(make([]byte, 10<<20)) }()
	time.Sleep(300 * time.Millisecond)
	done := make(chan struct{})
	go func() { _ = p.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close hung behind blocked stdin Write")
	}
}

func TestOutputBoundedByGrandchild(t *testing.T) {
	needSandbox(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	res, err := Run(ctx, sh("(sleep 6 &); echo hi"))
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("Run took %v waiting for grandchild", d)
	}
	if !strings.Contains(string(res.Stdout), "hi") {
		t.Fatalf("stdout=%q", res.Stdout)
	}
}

func TestWaitReturnsCopy(t *testing.T) {
	needSandbox(t)
	p, err := Spawn(context.Background(), sh("true"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	a, _ := p.Wait(context.Background())
	a.ExitCode = 99
	b, _ := p.Wait(context.Background())
	if b.ExitCode == 99 {
		t.Fatal("Wait returned shared pointer")
	}
}

func TestTimeoutRangeChecked(t *testing.T) {
	r := Request{Process: Workload{CommandLine: "true", Timeout: 5000000 * time.Second}}
	if _, err := r.JSON(); err == nil {
		t.Fatal("expected error for timeout > 4294967295ms")
	}
}

func TestLifecycleReservedKeysWin(t *testing.T) {
	js, err := lifecycleEnvelope("provision", "", nil, map[string]json.RawMessage{
		"phase": json.RawMessage(`"evil"`), "version": json.RawMessage(`"0"`)})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(js), &m)
	if m["phase"] != "provision" || m["version"] != SchemaVersion {
		t.Fatalf("reserved keys overridden: %s", js)
	}
}
