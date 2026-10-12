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
- Backend discovery, telemetry consent (including the `RequestConsent` prompt
  flow with a panic-safe callback), and structured errors.
- A forgotten `Close` is not fatal: a finalizer reclaims the process.
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

## Behavior notes

- `Run` output is decoded lossily by the native layer (invalid UTF-8 and NUL
  become U+FFFD). Use `Spawn` and read the pipes for raw bytes.
- `Result.ExitCode` is `-1` when the workload did not exit normally (timeout or
  killed by a signal).
- Closing a PTY process's stdin sends the terminal EOF character, which the
  terminal echoes as `^D`. `Output` closes stdin first, so PTY output from
  `Output` may begin with it.
- Cross-field rules (an ICMP rule has no port, an `except` CIDR lies inside its
  `cidr`, ...) are enforced by MXC natively and surface as a `malformed_request`
  or `policy_validation` error rather than being duplicated here.

## Platform support

| Platform | Backend | Status |
|---|---|---|
| macOS (arm64) | Seatbelt | Tested locally and in CI: run, filesystem and network enforcement, timeout, streaming, cancel, PTY |
| Linux (x64) | Bubblewrap | Tested in CI on `ubuntu-latest` (needs `bwrap` and unprivileged user namespaces) |
| Windows | ProcessContainer, WSLC, ... | Not tested (the cgo path needs a MinGW/MSVC import-library story); use the Java binding to exercise `mxc_ffi.dll` |

Availability always depends on the host; use `PlatformSupportInfo()` and
`AvailableBackends()` to check at runtime.

## Verification

Beyond unit and race-detector tests:

- **TLA+ model** of the process close protocol ([`formal/`](formal/README.md)),
  checked exhaustively with TLC in CI. It proves the shipped design free of
  use-after-free, double-free, leaks and deadlock, and it reproduces the original
  deadlock (asserted, so the model cannot go vacuous).
- **Property test** (`property_test.go`): random requests built through the
  typed API are never rejected by MXC's strict contract parser as malformed.
- **ABI check**: every C function this module declares must be exported by the
  pinned native library.

## Stability

MXC's C ABI is explicitly **unstable** ("regenerate bindings when this surface
changes"). This module pins a tested MXC commit in `MXC_REF` and verifies that
every function it declares is exported by the built library (`just abi-check`,
run in CI). Expect to bump the pin and re-check
`internal/ffi/mxc_ffi.h` against MXC's `src/ffi/mxc_ffi/src/*.rs` on upgrades.
This module is v0.x: the API may change before v1.0.

## Development

```sh
just native && just ci      # fmt-check, vet, golangci-lint, race tests, ABI check, TLA+ model
just hooks                  # install lefthook git hooks
```

Formatting is `gofumpt` + `goimports` (local prefix grouped last); dot and blank
imports are banned by `revive`. Commits must follow [Conventional Commits](https://www.conventionalcommits.org/)
with a subject of at most 72 characters (enforced by the `commit-msg` hook and
in CI). See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT. MXC itself is MIT-licensed by Microsoft Corporation and is not included
here.
