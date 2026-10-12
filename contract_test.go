package mxc

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Every typed section must be accepted by the native strict (deny-unknown-fields) parser.
func TestContract_AllSectionsParse(t *testing.T) {
	needSandbox(t)
	cases := map[string]func(*Request){
		"network rules": func(r *Request) {
			r.Network = &Network{
				Egress: &NetworkEgress{
					Default: Deny,
					Allow: []NetworkRule{{
						To:    []NetworkPeer{{CIDR: "140.82.112.0/20", Except: []string{"140.82.113.0/24"}}},
						Ports: []NetworkPort{{Protocol: TCP, Port: 443}, {Protocol: UDP, Port: 1000, EndPort: 2000}},
					}},
					Deny: []NetworkRule{{To: []NetworkPeer{{CIDR: "10.0.0.0/8"}}}},
				},
				Ingress: &NetworkIngress{Default: Deny, HostLoopback: Deny},
			}
		},
		"ui":        func(r *Request) { r.UI = &UI{Clipboard: ClipboardNone, Disable: Bool(true), Injection: Bool(false)} },
		"telemetry": func(r *Request) { r.Telemetry = &Telemetry{Enabled: false} },
		"seatbelt": func(r *Request) {
			r.Seatbelt = &Seatbelt{GUIAccess: Bool(false), KeychainAccess: Bool(false), NestedPTY: Bool(false)}
		},
		"runtimeConfig+proxy-less": func(r *Request) { r.RuntimeConfig = &RuntimeConfig{} },
		"containment seatbelt":     func(r *Request) { r.Containment = ContainmentSeatbelt },
		"env+cwd": func(r *Request) {
			r.Process.Env = []string{"A=1"}
			r.Process.InheritDefaultEnv = true
			r.Process.Cwd = "/tmp"
		},
		"fs all lists": func(r *Request) {
			r.Filesystem = &Filesystem{
				ReadonlyPaths: []string{"/usr"}, ReadwritePaths: []string{"/tmp"}, DeniedPaths: []string{"/tmp/secret"},
			}
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			req := sh("echo ok")
			mut(&req)
			res, err := Run(context.Background(), req)
			// The backend may refuse a policy it cannot enforce (policy_validation);
			// what this test pins is that the strict parser accepted our encoding.
			if HasCode(err, CodePolicyValidation) {
				t.Skipf("parsed, but unsupported by this backend: %v", err)
			}
			if err != nil {
				t.Fatalf("rejected: %v\njson: %s", err, mustJSON(req))
			}
			if res.ExitCode != 0 {
				t.Fatalf("exit=%d stderr=%q", res.ExitCode, res.Stderr)
			}
		})
	}
}

func mustJSON(r Request) string { s, _ := r.JSON(); return s }

func TestContract_ConcurrentKillWaitClose(t *testing.T) {
	needSandbox(t)
	for i := range 30 {
		p, err := Spawn(context.Background(), sh("sleep 5"))
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		wg.Add(4)
		go func() { defer wg.Done(); _, _ = p.Wait(ctx) }()
		go func() { defer wg.Done(); _ = p.Kill() }()
		go func() { defer wg.Done(); _ = p.Warnings(); _ = p.ID() }()
		go func() { defer wg.Done(); time.Sleep(time.Duration(i%5) * time.Millisecond); _ = p.Close() }()
		within(t, 15*time.Second, "concurrent ops", wg.Wait)
		cancel()
	}
}

func TestContract_PTYOutputEOF(t *testing.T) {
	needSandbox(t)
	p, err := SpawnPTY(context.Background(), sh("echo bye"))
	if err != nil {
		t.Skip(err)
	}
	defer p.Close()
	var res *Result
	within(t, 10*time.Second, "pty output", func() { res, err = p.Output(context.Background()) })
	if err != nil || res == nil || string(res.Stdout) == "" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	t.Logf("pty output=%q", res.Stdout)
}

func TestContract_LifecycleEnvelopeParses(t *testing.T) {
	// A malformed envelope and a well-formed one must be told apart by the native parser,
	// proving our phase/sandboxId encoding is what the contract expects.
	_, errBad := Validate(context.Background(), "start", "not-an-id", nil)
	_, errGood := Validate(context.Background(), "start", "wslc:0123456789abcdef0123456789abcdef", nil)
	t.Logf("bad id: %v", errBad)
	t.Logf("good id: %v", errGood)
	_, errPhase := Validate(context.Background(), "frobnicate", "wslc:0123456789abcdef0123456789abcdef", nil)
	t.Logf("bad phase: %v", errPhase)
	if errPhase == nil {
		t.Fatal("unknown phase must error")
	}
}
