package mxc

import (
	"encoding/json"

	"github.com/albertocavalcante/mxc-go/internal/ffi"
)

func telemetryCall(f func() (string, int)) (json.RawMessage, error) {
	s, st := f()
	if st != 0 {
		return nil, &Error{Code: Code(st), Message: "telemetry operation failed"}
	}
	return json.RawMessage(s), nil
}

// TelemetryConsent returns the persisted telemetry consent record (JSON).
func TelemetryConsent() (json.RawMessage, error) { return telemetryCall(ffi.TelemetryConsent) }

// TelemetryConsentStatus returns the consent status (JSON).
func TelemetryConsentStatus() (json.RawMessage, error) {
	return telemetryCall(ffi.TelemetryConsentStatus)
}

// TelemetryPolicy returns the administrative telemetry policy state (JSON).
func TelemetryPolicy() (json.RawMessage, error) { return telemetryCall(ffi.TelemetryPolicy) }

// TelemetryWithdrawConsent withdraws any persisted consent.
func TelemetryWithdrawConsent() (json.RawMessage, error) {
	return telemetryCall(ffi.TelemetryWithdraw)
}
