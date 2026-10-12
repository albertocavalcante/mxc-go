package mxc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/albertocavalcante/mxc-go/internal/ffi"
)

// Option adjusts how a request is launched.
type Option func(*options)

type options struct {
	experimental bool
	rows, cols   uint16
}

// WithExperimental permits experimental backends (never read from the JSON).
func WithExperimental() Option { return func(o *options) { o.experimental = true } }

// WithPTYSize sets the initial terminal size for SpawnPTY (default 24x80).
func WithPTYSize(rows, cols uint16) Option {
	return func(o *options) { o.rows, o.cols = rows, cols }
}

func apply(opts []Option) options {
	o := options{rows: 24, cols: 80}
	for _, f := range opts {
		f(&o)
	}
	return o
}

// Result is a completed run.
type Result struct {
	ExitCode int
	// TimedOut is true when the request's Workload.Timeout expired and the
	// workload was killed; ExitCode is then -1.
	TimedOut bool
	Stdout   []byte
	Stderr   []byte
	// Warnings are security warnings raised during the run (e.g. relaxed policy).
	Warnings []string
	// Metadata is backend-specific output metadata, if any.
	Metadata json.RawMessage
}

func newError(status int, d ffi.Detail) *Error {
	return &Error{Code: Code(status), Message: d.Message, Operation: d.Operation,
		NativeCode: d.NativeCode, Remediation: d.Remediation}
}

func resultFrom(r ffi.RunResult) (*Result, error) {
	if r.Status != ffi.StatusSuccess {
		return nil, newError(r.Status, r.Err)
	}
	res := &Result{ExitCode: r.ExitCode, TimedOut: r.TimedOut, Stdout: r.Stdout, Stderr: r.Stderr}
	if r.WarningsJSON != "" {
		_ = json.Unmarshal([]byte(r.WarningsJSON), &res.Warnings)
	}
	if r.MetadataJSON != "" {
		res.Metadata = json.RawMessage(r.MetadataJSON)
	}
	return res, nil
}

// Run executes the request to completion and captures its output.
//
// Captured output is decoded lossily by the native layer: invalid UTF-8 and NUL
// bytes become U+FFFD. Use [Spawn] and read the pipes for raw bytes. ExitCode is
// -1 when the workload did not exit normally (timeout, or killed by a signal).
//
// A non-zero workload exit code is not an error; inspect Result.ExitCode. If
// ctx can be canceled, the workload is killed on cancellation and ctx.Err() is
// returned; otherwise the native one-shot call is used directly.
func Run(ctx context.Context, req Request, opts ...Option) (*Result, error) {
	js, err := req.JSON()
	if err != nil {
		return nil, err
	}
	return RunJSON(ctx, js, opts...)
}

// RunJSON is Run for a caller-supplied exact-version MXC request JSON document
// (any registered version, e.g. "1.1.0-alpha").
func RunJSON(ctx context.Context, requestJSON string, opts ...Option) (*Result, error) {
	if err := checkJSON(requestJSON); err != nil {
		return nil, err
	}
	o := apply(opts)
	if ctx.Done() == nil {
		return resultFrom(ffi.RunJSON(requestJSON, o.experimental))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := spawnJSON(ctx, requestJSON, o)
	if err != nil {
		return nil, err
	}
	defer func() { _ = p.Close() }()
	return p.Output(ctx)
}

// Spawn starts the workload and returns a live process with piped stdio.
func Spawn(ctx context.Context, req Request, opts ...Option) (*Process, error) {
	js, err := req.JSON()
	if err != nil {
		return nil, err
	}
	return SpawnJSON(ctx, js, opts...)
}

// SpawnJSON is Spawn for a caller-supplied request JSON document.
func SpawnJSON(ctx context.Context, requestJSON string, opts ...Option) (*Process, error) {
	if err := checkJSON(requestJSON); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return spawnJSON(ctx, requestJSON, apply(opts))
}

// SpawnPTY starts the workload attached to an MXC-owned pseudo-terminal.
// Stdout carries the merged terminal output; Stdin feeds the terminal.
func SpawnPTY(ctx context.Context, req Request, opts ...Option) (*Process, error) {
	js, err := req.JSON()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o := apply(opts)
	sb, st, d := ffi.SpawnPTY(js, o.experimental, o.rows, o.cols)
	if sb == nil {
		return nil, newError(st, d)
	}
	return newProcess(ctx, sb), nil
}

func spawnJSON(ctx context.Context, js string, o options) (*Process, error) {
	sb, st, d := ffi.Spawn(js, o.experimental)
	if sb == nil {
		return nil, newError(st, d)
	}
	return newProcess(ctx, sb), nil
}

// Process is a live sandboxed process. Methods are safe for concurrent use,
// except that the three streams must each be driven from one goroutine at a
// time (stdout, stderr and stdin may be used concurrently with each other).
type Process struct {
	mu     sync.Mutex // serializes every call on the native handle
	sb     *ffi.Sandbox
	done   *Result // set once the exit has been observed
	closed bool
	stop   chan struct{} // closed by Close; releases the context watcher

	stdin  *stdinPipe
	stdout *readPipe
	stderr *readPipe
}

func newProcess(ctx context.Context, sb *ffi.Sandbox) *Process {
	p := &Process{sb: sb, stop: make(chan struct{})}
	if w := sb.TakeStdin(); w != nil {
		p.stdin = &stdinPipe{w: w}
	}
	if r := sb.TakeStdout(); r != nil {
		p.stdout = &readPipe{r: r, c: sb.StdoutCloser()}
	}
	if r := sb.TakeStderr(); r != nil {
		p.stderr = &readPipe{r: r, c: sb.StderrCloser()}
	}
	// Safety net for callers that forget Close; Close remains the contract.
	runtime.SetFinalizer(p, func(p *Process) { _ = p.Close() })
	// Like exec.CommandContext: canceling ctx kills the process.
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				_ = p.Kill()
			case <-p.stop:
			}
		}()
	}
	return p
}

// ID returns the native process id.
func (p *Process) ID() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0
	}
	return p.sb.ID()
}

// Stdin returns the child's stdin, or nil if it is not piped. Closing it sends EOF.
func (p *Process) Stdin() io.WriteCloser {
	if p.stdin == nil {
		return nil
	}
	return p.stdin
}

// Stdout returns the child's stdout (merged terminal output for a PTY), or nil.
func (p *Process) Stdout() io.ReadCloser {
	if p.stdout == nil {
		return nil
	}
	return p.stdout
}

// Stderr returns the child's stderr, or nil (always nil for a PTY).
func (p *Process) Stderr() io.ReadCloser {
	if p.stderr == nil {
		return nil
	}
	return p.stderr
}

// Warnings returns security warnings raised at launch.
func (p *Process) Warnings() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	var w []string
	if js := p.sb.WarningsJSON(); js != "" {
		_ = json.Unmarshal([]byte(js), &w)
	}
	return w
}

// Resize changes the terminal size of a PTY process.
func (p *Process) Resize(rows, cols uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errClosed
	}
	if st := p.sb.Resize(rows, cols); st != 0 {
		return &Error{Code: Code(st), Message: "pty resize failed"}
	}
	return nil
}

// Kill terminates the process tree.
func (p *Process) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errClosed
	}
	if st := p.sb.Kill(); st != 0 {
		return &Error{Code: Code(st), Message: "kill failed"}
	}
	return nil
}

var errClosed = errors.New("mxc: process is closed")

// tryWait polls once. done reports whether the process has exited.
func (p *Process) tryWait() (res *Result, done bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done != nil {
		cp := *p.done
		return &cp, true, nil
	}
	if p.closed {
		return nil, false, errClosed
	}
	exit, running, timedOut, st := p.sb.TryWait()
	if st != 0 {
		return nil, false, &Error{Code: Code(st), Message: "wait failed"}
	}
	if running {
		return nil, false, nil
	}
	p.done = &Result{ExitCode: exit, TimedOut: timedOut}
	cp := *p.done
	return &cp, true, nil
}

// Wait blocks until the process exits and returns its exit status (Stdout and
// Stderr are not populated; use Output to capture them). Per the native
// concurrency contract, cancellation is implemented by polling and killing from
// the same call, never by blocking one thread while another kills. On
// cancellation the process is killed and ctx.Err() is returned.
func (p *Process) Wait(ctx context.Context) (*Result, error) {
	delay := time.Millisecond
	for {
		res, done, err := p.tryWait()
		if err != nil {
			return nil, err
		}
		if done {
			return res, nil
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			_ = p.Kill()
			// Reap so the handle can be freed cleanly.
			for range 500 {
				if _, d, _ := p.tryWait(); d {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			return nil, ctx.Err()
		case <-t.C:
		}
		if delay < 25*time.Millisecond {
			delay *= 2
		}
	}
}

// Output drains stdout and stderr concurrently, waits for exit, and returns
// everything captured. It must not be combined with manual reads of the streams.
func (p *Process) Output(ctx context.Context) (*Result, error) {
	// Nothing is supplied on stdin here; close it so children such as `cat`
	// see EOF instead of waiting forever.
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	var out, errb lockedBuf
	var wg sync.WaitGroup
	drain := func(r io.Reader, dst *lockedBuf) {
		defer wg.Done()
		if r != nil {
			_, _ = io.Copy(dst, r)
		}
	}
	var so, se io.Reader
	if p.stdout != nil {
		so = p.stdout
	}
	if p.stderr != nil {
		se = p.stderr
	}
	wg.Add(2)
	go drain(so, &out)
	go drain(se, &errb)

	res, err := p.Wait(ctx)
	if err != nil {
		// Unblock the readers before returning.
		p.closeStreams()
		wg.Wait()
		return nil, err
	}
	// The child has exited, but a grandchild may still hold the pipes open.
	// Give the readers a short grace to drain, bounded by ctx.
	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	grace := time.NewTimer(drainGrace)
	defer grace.Stop()
	select {
	case <-drained:
	case <-grace.C:
		go p.closeStreams() // may block behind a Read; never wait on it
	case <-ctx.Done():
		go p.closeStreams()
		return nil, ctx.Err()
	}
	full := *res
	full.Stdout, full.Stderr = out.bytes(), errb.bytes()
	full.Warnings = p.Warnings()
	p.mu.Lock()
	if !p.closed {
		if md := p.sb.MetadataJSON(); md != "" {
			full.Metadata = json.RawMessage(md)
		}
	}
	p.mu.Unlock()
	return &full, nil
}

// lockedBuf is a bytes sink that can be snapshotted while a reader is still
// appending to it.
type lockedBuf struct {
	mu sync.Mutex
	b  []byte
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.b = append(l.b, p...)
	l.mu.Unlock()
	return len(p), nil
}

func (l *lockedBuf) bytes() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]byte(nil), l.b...)
}

// drainGrace bounds how long Output waits for stdout/stderr to reach EOF after
// the child has exited (a surviving grandchild may keep the pipes open).
const drainGrace = 500 * time.Millisecond

func (p *Process) closeStreams() {
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.stdout != nil {
		_ = p.stdout.Close()
	}
	if p.stderr != nil {
		_ = p.stderr.Close()
	}
}

// Close releases the process. If it is still running its tree is killed.
// Safe to call more than once.
func (p *Process) Close() error {
	// Kill first: this unblocks any in-flight stream Read/Write (including
	// readers with no native closer) so closeStreams cannot hang behind them.
	p.mu.Lock()
	if !p.closed {
		_ = p.sb.Kill()
	}
	p.mu.Unlock()
	p.closeStreams()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	p.sb.Free()
	return nil
}

// --- stream wrappers -------------------------------------------------------

type readPipe struct {
	mu   sync.Mutex // held across each native read; Close frees under it
	r    *ffi.ReadStream
	c    *ffi.Closer
	once sync.Once
}

func (rp *readPipe) Read(b []byte) (int, error) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if rp.r == nil {
		return 0, io.EOF
	}
	n, st := rp.r.Read(b)
	if st != 0 {
		return n, &Error{Code: Code(st), Message: "stream read failed"}
	}
	if n == 0 && len(b) > 0 {
		return 0, io.EOF
	}
	return n, nil
}

// Close unblocks any in-flight Read (without killing the child), then frees.
func (rp *readPipe) Close() error {
	rp.once.Do(func() {
		if rp.c != nil {
			rp.c.Close() // unblock a blocked Read; closer is not guarded by mu
		}
		rp.mu.Lock()
		if rp.r != nil {
			rp.r.Free()
			rp.r = nil
		}
		if rp.c != nil {
			rp.c.Free()
			rp.c = nil
		}
		rp.mu.Unlock()
	})
	return nil
}

type stdinPipe struct {
	mu sync.Mutex
	w  *ffi.WriteStream
}

func (sp *stdinPipe) Write(b []byte) (int, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.w == nil {
		return 0, io.ErrClosedPipe
	}
	total := 0
	for total < len(b) {
		n, st := sp.w.Write(b[total:])
		total += n
		if st != 0 {
			return total, &Error{Code: Code(st), Message: "stream write failed"}
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	if st := sp.w.Flush(); st != 0 {
		return total, &Error{Code: Code(st), Message: "stream flush failed"}
	}
	return total, nil
}

// Close flushes and closes stdin, delivering EOF to the child.
func (sp *stdinPipe) Close() error {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.w != nil {
		sp.w.Free()
		sp.w = nil
	}
	return nil
}

// checkJSON rejects a document the C boundary would silently truncate at a NUL
// byte, which could otherwise run a different request than the caller wrote.
func checkJSON(js string) error {
	if strings.IndexByte(js, 0) >= 0 {
		return &Error{Code: CodeMalformedRequest, Message: "request JSON contains a NUL byte"}
	}
	return nil
}
