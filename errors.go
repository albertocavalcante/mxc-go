package mxc

import (
	"errors"
	"fmt"
)

// Code classifies an MXC failure. Values 1-12 mirror mxc_sdk::v1::ErrorCode;
// the rest are FFI-local.
type Code int

const (
	CodeMalformedRequest       Code = 1
	CodeUnsupportedContainment Code = 2
	CodeUnsupportedPhase       Code = 3
	CodeBackendUnavailable     Code = 4
	CodeMalformedID            Code = 5
	CodeStaleID                Code = 6
	CodeNotProvisioned         Code = 7
	CodeNotStarted             Code = 8
	CodeAlreadyStarted         Code = 9
	CodeAlreadyStopped         Code = 10
	CodePolicyValidation       Code = 11
	CodeBackendError           Code = 12
	CodeNullArgument           Code = 100
	CodeInvalidUTF8            Code = 101
	CodePanic                  Code = 102
	CodeConsentWriteFailed     Code = 103
)

var codeNames = map[Code]string{
	1: "malformed_request", 2: "unsupported_containment", 3: "unsupported_phase",
	4: "backend_unavailable", 5: "malformed_id", 6: "stale_id", 7: "not_provisioned",
	8: "not_started", 9: "already_started", 10: "already_stopped",
	11: "policy_validation", 12: "backend_error", 100: "null_argument",
	101: "invalid_utf8", 102: "panic", 103: "consent_write_failed",
}

func (c Code) String() string {
	if n, ok := codeNames[c]; ok {
		return n
	}
	return fmt.Sprintf("code(%d)", int(c))
}

// Error is a failure reported by the native MXC engine.
type Error struct {
	Code        Code
	Message     string
	Operation   string // failing API call, when known
	NativeCode  string // platform status, e.g. 0x80070490
	Remediation string // actionable hint, when provided
}

func (e *Error) Error() string {
	s := fmt.Sprintf("mxc: %s: %s", e.Code, e.Message)
	if e.Operation != "" {
		s += " (" + e.Operation
		if e.NativeCode != "" {
			s += " " + e.NativeCode
		}
		s += ")"
	}
	if e.Remediation != "" {
		s += ": " + e.Remediation
	}
	return s
}

// HasCode reports whether err wraps an *Error with the given code.
func HasCode(err error, c Code) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == c
}
