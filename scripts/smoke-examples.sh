#!/usr/bin/env bash
# End-to-end smoke test of the example programs against the real sandbox.
# Requires libmxc_ffi (CGO_LDFLAGS as for `just ci`) and a supported backend.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin="$(mktemp -d)"
work="$(mktemp -d)"
trap 'rm -rf "$bin" "$work"; [ -n "${srv_pid:-}" ] && kill "$srv_pid" 2>/dev/null || true' EXIT

cd "$here"
go build -o "$bin/sandbox-run" ./examples/sandbox-run
go build -o "$bin/codeinterp" ./examples/codeinterp

fail() { echo "smoke: FAIL: $*" >&2; exit 1; }
ok() { echo "smoke: ok: $*"; }

# --- sandbox-run -------------------------------------------------------------
out="$("$bin/sandbox-run" --rw "$work" -- sh -c "echo hi > '$work/f' && cat '$work/f'")"
[ "$out" = "hi" ] || fail "rw write/read: got '$out'"
ok "sandbox-run writes inside --rw"

outside="$(mktemp -u)"
"$bin/sandbox-run" --rw "$work" -- sh -c "echo x > '$outside'" 2>/dev/null || true
[ ! -e "$outside" ] || fail "sandbox-run wrote outside --rw: $outside"
ok "sandbox-run cannot write outside --rw"

set +e
"$bin/sandbox-run" -- sh -c 'exit 42'; [ $? -eq 42 ] || fail "exit code not propagated"
"$bin/sandbox-run" --timeout 500ms -- sleep 10 2>/dev/null; [ $? -eq 124 ] || fail "timeout should exit 124"
set -e
ok "sandbox-run propagates exit codes and timeouts"

[ "$(echo piped | "$bin/sandbox-run" -- cat)" = "piped" ] || fail "stdin passthrough"
ok "sandbox-run passes stdin through"

# --- codeinterp --------------------------------------------------------------
"$bin/codeinterp" -addr 127.0.0.1:18099 -work "$work/ci" >"$work/ci.log" 2>&1 &
srv_pid=$!
for _ in $(seq 1 50); do curl -fsS localhost:18099/healthz >/dev/null 2>&1 && break; sleep 0.1; done
curl -fsS localhost:18099/healthz | grep -q '"ok":true' || fail "healthz"
ok "codeinterp is healthy"

resp="$(curl -fsS localhost:18099/v1/run -d '{"language":"sh","code":"echo smoke; exit 3"}')"
grep -q '"exit_code":3' <<<"$resp" && grep -q 'smoke' <<<"$resp" || fail "codeinterp run: $resp"
ok "codeinterp runs a snippet"

resp="$(curl -fsS localhost:18099/v1/run -d '{"language":"sh","code":"while :; do :; done","timeout_ms":600}')"
grep -q '"timed_out":true' <<<"$resp" || fail "codeinterp timeout: $resp"
ok "codeinterp enforces the timeout"
echo "smoke: all passed"
