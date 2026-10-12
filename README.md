# mxc-go

[![Go Reference](https://pkg.go.dev/badge/github.com/albertocavalcante/mxc-go.svg)](https://pkg.go.dev/github.com/albertocavalcante/mxc-go)
[![ci](https://github.com/albertocavalcante/mxc-go/actions/workflows/ci.yml/badge.svg)](https://github.com/albertocavalcante/mxc-go/actions/workflows/ci.yml)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

An **unofficial** Go binding for [MXC](https://github.com/microsoft/mxc)
(Microsoft eXecution Container), a policy-driven sandbox for running untrusted
code: model output, plugins, tools. You describe the workload and a policy
(filesystem, network, UI); MXC picks the OS-native backend (Seatbelt on macOS,
Bubblewrap on Linux, ProcessContainer on Windows, and others) and enforces it.

This module is not affiliated with or endorsed by Microsoft. It wraps MXC's
native `mxc_ffi` C library through cgo, the same layer Microsoft's .NET and Node
SDKs use.

```go
res, err := mxc.Run(ctx, mxc.Request{
    Process:    mxc.Workload{CommandLine: "echo hello", Timeout: 10 * time.Second},
    Filesystem: &mxc.Filesystem{ReadwritePaths: []string{os.TempDir()}},
    Network:    &mxc.Network{Egress: &mxc.NetworkEgress{Default: mxc.Deny}},
})
if err != nil {
    log.Fatal(err) // *mxc.Error: native failure with a Code, operation, remediation
}
fmt.Printf("exit=%d stdout=%q\n", res.ExitCode, res.Stdout)
```

## Features

- Typed `1.0.0` request model that serializes to MXC's exact request JSON, with
  an escape hatch (`RunJSON`, `SpawnJSON`, `Request.Extra`) for any registered
  raw version or backend-specific section.
- Captured runs and **live processes** with stdin/stdout/stderr as
  `io.Writer`/`io.Reader`.
- **PTY** processes with resize.
- `context.Context` support: cancellation kills the sandboxed process tree.
- Persistent-container lifecycle: `Provision`, `Start`, `Exec`, `SpawnExec`,
  `Stop`, `Deprovision`, `Validate` (WSLC and IsolationSession; Windows hosts).
- Backend discovery, telemetry consent, and structured errors.
- Concurrency-safe: native calls on a process handle are serialized, and
  `Wait`/`Output` follow MXC's rule of polling and killing from one call.

## Install

```sh
go get github.com/albertocavalcante/mxc-go
```

The module links against `libmxc_ffi`, which is **not bundled**. Build it from
the MXC commit this module is tested against (`MXC_REF`):

```sh
just native                                  # clones microsoft/mxc into .mxc/ and builds it
export MXC_FFI_DIR="$PWD/.mxc/src/target/release"
export CGO_LDFLAGS="-L$MXC_FFI_DIR -Wl,-rpath,$MXC_FFI_DIR"
```

Requirements: Go 1.22+, cgo, and a Rust toolchain (MXC pins its own via
`rust-toolchain.toml`). Consumers need the same `CGO_LDFLAGS` in their build.

## Usage

### Stream a process

```go
p, err := mxc.Spawn(ctx, mxc.Request{Process: mxc.Workload{CommandLine: "cat"}})
if err != nil { log.Fatal(err) }
defer p.Close()

io.WriteString(p.Stdin(), "ping\n")
p.Stdin().Close() // EOF
out, _ := io.ReadAll(p.Stdout())
res, _ := p.Wait(ctx)
```

### Cancel or time out

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
_, err := mxc.Run(ctx, req) // context.DeadlineExceeded; the sandbox is killed
```

`Workload.Timeout` is enforced by MXC itself and reported as `Result.TimedOut`.

### Environment semantics

`Workload.Env == nil` selects the backend's default environment; a non-nil empty
slice sends an explicitly empty environment.

## Platform support

| Platform | Backend | Status |
|---|---|---|
| macOS (arm64) | Seatbelt | Tested: run, filesystem enforcement, timeout, streaming, cancel, PTY |
| Linux | Bubblewrap, LXC | Untested; builds in CI on a best-effort basis |
| Windows | ProcessContainer, WSLC, ... | Untested (the cgo and lifecycle paths are written but unverified) |

Availability always depends on the host; use `PlatformSupportInfo()` and
`AvailableBackends()` to check at runtime.

## Stability

MXC's C ABI is explicitly **unstable** ("regenerate bindings when this surface
changes"). This module pins a tested MXC commit in `MXC_REF` and verifies that
every function it declares is exported by the built library (`just abi-check`,
run in CI). Expect to bump the pin and re-check
`internal/ffi/mxc_ffi.h` against MXC's `src/ffi/mxc_ffi/src/*.rs` on upgrades.
This module is v0.x: the API may change before v1.0.

## Development

```sh
just native && just ci      # fmt-check, vet, golangci-lint, race tests, ABI check
just hooks                  # install lefthook git hooks
```

Commits must follow [Conventional Commits](https://www.conventionalcommits.org/)
with a subject of at most 72 characters (enforced by the `commit-msg` hook and
in CI). See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT. MXC itself is MIT-licensed by Microsoft Corporation and is not included
here.
