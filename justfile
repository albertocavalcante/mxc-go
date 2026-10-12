# mxc-go development commands
#
# Native library: mxc_ffi is built from a pinned microsoft/mxc checkout (MXC_REF)
# into .mxc/. Override MXC_FFI_DIR to use an existing build instead.

mxc_ref := `cat MXC_REF`
mxc_dir := justfile_directory() / ".mxc"
ffi_dir := env("MXC_FFI_DIR", mxc_dir / "src/target/release")

export CGO_LDFLAGS := "-L" + ffi_dir + " -Wl,-rpath," + ffi_dir

default: test

# Build libmxc_ffi from the pinned upstream commit.
native:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ ! -d "{{ mxc_dir }}/.git" ]; then
        git clone --filter=blob:none https://github.com/microsoft/mxc "{{ mxc_dir }}"
    fi
    git -C "{{ mxc_dir }}" fetch --quiet origin "{{ mxc_ref }}" || git -C "{{ mxc_dir }}" fetch --quiet origin
    git -C "{{ mxc_dir }}" checkout --quiet "{{ mxc_ref }}"
    cargo build --release -p mxc_ffi --manifest-path "{{ mxc_dir }}/src/Cargo.toml"

build:
    go build ./...

test:
    go tool -modfile=tools.go.mod gotestsum --format pkgname-and-test-fails -- ./...

test-race:
    go tool -modfile=tools.go.mod gotestsum --format pkgname-and-test-fails -- -race ./...

lint:
    go tool -modfile=tools.go.mod golangci-lint run --timeout=5m

fmt:
    gofmt -w .

fmt-check:
    test -z "$(gofmt -l .)"

vet:
    go vet ./...

tidy:
    go mod tidy
    go mod tidy -modfile=tools.go.mod

# Verify every C function this binding declares exists in the built library.
abi-check:
    ./scripts/abi-check.sh "{{ ffi_dir }}"

example:
    go run ./examples/hello

ci: fmt-check vet lint test-race abi-check

# Point git at the repo's hooks (lefthook).
hooks:
    lefthook install
