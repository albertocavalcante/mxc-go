package mxc

import (
	"encoding/json"

	"github.com/albertocavalcante/mxc-go/internal/ffi"
)

// NativeVersion returns the version of the loaded mxc_ffi library.
func NativeVersion() string { return ffi.Version() }

// PlatformSupport reports whether this SDK can launch on the host.
type PlatformSupport struct {
	IsSupported      bool     `json:"isSupported"`
	Reason           *string  `json:"reason"`
	AvailableMethods []string `json:"availableMethods"`
}

// AvailableBackend is a host-available backend. Availability is advisory; the
// remaining fields are exposed raw because the native shape grows over time.
type AvailableBackend struct {
	Backend string          `json:"backend"`
	Raw     json.RawMessage `json:"-"`
}

// PlatformSupportInfo probes the host for launchable backends.
func PlatformSupportInfo() (PlatformSupport, error) {
	var ps PlatformSupport
	err := json.Unmarshal([]byte(ffi.PlatformSupportJSON()), &ps)
	return ps, err
}

// AvailableBackends lists backends currently available on this host.
func AvailableBackends() ([]AvailableBackend, error) {
	raw := ffi.AvailableBackendsJSON()
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, err
	}
	out := make([]AvailableBackend, 0, len(items))
	for _, it := range items {
		var b AvailableBackend
		if err := json.Unmarshal(it, &b); err != nil {
			return nil, err
		}
		b.Raw = it
		out = append(out, b)
	}
	return out, nil
}
