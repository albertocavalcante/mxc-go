------------------------------ MODULE ProcessHandle ------------------------------
(***************************************************************************)
(* Model of the native-handle protocol shared by mxc-go (Process) and      *)
(* mxc-java (MxcProcess). One sandboxed child, one native handle, a        *)
(* stdout stream and a stdin stream, each freed exactly once.              *)
(*                                                                         *)
(* Native rules the model enforces (from mxc_ffi's documented contract):   *)
(*   - calls on the sandbox handle are serialized by a handle lock `hl`;   *)
(*   - a stream is owned by one thread at a time, so a stream lock is held *)
(*     across each BLOCKING native read/write;                             *)
(*   - freeing a stream while a read/write is in flight is undefined       *)
(*     behavior (use-after-free);                                          *)
(*   - the stdout closer can unblock a read without killing the child, but *)
(*     nothing unblocks a write except the child dying.                    *)
(*                                                                         *)
(* The child is adversarial: it never exits by itself (unless              *)
(* ChildMayExit), never reads stdin and never writes stdout. The only way  *)
(* to unblock a stuck write is Kill. KillerRuns says whether anything other*)
(* than Close ever calls Kill (an explicit Kill, or a canceled context);   *)
(* FALSE is the hard case: Close alone must make progress.                 *)
(*                                                                         *)
(* Fixed = TRUE  : the shipped design. Close kills the process first.      *)
(* Fixed = FALSE : the original design. Close closes streams first.        *)
(*                                                                         *)
(* This is a model of the protocol, not a proof about the Go or Java       *)
(* source. TestHardening_* / CleanerTest exercise the real code against    *)
(* the same scenarios.                                                     *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS Fixed, ChildMayExit, KillerRuns

Actors == {"reader", "writer", "waiter", "killer", "closer", "cleaner"}
Closers == {"closer", "cleaner"}   \* explicit Close() and the GC/finalizer safety net
NONE == "none"

VARIABLES child, handle, outS, inS,   \* resource states
          hl, sl, wl,                 \* locks: owner or NONE
          outUnblock,                 \* stdout closer fired
          pc, frees

vars == <<child, handle, outS, inS, hl, sl, wl, outUnblock, pc, frees>>

Terminated == \A a \in Actors : pc[a] = "done"

TypeOK ==
  /\ child \in {"alive", "dead"}
  /\ handle \in {"live", "freed"} /\ outS \in {"open", "freed"} /\ inS \in {"open", "freed"}
  /\ hl \in Actors \cup {NONE} /\ sl \in Actors \cup {NONE} /\ wl \in Actors \cup {NONE}
  /\ outUnblock \in BOOLEAN
  /\ frees \in [h : 0..2, out : 0..2, in : 0..2]

Init ==
  /\ child = "alive" /\ handle = "live" /\ outS = "open" /\ inS = "open"
  /\ hl = NONE /\ sl = NONE /\ wl = NONE /\ outUnblock = FALSE
  /\ pc = [a \in Actors |-> CASE a = "reader" -> "r0"
                              [] a = "writer" -> "w0"
                              [] a = "waiter" -> "t0"
                              [] a = "killer" -> (IF KillerRuns THEN "k0" ELSE "done")
                              [] OTHER        -> IF Fixed THEN "c0" ELSE "c2"]
  /\ frees = [h |-> 0, out |-> 0, in |-> 0]

Step(a, to) == pc' = [pc EXCEPT ![a] = to]

(* ---- Reader: holds the stdout lock across a blocking native read ---- *)
Reader ==
  \/ pc["reader"] = "r0" /\ sl = NONE /\ sl' = "reader" /\ Step("reader", "r1")
       /\ UNCHANGED <<child, handle, outS, inS, hl, wl, outUnblock, frees>>
  \/ pc["reader"] = "r1" /\ outS = "freed"          \* already closed: EOF, no native call
       /\ sl' = NONE /\ Step("reader", "done")
       /\ UNCHANGED <<child, handle, outS, inS, hl, wl, outUnblock, frees>>
  \/ pc["reader"] = "r1" /\ outS = "open" /\ Step("reader", "r2")
       /\ UNCHANGED <<child, handle, outS, inS, hl, sl, wl, outUnblock, frees>>
  \/ pc["reader"] = "r2" /\ (child = "dead" \/ outUnblock)   \* blocking read returns
       /\ sl' = NONE /\ Step("reader", "done")
       /\ UNCHANGED <<child, handle, outS, inS, hl, wl, outUnblock, frees>>

(* ---- Writer: holds the stdin lock across a native write that cannot complete
        until the child dies (pipe full, child never reads) ---- *)
Writer ==
  \/ pc["writer"] = "w0" /\ wl = NONE /\ wl' = "writer" /\ Step("writer", "w1")
       /\ UNCHANGED <<child, handle, outS, inS, hl, sl, outUnblock, frees>>
  \/ pc["writer"] = "w1" /\ inS = "freed"
       /\ wl' = NONE /\ Step("writer", "done")
       /\ UNCHANGED <<child, handle, outS, inS, hl, sl, outUnblock, frees>>
  \/ pc["writer"] = "w1" /\ inS = "open" /\ Step("writer", "w2")
       /\ UNCHANGED <<child, handle, outS, inS, hl, sl, wl, outUnblock, frees>>
  \/ pc["writer"] = "w2" /\ child = "dead"
       /\ wl' = NONE /\ Step("writer", "done")
       /\ UNCHANGED <<child, handle, outS, inS, hl, sl, outUnblock, frees>>

(* ---- Waiter: a Wait/poll that takes the handle lock ---- *)
Waiter ==
  \/ pc["waiter"] = "t0" /\ hl = NONE /\ hl' = "waiter" /\ Step("waiter", "t1")
       /\ UNCHANGED <<child, handle, outS, inS, sl, wl, outUnblock, frees>>
  \/ pc["waiter"] = "t1"                             \* native try_wait only if live
       /\ hl' = NONE /\ Step("waiter", "done")
       /\ UNCHANGED <<child, handle, outS, inS, sl, wl, outUnblock, frees>>

(* ---- Killer: an explicit Kill() or the context watcher ---- *)
Killer ==
  \/ pc["killer"] = "k0" /\ hl = NONE /\ hl' = "killer" /\ Step("killer", "k1")
       /\ UNCHANGED <<child, handle, outS, inS, sl, wl, outUnblock, frees>>
  \/ pc["killer"] = "k1"
       /\ child' = (IF handle = "live" THEN "dead" ELSE child)
       /\ hl' = NONE /\ Step("killer", "done")
       /\ UNCHANGED <<handle, outS, inS, sl, wl, outUnblock, frees>>

(* ---- Close, run by `closer` and by `cleaner` (finalizer / Cleaner) ---- *)
Close(a) ==
  \/ pc[a] = "c0" /\ hl = NONE /\ hl' = a /\ Step(a, "c1")           \* take handle lock
       /\ UNCHANGED <<child, handle, outS, inS, sl, wl, outUnblock, frees>>
  \/ pc[a] = "c1"                                                    \* kill, release
       /\ child' = (IF handle = "live" THEN "dead" ELSE child)
       /\ hl' = NONE /\ Step(a, "c2")
       /\ UNCHANGED <<handle, outS, inS, sl, wl, outUnblock, frees>>
  \/ pc[a] = "c2" /\ outUnblock' = TRUE /\ Step(a, "c3")             \* stdout closer
       /\ UNCHANGED <<child, handle, outS, inS, hl, sl, wl, frees>>
  \/ pc[a] = "c3" /\ sl = NONE /\ sl' = a /\ Step(a, "c4")           \* stdout lock
       /\ UNCHANGED <<child, handle, outS, inS, hl, wl, outUnblock, frees>>
  \/ pc[a] = "c4"                                                    \* free stdout once
       /\ outS' = "freed"
       /\ frees' = [frees EXCEPT !.out = (IF outS = "open" THEN @ + 1 ELSE @)]
       /\ sl' = NONE /\ Step(a, "c5")
       /\ UNCHANGED <<child, handle, inS, hl, wl, outUnblock>>
  \/ pc[a] = "c5" /\ wl = NONE /\ wl' = a /\ Step(a, "c6")           \* stdin lock
       /\ UNCHANGED <<child, handle, outS, inS, hl, sl, outUnblock, frees>>
  \/ pc[a] = "c6"                                                    \* free stdin once
       /\ inS' = "freed"
       /\ frees' = [frees EXCEPT !.in = (IF inS = "open" THEN @ + 1 ELSE @)]
       /\ wl' = NONE /\ Step(a, "c7")
       /\ UNCHANGED <<child, handle, outS, hl, sl, outUnblock>>
  \/ pc[a] = "c7" /\ hl = NONE /\ hl' = a /\ Step(a, "c8")           \* handle lock
       /\ UNCHANGED <<child, handle, outS, inS, sl, wl, outUnblock, frees>>
  \/ pc[a] = "c8"                                                    \* free handle once
       /\ handle' = "freed"
       /\ frees' = [frees EXCEPT !.h = (IF handle = "live" THEN @ + 1 ELSE @)]
       /\ hl' = NONE /\ Step(a, "done")
       /\ UNCHANGED <<child, outS, inS, sl, wl, outUnblock>>

Exit ==
  /\ ChildMayExit /\ child = "alive" /\ child' = "dead"
  /\ UNCHANGED <<handle, outS, inS, hl, sl, wl, outUnblock, pc, frees>>

Done == Terminated /\ UNCHANGED vars

Next == Reader \/ Writer \/ Waiter \/ Killer \/ Close("closer") \/ Close("cleaner")
        \/ Exit \/ Done

Spec == Init /\ [][Next]_vars
        /\ WF_vars(Reader) /\ WF_vars(Writer) /\ WF_vars(Waiter) /\ WF_vars(Killer)
        /\ WF_vars(Close("closer")) /\ WF_vars(Close("cleaner"))

(***************************** Properties ***********************************)

\* A blocking read/write is never in flight on a freed stream.
NoStreamUseAfterFree ==
  /\ pc["reader"] = "r2" => outS = "open"
  /\ pc["writer"] = "w2" => inS = "open"

\* The handle is never freed while another actor holds the handle lock and is
\* about to use it.
NoHandleUseAfterFree ==
  /\ (pc["waiter"] = "t1" \/ pc["killer"] = "k1") => handle = "live" \/ hl # NONE

\* Each native resource is freed at most once.
NoDoubleFree == frees.h <= 1 /\ frees.out <= 1 /\ frees.in <= 1

\* When everything has finished, nothing leaked.
NoLeakAtEnd == Terminated => handle = "freed" /\ outS = "freed" /\ inS = "freed"

\* Every actor, Close in particular, eventually finishes (no deadlock).
EventuallyTerminates == <>Terminated
===============================================================================
