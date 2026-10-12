package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mxc "github.com/albertocavalcante/mxc-go"
)

// These tests drive the real service against the real sandbox with hostile
// snippets. They skip where no sandbox backend is available.

type harness struct {
	t         *testing.T
	cfg       *config
	srv       *httptest.Server
	secretDir string
	secretTok string
}

func newHarness(t *testing.T, mutate func(*config)) *harness {
	t.Helper()
	if ps, err := mxc.PlatformSupportInfo(); err != nil || !ps.IsSupported {
		t.Skip("no sandbox backend on this host")
	}
	secretDir := t.TempDir()
	tok := fmt.Sprintf("TOKEN-%d", rand.Uint64())
	if err := os.WriteFile(filepath.Join(secretDir, "id_rsa"), []byte(tok), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := newConfig(t.TempDir(), []string{secretDir})
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(cfg)
	}
	srv := httptest.NewServer(newHandler(cfg))
	t.Cleanup(srv.Close)
	return &harness{t: t, cfg: cfg, srv: srv, secretDir: secretDir, secretTok: tok}
}

func (h *harness) needLang(lang string) {
	if _, ok := h.cfg.interpreters[lang]; !ok {
		h.t.Skipf("%s not installed on this host", lang)
	}
}

func (h *harness) run(lang, code string, mut ...func(*runRequest)) (int, runResult) {
	h.t.Helper()
	return h.runCtx(context.Background(), lang, code, mut...)
}

func (h *harness) runCtx(ctx context.Context, lang, code string, mut ...func(*runRequest)) (int, runResult) {
	h.t.Helper()
	rq := runRequest{Language: lang, Code: code}
	for _, m := range mut {
		m(&rq)
	}
	body, _ := json.Marshal(rq)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.srv.URL+"/v1/run", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, runResult{}
		}
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var res runResult
	_ = json.NewDecoder(resp.Body).Decode(&res)
	return resp.StatusCode, res
}

// procRunning reports whether any process command line contains marker.
func procRunning(marker string) bool {
	return exec.Command("pgrep", "-f", marker).Run() == nil
}

func waitGone(marker string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !procRunning(marker) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !procRunning(marker)
}

func uniqueSleep() string {
	return fmt.Sprintf("sleep 4%d.%d", 100+rand.IntN(800), rand.IntN(1_000_000))
}

func TestNormalPrograms(t *testing.T) {
	h := newHarness(t, nil)

	t.Run("python computes and reads stdin", func(t *testing.T) {
		h.needLang("python")
		code, res := h.run("python", "import sys\nprint(sum(range(101)), sys.stdin.read().strip().upper())",
			func(r *runRequest) { r.Stdin = "hi" })
		if code != 200 || res.ExitCode != 0 || res.Stdout != "5050 HI\n" {
			t.Fatalf("%d %+v", code, res)
		}
	})
	t.Run("work directory is writable and private", func(t *testing.T) {
		h.needLang("python")
		_, res := h.run("python", "open('data.txt','w').write('persisted')\nprint(open('data.txt').read())")
		if res.Stdout != "persisted\n" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("exit code and stderr propagate", func(t *testing.T) {
		_, res := h.run("sh", "echo oops >&2; exit 7")
		if res.ExitCode != 7 || res.Stderr != "oops\n" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("node", func(t *testing.T) {
		h.needLang("node")
		_, res := h.run("node", "console.log(JSON.stringify([1,2,3].map(x => x * 2)))")
		if res.ExitCode != 0 || strings.TrimSpace(res.Stdout) != "[2,4,6]" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("validation", func(t *testing.T) {
		if code, _ := h.run("cobol", "x"); code != http.StatusBadRequest {
			t.Fatalf("unknown language: %d", code)
		}
		if code, _ := h.run("sh", ""); code != http.StatusBadRequest {
			t.Fatalf("empty code: %d", code)
		}
		if code, _ := h.run("sh", "true", func(r *runRequest) { r.TimeoutMS = 10 * 60 * 1000 }); code != http.StatusBadRequest {
			t.Fatalf("timeout over max: %d", code)
		}
		resp, err := http.Post(h.srv.URL+"/v1/run", "application/json", strings.NewReader(`{"language":"sh","code":"true","sandbox":"off"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("unknown field accepted: %d", resp.StatusCode)
		}
	})
}

func TestHostile(t *testing.T) {
	h := newHarness(t, func(c *config) { c.cpuSeconds = 1 })
	h.needLang("python")

	t.Run("cannot read a denied secret", func(t *testing.T) {
		secret := filepath.Join(h.secretDir, "id_rsa")
		_, res := h.run("python", fmt.Sprintf("print(open(%q).read())", secret))
		if strings.Contains(res.Stdout+res.Stderr, h.secretTok) || res.ExitCode == 0 {
			t.Fatalf("secret reachable: %+v", res)
		}
	})
	t.Run("cannot escape through a symlink", func(t *testing.T) {
		code := fmt.Sprintf("import os\nos.symlink(%q, 'link')\nprint(open('link/id_rsa').read())", h.secretDir)
		_, res := h.run("python", code)
		if strings.Contains(res.Stdout+res.Stderr, h.secretTok) {
			t.Fatalf("symlink escape: %+v", res)
		}
	})
	t.Run("cannot write outside the work directory", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "pwned.txt")
		_, res := h.run("python", fmt.Sprintf("open(%q,'w').write('x')", target))
		if _, err := os.Stat(target); err == nil {
			t.Fatalf("wrote outside the sandbox: %+v", res)
		}
	})
	t.Run("cannot reach the network", func(t *testing.T) {
		// Loopback is blocked by the separate hostLoopback rule; the egress rule is
		// only proven by a target on a non-loopback interface of this host.
		for name, host := range map[string]string{"loopback": "127.0.0.1", "lan": lanAddr(t)} {
			if host == "" {
				t.Logf("no non-loopback address on this host: skipping %s", name)
				continue
			}
			ln, err := net.Listen("tcp", "0.0.0.0:0")
			if err != nil {
				t.Fatal(err)
			}
			var hits atomic.Int32
			go func() {
				for {
					c, err := ln.Accept()
					if err != nil {
						return
					}
					hits.Add(1)
					c.Close()
				}
			}()
			port := ln.Addr().(*net.TCPAddr).Port
			code := fmt.Sprintf("import socket\nsocket.create_connection(('%s',%d),2)\nprint('CONNECTED')", host, port)
			_, res := h.run("python", code)
			time.Sleep(200 * time.Millisecond)
			ln.Close()
			if hits.Load() != 0 || strings.Contains(res.Stdout, "CONNECTED") {
				t.Fatalf("%s: network egress possible: hits=%d %+v", name, hits.Load(), res)
			}
		}
	})
	t.Run("cannot reach an external host", func(t *testing.T) {
		// Local addresses are covered by the host-loopback rule, so the egress
		// rule itself is only proven against a real external destination. The
		// check is meaningful only if this machine can reach it unsandboxed.
		const target = "1.1.1.1:443"
		if c, err := net.DialTimeout("tcp", target, 3*time.Second); err != nil {
			t.Skipf("%s unreachable from the test host (offline?): %v", target, err)
		} else {
			c.Close()
		}
		code := "import socket\nsocket.create_connection(('1.1.1.1',443),3)\nprint('CONNECTED')"
		_, res := h.run("python", code)
		if strings.Contains(res.Stdout, "CONNECTED") {
			t.Fatalf("sandbox reached the internet: %+v", res)
		}
	})
	t.Run("does not inherit the service environment", func(t *testing.T) {
		t.Setenv("CANARY_SECRET", "hunter2-canary")
		// The harness server runs in this process, so the variable is in its env.
		_, res := h.run("python", "import os\nprint(os.environ)")
		if strings.Contains(res.Stdout, "hunter2-canary") {
			t.Fatalf("environment leaked: %+v", res)
		}
	})
	t.Run("infinite loop is stopped by the timeout", func(t *testing.T) {
		start := time.Now()
		_, res := h.run("python", "while True: pass", func(r *runRequest) { r.TimeoutMS = 700 })
		if !res.TimedOut || time.Since(start) > 8*time.Second {
			t.Fatalf("timedOut=%v after %v", res.TimedOut, time.Since(start))
		}
	})
	t.Run("output flood is capped and the producer killed", func(t *testing.T) {
		// Raise the CPU limit so only the output cap, not ulimit, can stop it.
		h.cfg.cpuSeconds = 120
		t.Cleanup(func() { h.cfg.cpuSeconds = 1 })
		start := time.Now()
		_, res := h.run("python", "while True: print('x' * 4096)")
		if !res.Truncated || len(res.Stdout) > h.cfg.outputLimit || time.Since(start) > 8*time.Second {
			t.Fatalf("truncated=%v len=%d after %v", res.Truncated, len(res.Stdout), time.Since(start))
		}
	})
	t.Run("descendants do not survive a timeout", func(t *testing.T) {
		m := uniqueSleep()
		t.Cleanup(func() { _ = exec.Command("pkill", "-f", m).Run() })
		_, res := h.run("sh", fmt.Sprintf("for i in 1 2 3 4 5 6 7 8; do %s & done; wait", m),
			func(r *runRequest) { r.TimeoutMS = 700 })
		if !res.TimedOut {
			t.Fatalf("expected timeout: %+v", res)
		}
		if !waitGone(m, 5*time.Second) {
			t.Fatalf("descendant processes survived: %s", m)
		}
	})
	t.Run("a background child cannot hold the response hostage", func(t *testing.T) {
		m := uniqueSleep()
		t.Cleanup(func() { _ = exec.Command("pkill", "-f", m).Run() })
		start := time.Now()
		// The parent exits at once; the child keeps stdout open for ~70 minutes.
		_, res := h.run("sh", fmt.Sprintf("%s & echo parent-done", m))
		if res.Stdout != "parent-done\n" || time.Since(start) > 8*time.Second {
			t.Fatalf("response delayed %v: %+v", time.Since(start), res)
		}
		if !waitGone(m, 5*time.Second) {
			t.Fatalf("orphaned child survived: %s", m)
		}
	})
	t.Run("client disconnect kills the sandboxed process", func(t *testing.T) {
		m := uniqueSleep()
		t.Cleanup(func() { _ = exec.Command("pkill", "-f", m).Run() })
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(500 * time.Millisecond); cancel() }()
		h.runCtx(ctx, "sh", m)
		if !waitGone(m, 8*time.Second) {
			t.Fatalf("process outlived its client: %s", m)
		}
	})
	t.Run("cpu burn is limited by ulimit", func(t *testing.T) {
		start := time.Now()
		_, res := h.run("python", "while True: pass", func(r *runRequest) { r.TimeoutMS = 20_000 })
		if res.TimedOut || time.Since(start) > 10*time.Second {
			t.Fatalf("CPU limit not enforced (timedOut=%v after %v)", res.TimedOut, time.Since(start))
		}
	})
	t.Run("work directories are cleaned up", func(t *testing.T) {
		h.run("python", "open('junk','w').write('x'*1000)")
		entries, _ := os.ReadDir(h.cfg.workRoot)
		if len(entries) != 0 {
			t.Fatalf("leftover work dirs: %v", entries)
		}
	})
}

func TestBackpressure(t *testing.T) {
	h := newHarness(t, func(c *config) {
		c.slots = make(chan struct{}, 1)
	})
	done := make(chan struct{})
	go func() { h.run("sh", "sleep 1.5"); close(done) }()
	time.Sleep(400 * time.Millisecond)
	code, _ := h.run("sh", "true")
	if code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 while the only slot is busy, got %d", code)
	}
	<-done
	if code, _ := h.run("sh", "true"); code != http.StatusOK {
		t.Fatalf("slot not released: %d", code)
	}
}

func TestStreaming(t *testing.T) {
	h := newHarness(t, nil)
	resp, err := http.Post(h.srv.URL+"/v1/run/stream", "application/json",
		strings.NewReader(`{"language":"sh","code":"echo one; sleep 0.3; echo two"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	got := buf.String()
	for _, want := range []string{"event: stdout", `"data":"one\n"`, `"data":"two\n"`, "event: result", `"exit_code":0`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

// lanAddr returns a non-loopback IPv4 address of this host, or "".
func lanAddr(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil && !ipn.IP.IsLinkLocalUnicast() {
			return ipn.IP.String()
		}
	}
	return ""
}
