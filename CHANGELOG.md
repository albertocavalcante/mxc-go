# Changelog

## Versioning policy

`mxc-go` follows **v0.x = unstable**: breaking changes may ship in any minor
release until v1.0, after which it follows semver. Each release names the MXC
commit it was tested against.

## Unreleased

- Telemetry consent flow: `TelemetryNeedsConsentPrompt` and `RequestConsent` with a
  panic-safe presenter callback.
- A finalizer reclaims a `Process` that is dropped without `Close`.
- TLA+ model of the close protocol, a typed-request property test, and
  `gofumpt`/`goimports`/`revive` formatting and import rules.

Initial implementation, tested against MXC `c6f301d` (see `MXC_REF`):

- Typed `1.0.0` request model, `Run`/`RunJSON`, `Spawn`/`SpawnJSON`, `SpawnPTY`.
- Live processes with `io.Reader`/`io.Writer` stdio, `Wait`, `Output`, `Kill`,
  `Resize`, and context cancellation.
- Persistent-container lifecycle, discovery, telemetry consent, structured errors.

Behavior worth knowing (found while hardening before the first tag):

- `Spawn`, `SpawnPTY` and `SpawnExec` bind the process to their `ctx`, like
  `exec.CommandContext`: canceling it kills the sandboxed process tree.
- `Process.Close` kills the tree first, so it cannot hang behind a blocked
  stream read or write. `Run`/`Output` close stdin and bound the drain when a
  grandchild keeps the pipes open.
- `RunJSON`/`SpawnJSON` reject a NUL byte instead of letting the C boundary
  silently truncate the request.
- Telemetry: `TelemetryConsent` and `TelemetryPolicy` return typed states;
  they are bare strings natively, not JSON.
- Captured output from `Run` is decoded lossily by the native layer (invalid
  UTF-8 and NUL become U+FFFD); use `Spawn` for raw bytes.
