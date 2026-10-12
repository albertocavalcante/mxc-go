// Package ffi is the only package that touches cgo. It exposes the mxc_ffi C
// ABI as small Go functions that copy data out of C memory and free it, so the
// public package never handles a C pointer.
//
// Linking: set CGO_LDFLAGS to point at the directory holding libmxc_ffi, e.g.
//
//	export MXC_FFI_DIR=/path/to/mxc/src/target/release
//	export CGO_LDFLAGS="-L$MXC_FFI_DIR -Wl,-rpath,$MXC_FFI_DIR"
package ffi

/*
#cgo LDFLAGS: -lmxc_ffi
#include <stdlib.h>
#include "mxc_ffi.h"
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// Status codes (mxc_ffi MXC_STATUS_*).
const (
	StatusSuccess = 0
	StatusPanic   = 102
)

// Detail mirrors MxcErrorDetail with owned Go strings.
type Detail struct {
	Message, Operation, NativeCode, Remediation string
}

func detailFrom(d *C.MxcErrorDetail) Detail {
	return Detail{
		Message:     C.GoString(d.message_utf8),
		Operation:   C.GoString(d.operation_utf8),
		NativeCode:  C.GoString(d.native_code_utf8),
		Remediation: C.GoString(d.remediation_utf8),
	}
}

// RunResult is a copied-out MxcRunResult.
type RunResult struct {
	Status         int
	ExitCode       int
	TimedOut       bool
	Stdout, Stderr []byte
	Err            Detail
	MetadataJSON   string
	WarningsJSON   string
}

// Note: mxc_ffi exposes stdout/stderr as NUL-terminated C strings with no
// length, so output from the one-shot (RunJSON) path is truncated at the first
// NUL byte. Use Spawn/Output (length-aware streams) for binary-safe capture.
func runResultFrom(st C.int32_t, out *C.MxcRunResult) RunResult {
	r := RunResult{Status: int(st), ExitCode: int(out.exit_code), TimedOut: out.timed_out != 0}
	if out.stdout_utf8 != nil {
		r.Stdout = []byte(C.GoString(out.stdout_utf8))
	}
	if out.stderr_utf8 != nil {
		r.Stderr = []byte(C.GoString(out.stderr_utf8))
	}
	r.Err = detailFrom(&out.error)
	r.MetadataJSON = C.GoString(out.output_metadata_json_utf8)
	r.WarningsJSON = C.GoString(out.warnings_json_utf8)
	C.mxc_run_result_free(out)
	return r
}

func Version() string { return C.GoString(C.mxc_version()) }

func ownedString(p *C.char) string {
	if p == nil {
		return ""
	}
	s := C.GoString(p)
	C.mxc_string_free(p)
	return s
}

func AvailableBackendsJSON() string { return ownedString(C.mxc_available_backends_json()) }
func PlatformSupportJSON() string   { return ownedString(C.mxc_platform_support_json()) }

func b2i(b bool) C.int32_t {
	if b {
		return 1
	}
	return 0
}

// RunJSON runs a one-shot request to completion.
func RunJSON(req string, experimental bool) RunResult {
	c := C.CString(req)
	defer C.free(unsafe.Pointer(c))
	var out C.MxcRunResult
	st := C.mxc_run_json(c, b2i(experimental), &out)
	return runResultFrom(st, &out)
}

// RunStateAwareExecJSON runs an exec-phase request to completion.
func RunStateAwareExecJSON(req string, experimental bool) RunResult {
	c := C.CString(req)
	defer C.free(unsafe.Pointer(c))
	var out C.MxcRunResult
	st := C.mxc_run_state_aware_exec_json(c, b2i(experimental), &out)
	return runResultFrom(st, &out)
}

// StateAwareJSON drives an envelope phase (provision/start/stop/deprovision).
func StateAwareJSON(req string, dryRun, experimental bool) (resp string, status int, d Detail) {
	c := C.CString(req)
	defer C.free(unsafe.Pointer(c))
	var out C.MxcStateAwareResult
	st := C.mxc_run_state_aware_json(c, b2i(dryRun), b2i(experimental), &out)
	resp = C.GoString(out.response_json_utf8)
	d = detailFrom(&out.error)
	C.mxc_state_aware_result_free(&out)
	return resp, int(st), d
}

// Sandbox is a live process handle. Callers must serialize all calls on it.
type Sandbox struct{ p *C.MxcSandbox }
type ReadStream struct{ p *C.MxcReadStream }
type WriteStream struct{ p *C.MxcWriteStream }
type Closer struct{ p *C.MxcStreamCloser }

func spawnWith(f func(req *C.char, h **C.MxcSandbox, e *C.MxcErrorDetail) C.int32_t, req string) (*Sandbox, int, Detail) {
	c := C.CString(req)
	defer C.free(unsafe.Pointer(c))
	var h *C.MxcSandbox
	var e C.MxcErrorDetail
	st := f(c, &h, &e)
	d := detailFrom(&e)
	C.mxc_error_detail_free(&e)
	if st != 0 || h == nil {
		return nil, int(st), d
	}
	return &Sandbox{h}, 0, d
}

func Spawn(req string, experimental bool) (*Sandbox, int, Detail) {
	return spawnWith(func(r *C.char, h **C.MxcSandbox, e *C.MxcErrorDetail) C.int32_t {
		return C.mxc_spawn_json(r, b2i(experimental), h, e)
	}, req)
}

func SpawnPTY(req string, experimental bool, rows, cols uint16) (*Sandbox, int, Detail) {
	return spawnWith(func(r *C.char, h **C.MxcSandbox, e *C.MxcErrorDetail) C.int32_t {
		return C.mxc_spawn_pty_json(r, b2i(experimental), C.uint16_t(rows), C.uint16_t(cols), h, e)
	}, req)
}

func ExecStateAware(req string, experimental bool) (*Sandbox, int, Detail) {
	return spawnWith(func(r *C.char, h **C.MxcSandbox, e *C.MxcErrorDetail) C.int32_t {
		return C.mxc_exec_state_aware_json(r, b2i(experimental), h, e)
	}, req)
}

func ExecStateAwarePTY(req string, experimental bool, rows, cols uint16) (*Sandbox, int, Detail) {
	return spawnWith(func(r *C.char, h **C.MxcSandbox, e *C.MxcErrorDetail) C.int32_t {
		return C.mxc_state_aware_exec_pty(r, b2i(experimental), C.uint16_t(rows), C.uint16_t(cols), h, e)
	}, req)
}

func (s *Sandbox) Free()            { C.mxc_sandbox_free(s.p); s.p = nil }
func (s *Sandbox) ID() uint32       { return uint32(C.mxc_sandbox_id(s.p)) }
func (s *Sandbox) Kill() int        { return int(C.mxc_sandbox_kill(s.p)) }
func (s *Sandbox) KillTimeout() int { return int(C.mxc_sandbox_kill_for_timeout(s.p)) }
func (s *Sandbox) Resize(rows, cols uint16) int {
	return int(C.mxc_sandbox_pty_resize(s.p, C.uint16_t(rows), C.uint16_t(cols)))
}

func (s *Sandbox) TryWait() (exit int, running, timedOut bool, status int) {
	var e, r, t C.int32_t
	st := C.mxc_sandbox_try_wait(s.p, &e, &r, &t)
	return int(e), r != 0, t != 0, int(st)
}

func (s *Sandbox) Wait() (exit int, timedOut bool, status int) {
	var e, t C.int32_t
	st := C.mxc_sandbox_wait(s.p, &e, &t)
	return int(e), t != 0, int(st)
}

func (s *Sandbox) WarningsJSON() string {
	var o *C.char
	C.mxc_sandbox_warnings_json(s.p, &o)
	return ownedString(o)
}

func (s *Sandbox) MetadataJSON() string {
	var o *C.char
	C.mxc_sandbox_output_metadata_json(s.p, &o)
	return ownedString(o)
}

func (s *Sandbox) TakeStdin() *WriteStream {
	if p := C.mxc_sandbox_take_stdin(s.p); p != nil {
		return &WriteStream{p}
	}
	return nil
}
func (s *Sandbox) TakeStdout() *ReadStream {
	if p := C.mxc_sandbox_take_stdout(s.p); p != nil {
		return &ReadStream{p}
	}
	return nil
}
func (s *Sandbox) TakeStderr() *ReadStream {
	if p := C.mxc_sandbox_take_stderr(s.p); p != nil {
		return &ReadStream{p}
	}
	return nil
}
func (s *Sandbox) StdoutCloser() *Closer {
	if p := C.mxc_sandbox_stdout_closer(s.p); p != nil {
		return &Closer{p}
	}
	return nil
}
func (s *Sandbox) StderrCloser() *Closer {
	if p := C.mxc_sandbox_stderr_closer(s.p); p != nil {
		return &Closer{p}
	}
	return nil
}

func (r *ReadStream) Read(b []byte) (n, status int) {
	if len(b) == 0 {
		return 0, 0
	}
	var out C.size_t
	// b is Go memory passed to C for the duration of the call only; the C side
	// does not retain it (cgo pointer rules satisfied: no Go pointers inside).
	st := C.mxc_stream_read(r.p, (*C.uint8_t)(unsafe.Pointer(&b[0])), C.size_t(len(b)), &out)
	runtime.KeepAlive(b)
	return int(out), int(st)
}
func (r *ReadStream) Free() { C.mxc_read_stream_free(r.p); r.p = nil }

func (w *WriteStream) Write(b []byte) (n, status int) {
	if len(b) == 0 {
		return 0, 0
	}
	var out C.size_t
	st := C.mxc_stream_write(w.p, (*C.uint8_t)(unsafe.Pointer(&b[0])), C.size_t(len(b)), &out)
	runtime.KeepAlive(b)
	return int(out), int(st)
}
func (w *WriteStream) Flush() int { return int(C.mxc_stream_flush(w.p)) }
func (w *WriteStream) Free()      { C.mxc_write_stream_free(w.p); w.p = nil }

func (c *Closer) Close() int { return int(C.mxc_stream_closer_close(c.p)) }
func (c *Closer) Free()      { C.mxc_stream_closer_free(c.p); c.p = nil }

// Telemetry consent (JSON out-strings).
func telemetry(f func(**C.char) C.int32_t) (resp string, status int) {
	var o *C.char
	st := f(&o)
	return ownedString(o), int(st)
}
func TelemetryConsent() (resp string, status int) {
	return telemetry(func(o **C.char) C.int32_t { return C.mxc_telemetry_get_consent(o) })
}
func TelemetryConsentStatus() (resp string, status int) {
	return telemetry(func(o **C.char) C.int32_t { return C.mxc_telemetry_get_consent_status(o) })
}
func TelemetryPolicy() (resp string, status int) {
	return telemetry(func(o **C.char) C.int32_t { return C.mxc_telemetry_get_policy(o) })
}
func TelemetryWithdraw() (resp string, status int) {
	return telemetry(func(o **C.char) C.int32_t { return C.mxc_telemetry_withdraw_consent(o) })
}
