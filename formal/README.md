# Formal model: the `Process` close protocol

[`ProcessHandle.tla`](ProcessHandle.tla) is a TLA+ model of the one genuinely
concurrent part of this binding, and of its Java twin `MxcProcess`: a native
sandbox handle with a handle lock, per-stream locks held across **blocking** native
reads and writes, `Kill`, `Wait`, a context watcher, and `Close` called both
explicitly and by a finalizer / `Cleaner`.

```sh
just formal        # runs TLC on every scenario (downloads a checksum-pinned tla2tools.jar)
```

## What is checked

| Property | Meaning |
|---|---|
| `NoStreamUseAfterFree` | a blocking read/write is never in flight on a freed stream |
| `NoHandleUseAfterFree` | the handle is not freed under a thread about to use it |
| `NoDoubleFree` | the handle and each stream are freed at most once |
| `NoLeakAtEnd` | when everything has finished, everything was freed |
| `EventuallyTerminates` | liveness: no deadlock; every call, `Close` included, finishes |

The child is adversarial: it never exits by itself, never reads stdin and never
writes stdout, so the only thing that can unblock a stuck write is `Kill`.

## Scenarios

| Config | Design | Environment | Expected |
|---|---|---|---|
| `Fixed` | shipped (`Close` kills first) | nothing but `Close` ever kills | all properties hold |
| `FixedWithKiller` | shipped | plus a concurrent `Kill` / canceled context | all properties hold |
| `FixedChildExits` | shipped | the child may also exit on its own | all properties hold |
| `Original` | streams closed **before** the kill | nothing but `Close` ever kills | **deadlock** (asserted) |
| `OriginalWithKiller` | original | plus a concurrent `Kill` | holds, which is why the bug was subtle |

`Original` is a regression guard for the model itself: `scripts/check-formal.sh`
fails if TLC *stops* finding that deadlock, because a model that cannot find the
known bug proves nothing. The invariants were also mutation-checked: removing
the stream lock from `Close` makes `NoStreamUseAfterFree` fail.

## What this does and does not show

It checks the **design**, exhaustively over all interleavings of the modeled
actors (about 2,000 to 6,000 states per scenario). It does **not** verify the Go
or Java source, and it does not model the native library, only the contract
documented in `mxc_ffi`. The real code is covered by the race-detector tests
(`TestHardening_*`, `TestContract_ConcurrentKillWaitClose`) and, in Java,
`CleanerTest`; the model is what tells you those tests are asking the right
question.

Not modeled: output contents, timeouts, PTY, lifecycle phases, and more than one
reader or writer per stream (the API forbids it).
