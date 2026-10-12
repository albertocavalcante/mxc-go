package mxc

import (
	"encoding/json"
	"time"
)

// SchemaVersion is the exact MXC configuration contract this package emits.
const SchemaVersion = "1.0.0"

// Containment selects a backend. Leave empty for the platform default.
type Containment string

const (
	ContainmentDefault          Containment = ""
	ContainmentProcess          Containment = "process"
	ContainmentProcessContainer Containment = "processcontainer"
	ContainmentLXC              Containment = "lxc"
	ContainmentBubblewrap       Containment = "bubblewrap"
	ContainmentSeatbelt         Containment = "seatbelt"
	ContainmentIsolationSession Containment = "isolation_session"
	ContainmentWSLC             Containment = "wslc"
)

// Action is an allow/deny decision for network rules.
type Action string

const (
	Allow Action = "allow"
	Deny  Action = "deny"
)

// Protocol is a transport protocol in a port rule.
type Protocol string

const (
	TCP  Protocol = "tcp"
	UDP  Protocol = "udp"
	ICMP Protocol = "icmp"
	Any  Protocol = "any"
)

// Clipboard is a clipboard access level.
type Clipboard string

const (
	ClipboardNone  Clipboard = "none"
	ClipboardRead  Clipboard = "read"
	ClipboardWrite Clipboard = "write"
	ClipboardAll   Clipboard = "all"
)

// Workload describes the process to execute.
type Workload struct {
	// CommandLine is the (non-empty) command line to execute.
	CommandLine string
	Cwd         string
	// Env is KEY=VALUE entries. nil selects the backend's default environment;
	// a non-nil empty slice is sent as an explicit empty environment.
	Env []string
	// InheritDefaultEnv layers Env on top of the backend's default environment.
	InheritDefaultEnv bool
	// Timeout kills the workload after this long (millisecond resolution). 0 = none.
	Timeout time.Duration
}

func (p Workload) MarshalJSON() ([]byte, error) {
	w := struct {
		CommandLine       string    `json:"commandLine"`
		Cwd               string    `json:"cwd,omitempty"`
		Env               *[]string `json:"env,omitempty"`
		InheritDefaultEnv bool      `json:"inheritDefaultEnv,omitempty"`
		Timeout           *int64    `json:"timeout,omitempty"`
	}{CommandLine: p.CommandLine, Cwd: p.Cwd, InheritDefaultEnv: p.InheritDefaultEnv}
	if p.Env != nil {
		w.Env = &p.Env
	}
	if p.Timeout > 0 {
		ms := p.Timeout.Milliseconds()
		if ms == 0 {
			ms = 1
		}
		w.Timeout = &ms
	}
	return json.Marshal(w)
}

// Filesystem is the filesystem policy.
type Filesystem struct {
	ReadonlyPaths  []string `json:"readonlyPaths,omitempty"`
	ReadwritePaths []string `json:"readwritePaths,omitempty"`
	DeniedPaths    []string `json:"deniedPaths,omitempty"`
}

type NetworkPeer struct {
	CIDR   string   `json:"cidr"`
	Except []string `json:"except,omitempty"`
}

type NetworkPort struct {
	Protocol Protocol `json:"protocol,omitempty"`
	Port     int      `json:"port,omitempty"`
	EndPort  int      `json:"endPort,omitempty"`
}

type NetworkRule struct {
	To    []NetworkPeer `json:"to,omitempty"`
	Ports []NetworkPort `json:"ports,omitempty"`
}

type NetworkEgress struct {
	Default Action        `json:"default,omitempty"`
	Allow   []NetworkRule `json:"allow,omitempty"`
	Deny    []NetworkRule `json:"deny,omitempty"`
}

type NetworkIngress struct {
	Default      Action `json:"default,omitempty"`
	HostLoopback Action `json:"hostLoopback,omitempty"`
}

// Network is the directional network policy. Omitted directions default to deny.
type Network struct {
	Egress  *NetworkEgress  `json:"egress,omitempty"`
	Ingress *NetworkIngress `json:"ingress,omitempty"`
}

// UI is the cross-platform user-interface policy.
type UI struct {
	Clipboard Clipboard `json:"clipboard,omitempty"`
	Disable   *bool     `json:"disable,omitempty"`
	Injection *bool     `json:"injection,omitempty"`
}

// Telemetry opts one run into diagnostic telemetry (still subject to consent).
type Telemetry struct {
	Enabled bool `json:"enabled"`
}

// Seatbelt holds macOS-specific settings.
type Seatbelt struct {
	ExtraMachLookups []string `json:"extraMachLookups,omitempty"`
	GUIAccess        *bool    `json:"guiAccess,omitempty"`
	KeychainAccess   *bool    `json:"keychainAccess,omitempty"`
	NestedPTY        *bool    `json:"nestedPty,omitempty"`
	ProfileOverride  string   `json:"profileOverride,omitempty"`
}

// LXC holds Linux LXC distribution settings.
type LXC struct {
	Distribution string `json:"distribution"`
	Release      string `json:"release"`
}

// RuntimeConfig carries runtime values such as a cooperative proxy URL.
type RuntimeConfig struct {
	NetworkProxy string `json:"networkProxy,omitempty"`
}

// Request is a create-and-run container request.
//
// Backend sections not modeled here (processContainer, wslc, ...) can be
// supplied through Extra, whose top-level keys are merged into the document.
type Request struct {
	Containment   Containment    `json:"containment,omitempty"`
	Process       Workload       `json:"process"`
	Filesystem    *Filesystem    `json:"filesystem,omitempty"`
	Network       *Network       `json:"network,omitempty"`
	UI            *UI            `json:"ui,omitempty"`
	Telemetry     *Telemetry     `json:"telemetry,omitempty"`
	Seatbelt      *Seatbelt      `json:"seatbelt,omitempty"`
	LXC           *LXC           `json:"lxc,omitempty"`
	RuntimeConfig *RuntimeConfig `json:"runtimeConfig,omitempty"`

	Extra map[string]json.RawMessage `json:"-"`
}

// JSON renders the exact MXC request JSON (with "version") for the request.
func (r Request) JSON() (string, error) {
	type alias Request
	return marshalMerged(struct {
		Version string `json:"version"`
		alias
	}{SchemaVersion, alias(r)}, r.Extra)
}

func marshalMerged(v any, extra map[string]json.RawMessage) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if len(extra) == 0 {
		return string(b), nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return "", err
	}
	for k, val := range extra {
		m[k] = val
	}
	b, err = json.Marshal(m)
	return string(b), err
}

// Bool returns a pointer to b, for the optional *bool policy fields.
func Bool(b bool) *bool { return &b }
