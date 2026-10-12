package mxc

import (
	"encoding/json"

	"github.com/albertocavalcante/mxc-go/internal/ffi"
)

// ConsentState is the effective telemetry consent for this host.
type ConsentState string

// Effective telemetry consent states. Non-Windows hosts report NotApplicable.
const (
	ConsentGranted       ConsentState = "granted"
	ConsentDenied        ConsentState = "denied"
	ConsentUndetermined  ConsentState = "undetermined"
	ConsentNotApplicable ConsentState = "not-applicable"
)

// TelemetryPolicyState is the administrative telemetry policy for this host.
type TelemetryPolicyState string

// Administrative telemetry policy states.
const (
	PolicyUnrestricted  TelemetryPolicyState = "unrestricted"
	PolicyAllowed       TelemetryPolicyState = "allowed"
	PolicyBlocked       TelemetryPolicyState = "blocked"
	PolicyNotApplicable TelemetryPolicyState = "not-applicable"
)

func telemetryCall(op string, f func() (string, int)) (string, error) {
	s, st := f()
	if st != 0 {
		return "", &Error{Code: Code(st), Message: "telemetry " + op + " failed"}
	}
	return s, nil
}

// TelemetryConsent returns the effective consent state.
func TelemetryConsent() (ConsentState, error) {
	s, err := telemetryCall("consent", ffi.TelemetryConsent)
	return ConsentState(s), err
}

// TelemetryPolicy returns the administrative policy state.
func TelemetryPolicy() (TelemetryPolicyState, error) {
	s, err := telemetryCall("policy", ffi.TelemetryPolicy)
	return TelemetryPolicyState(s), err
}

// TelemetryConsentStatus returns the persisted and effective consent/policy
// snapshot as JSON.
func TelemetryConsentStatus() (json.RawMessage, error) {
	s, err := telemetryCall("status", ffi.TelemetryConsentStatus)
	return json.RawMessage(s), err
}

// TelemetryWithdrawConsent withdraws any persisted consent and returns the
// outcome as JSON.
func TelemetryWithdrawConsent() (json.RawMessage, error) {
	s, err := telemetryCall("withdraw", ffi.TelemetryWithdraw)
	return json.RawMessage(s), err
}
