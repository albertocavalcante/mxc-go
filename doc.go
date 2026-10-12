// Package mxc is an unofficial Go binding for MXC (Microsoft eXecution
// Container, https://github.com/microsoft/mxc): a policy-driven sandbox for
// running untrusted code.
//
// It wraps the native mxc_ffi C library via cgo. Requests are built as typed Go
// values, serialized to the exact "1.0.0" MXC request JSON, and handed to the
// native engine, which selects a containment backend (Seatbelt on macOS,
// Bubblewrap on Linux, ProcessContainer on Windows, ...).
//
// # Running
//
// [Run] executes a request to completion and captures its output. [Spawn]
// returns a live [Process] with piped stdio, and [SpawnPTY] attaches the
// workload to a pseudo-terminal. All of them honor context cancellation by
// killing the sandboxed process.
//
// # Errors
//
// Failures reported by the native engine are [*Error] values carrying a [Code]
// (see [HasCode]). A workload that exits non-zero is not an error; inspect
// [Result.ExitCode].
//
// # Native library
//
// The module links against libmxc_ffi, which is not bundled. See the README for
// building it and pointing CGO_LDFLAGS at it. MXC's C ABI is explicitly
// unstable; this module is tested against the commit in MXC_REF.
package mxc
