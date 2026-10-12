// Command sandbox-run runs any command inside an MXC sandbox, with stdio passed
// through and the exit code propagated:
//
//	sandbox-run --rw . --ro /usr/share -- make test
//	sandbox-run --net deny --timeout 5s -- sh -c 'curl -m 2 https://example.com'
//
// Reads outside --ro/--rw paths and all writes outside --rw paths are denied by
// the backend's default policy, and the network is denied unless --net allow.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	mxc "github.com/albertocavalcante/mxc-go"
)

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func main() { os.Exit(run()) }

func run() int {
	var ro, rw, deny listFlag
	flag.Var(&ro, "ro", "read-only path (repeatable)")
	flag.Var(&rw, "rw", "read-write path (repeatable)")
	flag.Var(&deny, "deny", "path to hide (repeatable)")
	net := flag.String("net", "deny", "network policy: deny or allow")
	timeout := flag.Duration("timeout", 0, "kill the command after this long (0 = no limit)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: sandbox-run [flags] -- command [args...]\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		return 2
	}

	action := mxc.Deny
	switch *net {
	case "deny":
	case "allow":
		action = mxc.Allow
	default:
		fmt.Fprintf(os.Stderr, "sandbox-run: --net must be deny or allow\n")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	p, err := mxc.Spawn(ctx, mxc.Request{
		Process: mxc.Workload{CommandLine: shellJoin(flag.Args()), Timeout: *timeout},
		Filesystem: &mxc.Filesystem{
			ReadonlyPaths: ro, ReadwritePaths: rw, DeniedPaths: deny,
		},
		Network: &mxc.Network{
			Egress:  &mxc.NetworkEgress{Default: action},
			Ingress: &mxc.NetworkIngress{Default: mxc.Deny, HostLoopback: action},
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox-run: %v\n", err)
		return 126
	}
	defer func() { _ = p.Close() }()

	for _, w := range p.Warnings() {
		fmt.Fprintf(os.Stderr, "sandbox-run: warning: %s\n", w)
	}
	go func() { // forward stdin, then EOF
		defer p.Stdin().Close()
		_, _ = io.Copy(p.Stdin(), os.Stdin)
	}()
	done := make(chan struct{}, 2)
	pump := func(dst io.Writer, src io.Reader) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go pump(os.Stdout, p.Stdout())
	go pump(os.Stderr, p.Stderr())

	res, err := p.Wait(ctx)
	if err != nil { // interrupted: Wait killed the process
		fmt.Fprintf(os.Stderr, "sandbox-run: %v\n", err)
		return 130
	}
	<-done
	<-done
	if res.TimedOut {
		fmt.Fprintf(os.Stderr, "sandbox-run: timed out after %v\n", *timeout)
		return 124 // same as timeout(1)
	}
	return res.ExitCode
}

// shellJoin quotes argv into one POSIX shell command line.
func shellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}
