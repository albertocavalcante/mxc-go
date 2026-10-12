package mxc

import (
	"encoding/json"
	"errors"

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

// ConsentDecision is the user's answer to the consent prompt.
type ConsentDecision int

// Possible answers a [ConsentPresenter] can return.
const (
	ConsentNo        ConsentDecision = ffi.DecisionNo
	ConsentYes       ConsentDecision = ffi.DecisionYes
	ConsentDismissed ConsentDecision = ffi.DecisionDismissed
)

// ConsentMessage is one localized string of the prompt.
type ConsentMessage struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ConsentPrompt is the canonical telemetry consent prompt the host must show.
type ConsentPrompt struct {
	ResourceVersion  string         `json:"resourceVersion"`
	Locale           string         `json:"locale"`
	Title            ConsentMessage `json:"title"`
	Body             ConsentMessage `json:"body"`
	AffirmativeLabel ConsentMessage `json:"affirmativeLabel"`
	NegativeLabel    ConsentMessage `json:"negativeLabel"`
	LearnMoreLabel   ConsentMessage `json:"learnMoreLabel"`
	LearnMoreURL     string         `json:"learnMoreUrl"`
}

// ConsentPresenter shows the prompt to the user and returns their decision. It
// is called synchronously during [RequestConsent]. Returning an error, or
// panicking, is reported to MXC as a presenter failure (never as consent).
type ConsentPresenter func(ConsentPrompt) (ConsentDecision, error)

// TelemetryNeedsConsentPrompt reports whether the host should show the prompt.
func TelemetryNeedsConsentPrompt() (bool, error) {
	needs, st := ffi.NeedsConsentPrompt()
	if st != 0 {
		return false, &Error{Code: Code(st), Message: "telemetry needs-prompt failed"}
	}
	return needs, nil
}

// RequestConsent runs the telemetry consent flow, invoking present only when a
// prompt is needed. locale may be empty. The outcome is returned as JSON; on
// non-Windows hosts it reports "notApplicable" without calling present.
func RequestConsent(locale string, present ConsentPresenter) (json.RawMessage, error) {
	if present == nil {
		return nil, errors.New("mxc: RequestConsent needs a presenter")
	}
	p := func(promptJSON string) (int, error) {
		var prompt ConsentPrompt
		if err := json.Unmarshal([]byte(promptJSON), &prompt); err != nil {
			return ffi.DecisionError, err
		}
		d, err := present(prompt)
		return int(d), err
	}
	out, st := ffi.RequestConsent(locale, p)
	if st != 0 {
		return nil, &Error{Code: Code(st), Message: "telemetry consent request failed"}
	}
	return json.RawMessage(out), nil
}
