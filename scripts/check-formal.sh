#!/usr/bin/env bash
# Model-checks formal/ProcessHandle.tla with TLC (checksum-pinned tla2tools.jar).
#
# Expectations per scenario:
#   Fixed*               TLC must find no error (safety + liveness hold).
#   Original             TLC must report a deadlock. This guards against the
#                        model silently becoming vacuous.
#   OriginalWithKiller   TLC finds no error (documents why the bug was subtle).
set -euo pipefail

VERSION=1.7.4
SHA256=936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
jar="$here/.tools/tla2tools-$VERSION.jar"

if [ ! -f "$jar" ]; then
  mkdir -p "$here/.tools"
  curl -fsSL -o "$jar.tmp" \
    "https://github.com/tlaplus/tlaplus/releases/download/v$VERSION/tla2tools.jar"
  echo "$SHA256  $jar.tmp" | shasum -a 256 -c - >/dev/null
  mv "$jar.tmp" "$jar"
fi

cd "$here/formal"
status=0
run() { # <config> <expect: ok|deadlock>
  local cfg="$1" expect="$2" out
  out="$(java -XX:+UseParallelGC -cp "$jar" tlc2.TLC -config "$cfg.cfg" -workers auto \
    -nowarning -metadir "$(mktemp -d)" ProcessHandle.tla 2>&1 || true)"
  case "$expect" in
    ok)
      if grep -q "Model checking completed. No error has been found." <<<"$out"; then
        echo "formal: $cfg ok ($(grep -oE '[0-9,]+ distinct states found' <<<"$out" | tail -1))"
      else
        echo "formal: $cfg FAILED (expected no error)" >&2
        grep -E "^Error|violated|Deadlock" <<<"$out" | head -5 >&2
        status=1
      fi ;;
    deadlock)
      if grep -q "Deadlock reached" <<<"$out"; then
        echo "formal: $cfg deadlocks as expected (the original design is unsafe)"
      else
        echo "formal: $cfg should deadlock but did not: the model may be vacuous" >&2
        status=1
      fi ;;
  esac
}

run Fixed ok
run FixedWithKiller ok
run FixedChildExits ok
run OriginalWithKiller ok
run Original deadlock
exit "$status"
