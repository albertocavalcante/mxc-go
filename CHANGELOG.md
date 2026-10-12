# Changelog

## Versioning policy

`mxc-go` follows **v0.x = unstable**: breaking changes may ship in any minor
release until v1.0, after which it follows semver. Each release names the MXC
commit it was tested against.

## Unreleased

Initial implementation, tested against MXC `c6f301d` (see `MXC_REF`):

- Typed `1.0.0` request model, `Run`/`RunJSON`, `Spawn`/`SpawnJSON`, `SpawnPTY`.
- Live processes with `io.Reader`/`io.Writer` stdio, `Wait`, `Output`, `Kill`,
  `Resize`, and context cancellation.
- Persistent-container lifecycle, discovery, telemetry consent, structured errors.
