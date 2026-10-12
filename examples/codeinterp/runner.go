package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mxc "github.com/albertocavalcante/mxc-go"
)

// interpreter describes how to run one language inside the sandbox.
type interpreter struct {
	file     string   // file name written into the work directory
	command  string   // absolute interpreter path
	args     string   // arguments before the file name
	readOnly []string // extra directories the interpreter needs to read
}

// config holds the service's policy. Everything here is operator-controlled;
// nothing is taken from the request except the language, code and limits below.
type config struct {
	workRoot      string
	maxTimeout    time.Duration
	maxCodeBytes  int
	outputLimit   int      // per stream, bytes
	denyPaths     []string // always unreadable inside the sandbox (secrets)
	interpreters  map[string]interpreter
	cpuSeconds    int // ulimit -t
	fileSizeKB    int // ulimit -f
	maxOpenFiles  int // ulimit -n
	maxConcurrent int
	slots         chan struct{}
}

func newConfig(workRoot string, denyPaths []string) (*config, error) {
	c := &config{
		workRoot:      workRoot,
		maxTimeout:    30 * time.Second,
		maxCodeBytes:  256 << 10,
		outputLimit:   64 << 10,
		denyPaths:     denyPaths,
		interpreters:  map[string]interpreter{},
		cpuSeconds:    20,
		fileSizeKB:    10 << 10,
		maxOpenFiles:  128,
		maxConcurrent: 4,
	}
	c.slots = make(chan struct{}, c.maxConcurrent)
	if err := os.MkdirAll(workRoot, 0o700); err != nil {
		return nil, err
	}
	c.interpreters["sh"] = interpreter{file: "main.sh", command: "/bin/sh"}
	for lang, spec := range map[string]struct{ bin, file, args string }{
		"python": {"python3", "main.py", "-I "}, // -I: isolated mode, ignores env and user site
		"node":   {"node", "main.js", ""},
	} {
		path, err := exec.LookPath(spec.bin)
		if err != nil {
			continue // language simply not offered on this host
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			resolved = path
		}
		c.interpreters[lang] = interpreter{
			file: spec.file, command: resolved, args: spec.args,
			readOnly: interpreterReadRoots(resolved),
		}
	}
	return c, nil
}

// interpreterReadRoots returns the read-only directories an interpreter needs.
// A Homebrew binary links libraries all over its prefix (Cellar, opt, lib), so
// that prefix is granted read-only; nothing under it is writable or a secret.
func interpreterReadRoots(bin string) []string {
	roots := []string{filepath.Dir(bin), filepath.Dir(filepath.Dir(bin))}
	for _, prefix := range []string{"/opt/homebrew", "/usr/local"} {
		if strings.HasPrefix(bin, prefix+"/") {
			roots = append(roots, prefix)
		}
	}
	return roots
}

// runRequest is the API input.
type runRequest struct {
	Language  string `json:"language"`
	Code      string `json:"code"`
	Stdin     string `json:"stdin,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// runResult is the API output.
type runResult struct {
	ExitCode        int    `json:"exit_code"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	TimedOut        bool   `json:"timed_out"`
	Truncated       bool   `json:"truncated"`
	DurationMS      int64  `json:"duration_ms"`
	StdoutTruncated bool   `json:"-"`
	StderrTruncated bool   `json:"-"`
}

var (
	errBusy    = errors.New("all sandbox slots are busy")
	errInvalid = errors.New("invalid request")
)

// execute runs one snippet. onChunk (optional) receives output as it arrives.
func (c *config) execute(ctx context.Context, rq runRequest, onChunk func(stream string, b []byte)) (*runResult, error) {
	interp, ok := c.interpreters[rq.Language]
	if !ok {
		return nil, fmt.Errorf("%w: unsupported language %q", errInvalid, rq.Language)
	}
	if rq.Code == "" || len(rq.Code) > c.maxCodeBytes {
		return nil, fmt.Errorf("%w: code must be 1..%d bytes", errInvalid, c.maxCodeBytes)
	}
	timeout := 10 * time.Second
	if rq.TimeoutMS > 0 {
		timeout = time.Duration(rq.TimeoutMS) * time.Millisecond
	}
	if timeout > c.maxTimeout {
		return nil, fmt.Errorf("%w: timeout_ms exceeds %d", errInvalid, c.maxTimeout.Milliseconds())
	}

	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return nil, errBusy
	}

	// A fresh, private work directory per request: the only writable path.
	work, err := os.MkdirTemp(c.workRoot, "run-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	if err := os.WriteFile(filepath.Join(work, interp.file), []byte(rq.Code), 0o600); err != nil {
		return nil, err
	}

	// MXC enforces filesystem and network policy and a wall-clock timeout. It
	// does not cap CPU, memory, file size or open files, so the service adds
	// the limits it can with ulimit (macOS ignores some, e.g. -v).
	cmd := fmt.Sprintf("ulimit -t %d -f %d -n %d 2>/dev/null; exec %s %s%s",
		c.cpuSeconds, c.fileSizeKB*2, c.maxOpenFiles, interp.command, interp.args, interp.file)

	req := mxc.Request{
		Process: mxc.Workload{
			CommandLine: cmd,
			Cwd:         work,
			// An explicit environment: nothing from the service's own
			// environment (tokens, credentials) is inherited.
			Env:     []string{"PATH=/usr/bin:/bin", "HOME=" + work, "TMPDIR=" + work, "LANG=C.UTF-8"},
			Timeout: timeout,
		},
		Filesystem: &mxc.Filesystem{
			ReadwritePaths: []string{work},
			ReadonlyPaths:  interp.readOnly,
			DeniedPaths:    c.denyPaths,
		},
		Network: &mxc.Network{
			Egress:  &mxc.NetworkEgress{Default: mxc.Deny},
			Ingress: &mxc.NetworkIngress{Default: mxc.Deny, HostLoopback: mxc.Deny},
		},
	}

	start := time.Now()
	// ctx also bounds the process: canceling it (client disconnect, shutdown)
	// kills the sandboxed process tree.
	ctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()
	p, err := mxc.Spawn(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = p.Close() }()

	go func() { // feed stdin, then deliver EOF
		defer p.Stdin().Close()
		if rq.Stdin != "" {
			_, _ = io.WriteString(p.Stdin(), rq.Stdin)
		}
	}()

	var wg sync.WaitGroup
	var out, errb capped
	pump := func(stream string, r io.Reader, dst *capped) {
		defer wg.Done()
		buf := make([]byte, 8<<10)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if kept := dst.add(buf[:n], c.outputLimit); kept > 0 && onChunk != nil {
					onChunk(stream, buf[:kept])
				}
				if dst.truncated {
					_ = p.Kill() // a flood is a policy violation: stop the producer
				}
			}
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go pump("stdout", p.Stdout(), &out)
	go pump("stderr", p.Stderr(), &errb)

	res, err := p.Wait(ctx)
	if err != nil {
		return nil, err
	}
	// The child has exited, but a surviving descendant could hold the pipes
	// open; Close kills the tree and unblocks the readers.
	_ = p.Close()
	wg.Wait()

	return &runResult{
		ExitCode:   res.ExitCode,
		Stdout:     string(out.b),
		Stderr:     string(errb.b),
		TimedOut:   res.TimedOut,
		Truncated:  out.truncated || errb.truncated,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

// capped accumulates at most limit bytes and remembers if it dropped any.
type capped struct {
	b         []byte
	truncated bool
}

// add appends up to the limit and returns how many bytes of p were kept.
func (c *capped) add(p []byte, limit int) int {
	room := limit - len(c.b)
	if room <= 0 {
		c.truncated = true
		return 0
	}
	if len(p) > room {
		p = p[:room]
		c.truncated = true
	}
	c.b = append(c.b, p...)
	return len(p)
}
