# Examples

Runnable programs that use the library the way a real application would. All of
them need `libmxc_ffi` (see the main README) and run against the real sandbox.

| Example | What it shows |
|---|---|
| [`hello`](hello) | The smallest possible `mxc.Run`. |
| [`sandbox-run`](sandbox-run) | A `firejail`-style CLI: run any command with `--ro`, `--rw`, `--deny`, `--net` and `--timeout`; stdio passes through and exit codes propagate. |
| [`codeinterp`](codeinterp) | A code-interpreter HTTP service for untrusted snippets (LLM output, plugins), with a hostile test suite. |

```sh
just native && export CGO_LDFLAGS="-L$PWD/.mxc/src/target/release -Wl,-rpath,$PWD/.mxc/src/target/release"
go run ./examples/sandbox-run --rw . -- sh -c 'echo hi > out.txt && cat out.txt'
go run ./examples/codeinterp &        # listens on 127.0.0.1:8080
curl -s localhost:8080/v1/run -d '{"language":"python","code":"print(sum(range(10)))"}'
curl -sN localhost:8080/v1/run/stream -d '{"language":"sh","code":"echo one; sleep 1; echo two"}'
```

## `codeinterp`: what the policy does

Per request, the service writes the code into a fresh private directory and runs
it with:

- **Filesystem**: the work directory is the only writable path; operator-chosen
  secret paths (default: your home directory) are denied; the interpreter's own
  directories are readable.
- **Network**: all egress, ingress and host-loopback denied.
- **Environment**: an explicit minimal one, so the service's own credentials are
  never inherited.
- **Limits**: wall-clock timeout (MXC), plus CPU seconds, file size and open
  files via `ulimit`, an output cap per stream that **kills** a flooding
  producer, a concurrency cap (`429` when busy), and a request body cap.
- **Cleanup**: the work directory is removed, and the whole process tree is
  killed when the client disconnects, on timeout, and when a background child
  would otherwise hold the response open.

The test suite (`go test ./examples/codeinterp`) drives the service with hostile
snippets and asserts the attacks fail: reading a canary secret, escaping through
a symlink, writing outside the work directory, connecting to a local listener and
to the internet, reading a canary environment variable, spinning forever,
flooding output, leaving background processes behind, and holding the response
open from a child. The policy was mutation-checked: weakening each rule makes the
matching test fail.

### Lessons from building it

- MXC enforces filesystem and network policy and a wall-clock timeout. It does
  **not** cap memory, CPU or process count: a 2,000-process fork storm succeeds.
  Add `ulimit`s, and for real resource isolation run the service in a container
  or VM. Never expose it beyond loopback without authentication.
- Homebrew interpreters link libraries across their whole prefix, so the policy
  grants that prefix read-only.
- The Seatbelt default already denies reads outside granted paths, but a Linux
  backend may differ: keep secrets in `-deny` explicitly rather than relying on
  the default.
- Local addresses are governed by the host-loopback rule, so proving that egress
  is blocked needs an external destination (the test checks the test host can
  reach it unsandboxed first, so it cannot pass vacuously).
