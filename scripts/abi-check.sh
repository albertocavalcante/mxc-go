#!/usr/bin/env bash
# Fails if the header declares a function the native library does not export.
# MXC's C ABI is unstable, so run this whenever MXC_REF changes.
set -euo pipefail

dir="${1:?usage: abi-check.sh <dir containing libmxc_ffi>}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
header="$here/internal/ffi/mxc_ffi.h"

lib=""
for c in libmxc_ffi.dylib libmxc_ffi.so mxc_ffi.dll; do
  [ -f "$dir/$c" ] && lib="$dir/$c" && break
done
[ -n "$lib" ] || { echo "abi-check: no libmxc_ffi in $dir" >&2; exit 1; }

exported="$(nm -g "$lib" 2>/dev/null | awk '{print $NF}' | sed 's/^_//' | grep '^mxc_' | sort -u)"
declared="$(grep -oE '\bmxc_[a-z0-9_]+\(' "$header" | tr -d '(' | sort -u)"

missing="$(comm -23 <(echo "$declared") <(echo "$exported"))"
if [ -n "$missing" ]; then
  echo "abi-check: declared but not exported by $lib:" >&2
  echo "$missing" >&2
  exit 1
fi
echo "abi-check: ok ($(echo "$declared" | wc -l | tr -d ' ') symbols present)"
