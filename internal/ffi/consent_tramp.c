#include "consent_tramp.h"

// Implemented in Go with //export (consent.go).
extern int32_t goConsentPresenter(char *prompt, void *context);

static int32_t trampoline(const char *prompt, void *context) {
  return goConsentPresenter((char *)prompt, context);
}

MxcTelemetryConsentPresenter mxc_go_consent_presenter(void) { return trampoline; }
