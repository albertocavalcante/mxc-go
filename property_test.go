package mxc

import (
	"context"
	"math/rand/v2"
	"os"
	"runtime"
	"testing"
	"time"
)

// randomRequest builds a request touching every typed section with random
// values. The native contract parser is the authority on what is well-formed
// (it denies unknown fields and checks enums), so the property is: nothing the
// typed API can emit is ever rejected as malformed_request.
func randomRequest(r *rand.Rand) Request {
	pick := func(xs ...string) string { return xs[r.IntN(len(xs))] }
	maybe := func() bool { return r.IntN(2) == 0 }
	boolp := func() *bool { return Bool(maybe()) }

	req := Request{Process: Workload{CommandLine: "true"}}
	onMac := runtime.GOOS == "darwin"
	if onMac && maybe() {
		req.Containment = ContainmentSeatbelt
	}
	if maybe() {
		req.Process.Cwd = pick("/tmp", "/usr", os.TempDir())
	}
	switch r.IntN(3) {
	case 0: // nil: backend default environment
	case 1:
		req.Process.Env = []string{} // explicitly empty
	case 2:
		req.Process.Env = []string{"A=1", "B=two words"}
		req.Process.InheritDefaultEnv = maybe()
	}
	if maybe() {
		req.Process.Timeout = time.Duration(1+r.IntN(30_000)) * time.Millisecond
	}
	if maybe() {
		req.Filesystem = &Filesystem{}
		if maybe() {
			req.Filesystem.ReadonlyPaths = []string{pick("/usr", "/bin", "/etc")}
		}
		if maybe() {
			req.Filesystem.ReadwritePaths = []string{os.TempDir(), "/tmp"}
		}
		if maybe() {
			req.Filesystem.DeniedPaths = []string{pick("/tmp/mxc-denied", "/var/empty")}
		}
	}
	if maybe() {
		n := &Network{}
		if maybe() {
			n.Egress = &NetworkEgress{Default: Action(pick("allow", "deny"))}
			if maybe() {
				n.Egress.Allow = []NetworkRule{{
					To:    []NetworkPeer{randomPeer(r)},
					Ports: []NetworkPort{randomPort(r)},
				}}
			}
		}
		if maybe() {
			n.Ingress = &NetworkIngress{Default: Action(pick("allow", "deny")), HostLoopback: Action(pick("allow", "deny"))}
		}
		req.Network = n
	}
	if maybe() {
		req.UI = &UI{Clipboard: Clipboard(pick("none", "read", "write", "all")), Disable: boolp(), Injection: boolp()}
	}
	if maybe() {
		req.Telemetry = &Telemetry{Enabled: false}
	}
	// A backend section must match the selected containment, which is a
	// cross-field rule MXC enforces. Seatbelt is only selectable on macOS.
	if onMac && maybe() {
		req.Seatbelt = &Seatbelt{GUIAccess: boolp(), KeychainAccess: boolp(), NestedPTY: boolp()}
	}
	if maybe() {
		req.RuntimeConfig = &RuntimeConfig{}
	}
	return req
}

// randomPort respects the cross-field rule that ICMP carries no port.
func randomPort(r *rand.Rand) NetworkPort {
	switch r.IntN(3) {
	case 0:
		return NetworkPort{Protocol: ICMP}
	case 1:
		return NetworkPort{Protocol: TCP, Port: 1 + r.IntN(65535)}
	default:
		return NetworkPort{Protocol: UDP, Port: 1000, EndPort: 2000}
	}
}

// randomPeer returns a CIDR with an except entry that lies inside it (the
// contract requires containment; that is a semantic rule, not a typing one).
func randomPeer(r *rand.Rand) NetworkPeer {
	if r.IntN(2) == 0 {
		return NetworkPeer{CIDR: "10.0.0.0/8", Except: []string{"10.1.0.0/16"}}
	}
	return NetworkPeer{CIDR: "fd00::/8", Except: []string{"fd00:1::/32"}}
}

func TestProperty_TypedRequestsAreNeverMalformed(t *testing.T) {
	needSandbox(t)
	seed := uint64(time.Now().UnixNano())
	t.Logf("seed=%d", seed)
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))

	outcomes := map[string]int{}
	for i := range 150 {
		req := randomRequest(r)
		_, err := Run(context.Background(), req)
		switch {
		case err == nil:
			outcomes["ok"]++
		case HasCode(err, CodeMalformedRequest):
			js, _ := req.JSON()
			t.Fatalf("typed request rejected as malformed (seed=%d, i=%d): %v\n%s", seed, i, err, js)
		default:
			outcomes[err.(*Error).Code.String()]++
		}
	}
	t.Logf("outcomes: %v", outcomes)
	// Non-vacuous: a healthy share of the random requests must actually run.
	if outcomes["ok"] < 20 {
		t.Fatalf("only %d/150 random requests ran; the generator is too hostile to prove anything", outcomes["ok"])
	}
}
