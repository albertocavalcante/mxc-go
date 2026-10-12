package mxc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/albertocavalcante/mxc-go/internal/ffi"
)

// ContainerID identifies a provisioned persistent container (e.g. "wslc:0123...").
type ContainerID string

// ProvisionRequest creates a persistent container. Persistent (state-aware)
// containers are available on backends such as WSLC and IsolationSession
// (Windows); on other hosts Provision returns an unsupported/unavailable error.
type ProvisionRequest struct {
	Containment Containment `json:"containment"`
	Filesystem  *Filesystem `json:"filesystem,omitempty"`
	Network     *Network    `json:"network,omitempty"`
	Telemetry   *Telemetry  `json:"telemetry,omitempty"`

	// Extra merges backend-specific sections (e.g. "wslc") into the document.
	Extra map[string]json.RawMessage `json:"-"`
}

// ExecRequest runs a process in an existing container.
type ExecRequest struct {
	Process       Workload       `json:"process"`
	Network       *Network       `json:"network,omitempty"`
	RuntimeConfig *RuntimeConfig `json:"runtimeConfig,omitempty"`
	Telemetry     *Telemetry     `json:"telemetry,omitempty"`
}

func lifecycleEnvelope(phase string, id ContainerID, body any, extra map[string]json.RawMessage) (string, error) {
	type env struct {
		Version   string `json:"version"`
		Phase     string `json:"phase"`
		SandboxID string `json:"sandboxId,omitempty"`
	}
	hdr, _ := json.Marshal(env{SchemaVersion, phase, string(id)})
	var m map[string]json.RawMessage
	_ = json.Unmarshal(hdr, &m)
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		var bm map[string]json.RawMessage
		if err := json.Unmarshal(b, &bm); err != nil {
			return "", err
		}
		for k, v := range bm {
			m[k] = v
		}
	}
	for k, v := range extra {
		m[k] = v
	}
	// Reserved envelope keys always win over body/extra.
	var hm map[string]json.RawMessage
	_ = json.Unmarshal(hdr, &hm)
	for _, k := range []string{"version", "phase", "sandboxId"} {
		if v, ok := hm[k]; ok {
			m[k] = v
		} else {
			delete(m, k)
		}
	}
	b, err := json.Marshal(m)
	return string(b), err
}

func runPhase(ctx context.Context, js string, dry bool, o options) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, st, d := ffi.StateAwareJSON(js, dry, o.experimental)
	if st != 0 {
		return nil, newError(st, d)
	}
	return json.RawMessage(resp), nil
}

// Provision creates a persistent container and returns its ID.
func Provision(ctx context.Context, req ProvisionRequest, opts ...Option) (ContainerID, error) {
	js, err := lifecycleEnvelope("provision", "", req, req.Extra)
	if err != nil {
		return "", err
	}
	resp, err := runPhase(ctx, js, false, apply(opts))
	if err != nil {
		return "", err
	}
	var env struct {
		Result struct {
			SandboxID string `json:"sandboxId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &env); err != nil || env.Result.SandboxID == "" {
		return "", fmt.Errorf("mxc: provision response carried no sandboxId: %s", resp)
	}
	return ContainerID(env.Result.SandboxID), nil
}

// Start starts a provisioned container.
func Start(ctx context.Context, id ContainerID, opts ...Option) error {
	return simplePhase(ctx, "start", id, opts)
}

// Stop stops a started container.
func Stop(ctx context.Context, id ContainerID, opts ...Option) error {
	return simplePhase(ctx, "stop", id, opts)
}

// Deprovision destroys a container.
func Deprovision(ctx context.Context, id ContainerID, opts ...Option) error {
	return simplePhase(ctx, "deprovision", id, opts)
}

func simplePhase(ctx context.Context, phase string, id ContainerID, opts []Option) error {
	js, err := lifecycleEnvelope(phase, id, nil, nil)
	if err != nil {
		return err
	}
	_, err = runPhase(ctx, js, false, apply(opts))
	return err
}

// Validate performs a native dry run of a lifecycle phase
// ("provision"|"start"|"stop"|"deprovision") without changing anything, and
// returns the raw response envelope.
func Validate(ctx context.Context, phase string, id ContainerID, body any, opts ...Option) (json.RawMessage, error) {
	js, err := lifecycleEnvelope(phase, id, body, nil)
	if err != nil {
		return nil, err
	}
	return runPhase(ctx, js, true, apply(opts))
}

func execJSON(id ContainerID, req ExecRequest) (string, error) {
	return lifecycleEnvelope("exec", id, req, nil)
}

// Exec runs a process in a container to completion, capturing its output.
func Exec(ctx context.Context, id ContainerID, req ExecRequest, opts ...Option) (*Result, error) {
	js, err := execJSON(id, req)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return resultFrom(ffi.RunStateAwareExecJSON(js, apply(opts).experimental))
}

// SpawnExec runs a process in a container with live piped stdio.
func SpawnExec(ctx context.Context, id ContainerID, req ExecRequest, opts ...Option) (*Process, error) {
	js, err := execJSON(id, req)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sb, st, d := ffi.ExecStateAware(js, apply(opts).experimental)
	if sb == nil {
		return nil, newError(st, d)
	}
	return newProcess(ctx, sb), nil
}
