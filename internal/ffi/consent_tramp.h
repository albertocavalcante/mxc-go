#ifndef MXC_GO_CONSENT_TRAMP_H
#define MXC_GO_CONSENT_TRAMP_H
#include "mxc_ffi.h"

// Returns the C-callable presenter that forwards into Go (goConsentPresenter).
MxcTelemetryConsentPresenter mxc_go_consent_presenter(void);
#endif
