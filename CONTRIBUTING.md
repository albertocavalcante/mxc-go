# Contributing

## Setup

```sh
just native    # build libmxc_ffi from the pinned MXC commit (needs Rust)
just hooks     # install lefthook (brew install lefthook just)
just ci        # everything CI runs
```

## Commits

[Conventional Commits](https://www.conventionalcommits.org/):
`type(scope)!: subject`, where type is one of `build chore ci docs feat fix perf
refactor revert style test`, subject at most 72 characters. The `commit-msg`
hook rejects anything else; CI checks every commit in a PR.

## Upgrading MXC

1. Update `MXC_REF` to the new commit and `just native`.
2. Diff `src/ffi/mxc_ffi/src/*.rs` against `internal/ffi/mxc_ffi.h`; update the
   header and `internal/ffi/ffi.go`.
3. `just abi-check` and `just ci`.
4. Record the bump in `CHANGELOG.md`.

## Changing the API

Public changes belong in `CHANGELOG.md` under `Unreleased`. Keep cgo confined to
`internal/ffi`: the public package must never handle a C pointer.
