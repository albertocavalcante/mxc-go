package ffi

/*
#include <stdint.h>
#include <stdlib.h>
#include "consent_tramp.h"
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"unsafe"
)

// Presenter decisions, as returned across the C boundary.
const (
	DecisionNo        = 0
	DecisionYes       = 1
	DecisionDismissed = 2
	DecisionError     = -1
)

// Presenter shows a consent prompt (given as canonical JSON) and returns one of
// the Decision* values. A non-nil error is reported as DecisionError.
type Presenter func(promptJSON string) (int, error)

// dispatch runs a presenter and converts every outcome, including a panic, to a
// value that is safe to return across the C boundary: a Go panic must never
// unwind into native code.
func dispatch(p Presenter, promptJSON string) (decision int) {
	defer func() {
		if recover() != nil {
			decision = DecisionError
		}
	}()
	d, err := p(promptJSON)
	if err != nil {
		return DecisionError
	}
	switch d {
	case DecisionNo, DecisionYes, DecisionDismissed:
		return d
	default:
		return DecisionError
	}
}

//export goConsentPresenter
func goConsentPresenter(prompt *C.char, context unsafe.Pointer) C.int32_t {
	if context == nil || prompt == nil {
		return DecisionError
	}
	h := cgo.Handle(*(*C.uintptr_t)(context))
	p, ok := h.Value().(Presenter)
	if !ok {
		return DecisionError
	}
	return C.int32_t(dispatch(p, C.GoString(prompt)))
}

// NeedsConsentPrompt reports whether the host should show the consent prompt.
func NeedsConsentPrompt() (needs bool, status int) {
	var out C.int32_t
	st := C.mxc_telemetry_needs_consent_prompt(&out)
	return out != 0, int(st)
}

// RequestConsent runs the consent flow. The presenter is invoked synchronously,
// on the calling goroutine's thread, only when the host needs a prompt.
func RequestConsent(locale string, p Presenter) (outcomeJSON string, status int) {
	var loc *C.char
	if locale != "" {
		loc = C.CString(locale)
		defer C.free(unsafe.Pointer(loc))
	}
	// The context lives in C memory (holding a cgo.Handle) so no Go pointer is
	// retained by native code.
	ctx := (*C.uintptr_t)(C.malloc(C.size_t(unsafe.Sizeof(C.uintptr_t(0)))))
	defer C.free(unsafe.Pointer(ctx))
	h := cgo.NewHandle(p)
	defer h.Delete()
	*ctx = C.uintptr_t(h)

	var out *C.char
	st := C.mxc_telemetry_request_consent(loc, C.mxc_go_consent_presenter(), unsafe.Pointer(ctx), &out)
	return ownedString(out), int(st)
}

// decisionName renders a presenter decision for diagnostics.
func decisionName(d int) string {
	switch d {
	case DecisionNo:
		return "no"
	case DecisionYes:
		return "yes"
	case DecisionDismissed:
		return "dismissed"
	default:
		return fmt.Sprintf("error(%d)", d)
	}
}
