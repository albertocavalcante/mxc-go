//go:build !cgo

package ffi

// mxc-go wraps the native libmxc_ffi library through cgo and cannot be built
// with CGO_ENABLED=0. Re-run with CGO_ENABLED=1 and a C toolchain; the
// reference below is intentionally undefined so the compiler reports it.
var _ = mxc_go_requires_cgo__set_CGO_ENABLED_1
