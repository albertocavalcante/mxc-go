package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	mxc "github.com/albertocavalcante/mxc-go"
)

func main() {
	res, err := mxc.Run(context.Background(), mxc.Request{
		Process:    mxc.Workload{CommandLine: "echo hello from the sandbox", Timeout: 10 * time.Second},
		Filesystem: &mxc.Filesystem{ReadwritePaths: []string{os.TempDir()}},
		Network:    &mxc.Network{Egress: &mxc.NetworkEgress{Default: mxc.Deny}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("exit=%d stdout=%q warnings=%v\n", res.ExitCode, res.Stdout, res.Warnings)
}
