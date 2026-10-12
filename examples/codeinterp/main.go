// Command codeinterp is a small code-interpreter service: it runs untrusted
// snippets (for example LLM-generated code) inside an MXC sandbox and returns
// the result over HTTP.
//
//	POST /v1/run         {"language":"python","code":"print(1+1)"}  -> JSON result
//	POST /v1/run/stream  same request                               -> Server-Sent Events
//	GET  /healthz                                                   -> sandbox support
//
// The policy (see runner.go) is operator-controlled: private per-request work
// directory, no network, explicit empty-ish environment, secret paths denied,
// output and time caps. Run it as an unprivileged user, ideally inside a
// container or VM, because MXC itself does not limit memory or process count.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	mxc "github.com/albertocavalcante/mxc-go"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address (bind loopback unless you add auth)")
	work := flag.String("work", filepath.Join(os.TempDir(), "codeinterp"), "directory for per-request work dirs")
	deny := flag.String("deny", defaultDeny(), "comma-separated paths the sandbox must never read")
	flag.Parse()

	cfg, err := newConfig(*work, splitList(*deny))
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Addr: *addr, Handler: newHandler(cfg), ReadHeaderTimeout: 5 * time.Second}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	log.Printf("codeinterp listening on http://%s (mxc native %s)", *addr, mxc.NativeVersion())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func defaultDeny() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func newHandler(cfg *config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		ps, err := mxc.PlatformSupportInfo()
		if err != nil || !ps.IsSupported {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "support": ps})
			return
		}
		langs := make([]string, 0, len(cfg.interpreters))
		for l := range cfg.interpreters {
			langs = append(langs, l)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "backends": ps.AvailableMethods, "languages": langs})
	})
	mux.HandleFunc("POST /v1/run", func(w http.ResponseWriter, r *http.Request) {
		rq, ok := decode(w, r)
		if !ok {
			return
		}
		res, err := cfg.execute(r.Context(), rq, nil)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /v1/run/stream", func(w http.ResponseWriter, r *http.Request) {
		rq, ok := decode(w, r)
		if !ok {
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		event := func(name string, v any) {
			b, _ := json.Marshal(v)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
			fl.Flush()
		}
		res, err := cfg.execute(r.Context(), rq, func(stream string, b []byte) {
			event(stream, map[string]string{"data": string(b)})
		})
		if err != nil {
			event("error", map[string]string{"error": err.Error()})
			return
		}
		res.Stdout, res.Stderr = "", "" // already streamed
		event("result", res)
	})
	return mux
}

func decode(w http.ResponseWriter, r *http.Request) (runRequest, bool) {
	var rq runRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rq); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return rq, false
	}
	return rq, true
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, errBusy):
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
	default:
		log.Printf("run failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "sandbox failure"})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
