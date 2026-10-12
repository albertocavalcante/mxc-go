package mxc_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	mxc "github.com/albertocavalcante/mxc-go"
)

// Examples have no Output comment: they are compiled but not executed, because
// running them needs a native library and a supported sandbox backend.

func ExampleRun() {
	res, err := mxc.Run(context.Background(), mxc.Request{
		Process:    mxc.Workload{CommandLine: "echo hello", Timeout: 10 * time.Second},
		Filesystem: &mxc.Filesystem{ReadwritePaths: []string{os.TempDir()}},
		Network:    &mxc.Network{Egress: &mxc.NetworkEgress{Default: mxc.Deny}},
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("exit=%d stdout=%q\n", res.ExitCode, res.Stdout)
}

func ExampleSpawn() {
	p, err := mxc.Spawn(context.Background(), mxc.Request{
		Process: mxc.Workload{CommandLine: "cat"},
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer p.Close()

	_, _ = io.WriteString(p.Stdin(), "ping\n")
	p.Stdin().Close() // EOF
	out, _ := io.ReadAll(p.Stdout())
	fmt.Printf("%s", out)
}

func ExampleHasCode() {
	_, err := mxc.RunJSON(context.Background(), `{"version":"0.0.0"}`)
	if mxc.HasCode(err, mxc.CodeMalformedRequest) {
		fmt.Println("rejected by the native contract parser")
	}
}

func ExampleRequest_JSON() {
	js, _ := mxc.Request{
		Process: mxc.Workload{CommandLine: "true"},
	}.JSON()
	fmt.Println(js)
	// Output: {"version":"1.0.0","process":{"commandLine":"true"}}
}
