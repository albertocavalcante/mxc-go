package mxc

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func within(t *testing.T, d time.Duration, name string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s: hung for %v", name, d)
	}
}

// Close must not deadlock behind a Write blocked on a full stdin pipe.
func TestHardening_CloseWhileStdinWriteBlocked(t *testing.T) {
	needSandbox(t)
	p, err := Spawn(context.Background(), sh("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = p.Stdin().Write(make([]byte, 8<<20)) }() // child never reads
	time.Sleep(300 * time.Millisecond)
	within(t, 10*time.Second, "Close", func() { _ = p.Close() })
}

// Spawn's ctx should bind the process lifetime (like exec.CommandContext).
func TestHardening_SpawnContextKillsProcess(t *testing.T) {
	needSandbox(t)
	ctx, cancel := context.WithCancel(context.Background())
	p, err := Spawn(ctx, sh("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	cancel()
	within(t, 8*time.Second, "read after cancel", func() { _, _ = io.Copy(io.Discard, p.Stdout()) })
}

func TestHardening_SpawnTimeoutReported(t *testing.T) {
	needSandbox(t)
	req := sh("sleep 30")
	req.Process.Timeout = 400 * time.Millisecond
	p, err := Spawn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	res, err := p.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("spawn path did not report timeout: %+v", res)
	}
}

func TestHardening_NULInRawJSONRejected(t *testing.T) {
	_, err := RunJSON(context.Background(), "{\"version\":\"1.0.0\",\x00\"process\":{\"commandLine\":\"echo hi\"}}")
	if err == nil {
		t.Fatal("NUL byte silently accepted/truncated")
	}
}

func TestHardening_UseAfterClose(t *testing.T) {
	needSandbox(t)
	p, _ := Spawn(context.Background(), sh("echo x"))
	_ = p.Close()
	_ = p.Close()
	if _, err := p.Wait(context.Background()); err == nil {
		t.Fatal("Wait after Close should error")
	}
	_ = p.Kill()
	_ = p.Resize(1, 1)
	_ = p.Warnings()
	_ = p.ID()
	if p.Stdout() != nil {
		if n, err := p.Stdout().Read(make([]byte, 4)); n != 0 || err != io.EOF {
			t.Fatalf("read after close: %d %v", n, err)
		}
	}
	if _, err := p.Stdin().Write([]byte("x")); err == nil {
		t.Fatal("stdin write after close should error")
	}
}

func TestHardening_ResizeNonPTY(t *testing.T) {
	needSandbox(t)
	p, _ := Spawn(context.Background(), sh("sleep 1"))
	defer p.Close()
	if err := p.Resize(10, 10); err == nil {
		t.Fatal("resize of non-PTY must error")
	}
	if err := p.Resize(0, 0); err == nil {
		t.Fatal("zero size must error")
	}
}

func TestHardening_ParallelRunsAndFDLeak(t *testing.T) {
	needSandbox(t)
	before := fdCount()
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				if r, err := Run(context.Background(), sh("echo ok")); err != nil || string(r.Stdout) != "ok\n" {
					t.Errorf("run: %v %v", r, err)
					return
				}
				p, err := Spawn(context.Background(), sh("echo ok"))
				if err != nil {
					t.Errorf("spawn: %v", err)
					return
				}
				_, _ = p.Output(context.Background())
				_ = p.Close()
			}
		}()
	}
	wg.Wait()
	time.Sleep(300 * time.Millisecond)
	if after := fdCount(); after > before+8 {
		t.Fatalf("fd leak: before=%d after=%d", before, after)
	}
}

func fdCount() int {
	e, _ := os.ReadDir("/dev/fd")
	return len(e)
}

func TestHardening_Env(t *testing.T) {
	needSandbox(t)
	r := sh("env | grep -c .; echo FOO=$FOO")
	r.Process.Env = []string{"FOO=bar"}
	res, err := Run(context.Background(), r)
	if err != nil || !strings.Contains(string(res.Stdout), "FOO=bar") {
		t.Fatalf("custom env: %q %v", res.Stdout, err)
	}
	r2 := sh("echo FOO=$FOO HOME=$HOME")
	r2.Process.Env = []string{}
	res, err = Run(context.Background(), r2)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("explicit-empty env output: %q", res.Stdout)
	if strings.Contains(string(res.Stdout), "HOME=/") {
		t.Errorf("explicit empty env still leaked HOME: %q", res.Stdout)
	}
}

func TestHardening_OutputEdgeCases(t *testing.T) {
	needSandbox(t)
	res, err := Run(context.Background(), sh(`printf 'a\000b\377c\n'; exit 300`))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("binary stdout=%q exit=%d", res.Stdout, res.ExitCode)
	res, err = Run(context.Background(), sh(`kill -9 $$`))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("sigkill exit=%d timedOut=%v", res.ExitCode, res.TimedOut)
	res, err = Run(context.Background(), sh(`echo 'héllo "wörld" 日本語'`))
	if err != nil || strings.TrimSpace(string(res.Stdout)) != `héllo "wörld" 日本語` {
		t.Fatalf("unicode: %q %v", res.Stdout, err)
	}
}

func TestHardening_LargeOutput(t *testing.T) {
	needSandbox(t)
	res, err := Run(context.Background(), sh(`head -c 50000000 /dev/zero | tr '\0' 'x'`))
	if err != nil || len(res.Stdout) != 50000000 {
		t.Fatalf("large output: len=%d err=%v", len(res.Stdout), err)
	}
}

func TestHardening_NetworkDeny(t *testing.T) {
	needSandbox(t)
	r := sh("curl -s -m 5 -o /dev/null -w '%{http_code}' https://example.com; echo rc=$?")
	r.Network = &Network{Egress: &NetworkEgress{Default: Deny}}
	res, err := Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("network deny: %q", res.Stdout)
	if strings.Contains(string(res.Stdout), "200") {
		t.Fatal("network egress was NOT blocked")
	}
}

func TestHardening_Telemetry(t *testing.T) {
	c, err := TelemetryConsent()
	if err != nil || c == "" {
		t.Fatalf("consent: %q %v", c, err)
	}
	p, err := TelemetryPolicy()
	if err != nil || p == "" {
		t.Fatalf("policy: %q %v", p, err)
	}
	st, err := TelemetryConsentStatus()
	if err != nil || !json.Valid(st) {
		t.Fatalf("status must be JSON: %q %v", st, err)
	}
	w, err := TelemetryWithdrawConsent()
	if err != nil || !json.Valid(w) {
		t.Fatalf("withdraw must be JSON: %q %v", w, err)
	}
}

func TestHardening_StreamsPreserveRawBytes(t *testing.T) {
	needSandbox(t)
	p, err := Spawn(context.Background(), sh(`printf 'a\000b\377c'`))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	out, _ := io.ReadAll(p.Stdout())
	if string(out) != "a\x00b\xffc" {
		t.Fatalf("stream bytes not preserved: %q", out)
	}
}

func TestHardening_GoroutineLeak(t *testing.T) {
	needSandbox(t)
	for range 20 {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		_, _ = Run(ctx, sh("sleep 5"))
		cancel()
	}
}

func TestHardening_RequestConsentNotApplicableOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows may prompt")
	}
	called := false
	out, err := RequestConsent("en-US", func(ConsentPrompt) (ConsentDecision, error) {
		called = true
		return ConsentNo, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("presenter must not run on a host where consent is not applicable")
	}
	if !json.Valid(out) || !strings.Contains(string(out), "notApplicable") {
		t.Fatalf("outcome=%s", out)
	}
	if needs, err := TelemetryNeedsConsentPrompt(); err != nil || needs {
		t.Fatalf("needs=%v err=%v", needs, err)
	}
	if _, err := RequestConsent("", nil); err == nil {
		t.Fatal("nil presenter must error")
	}
}
