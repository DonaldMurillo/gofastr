package pmtestkit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// EnvName is the env var gating the in-test child binary: a suite's
// TestMain re-execs the test binary with it set to a Mode value.
const EnvName = "GOFASTR_PROCESS_MODULE_CHILD"

// Mode enumerates the behavior presets the child honors. The harness
// sets one via [EnvName].
type Mode string

const (
	ModeEcho      Mode = "echo"       // happy path: handshake, ready, echo http
	ModeBadDigest Mode = "bad_digest" // surface_sha256 mismatch → terminal
	ModeSlow      Mode = "slow"       // http handler sleeps so kill -9 lands mid-call
	ModeCrashExit Mode = "crash_exit" // exit 1 immediately on each spawn
	ModeFault     Mode = "fault"      // module.http answers with an unsolicited response
	// ModeCrashInHandshake exits 1 from inside the handshake handler, so
	// the host's handshake call fails on the transport (peer closed /
	// EOF / EPIPE) rather than through a verdict — the failure
	// ModeCrashExit only hits by accident under load, made
	// deterministic.
	ModeCrashInHandshake Mode = "crash_in_handshake"
)

// Env returns the env block for re-executing the test binary as a module
// child in the given mode. PATH is preserved so the child can exec.
func Env(mode Mode, extra map[string]string) []string {
	base := []string{
		EnvName + "=" + string(mode),
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + os.Getenv("TMPDIR"),
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}
	for k, v := range extra {
		base = append(base, k+"="+v)
	}
	return base
}

// ChildMain wires a moduleproto Peer over stdin/stdout and serves the
// configured Mode behavior (handshake/ready/http/drain handlers, then
// peer.Start). A suite's TestMain calls it via [MaybeChild] when EnvName
// is set; it returns the exit code the process should use.
func ChildMain(mode Mode) int {
	codec, err := moduleproto.NewCodec(os.Stdin, os.Stdout, moduleproto.DefaultMaxFrameBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "child: codec:", err)
		return 1
	}
	peer := moduleproto.NewPeer(codec, moduleproto.RoleChild)
	// Handshake handler: echo expected, with per-mode digest behavior.
	if err := peer.Handle(moduleproto.MethodHandshake, func(_ context.Context, params json.RawMessage) (any, error) {
		var hp moduleproto.HandshakeParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &hp); err != nil {
				return nil, err
			}
		}
		surface := hp.Expected.SurfaceSHA256
		if mode == ModeBadDigest {
			surface = "DIFFERENT"
		}
		if mode == ModeCrashInHandshake {
			os.Exit(1)
		}
		return moduleproto.HandshakeResult{
			Proto: moduleproto.ProtoRange{Min: 1, Max: 1},
			Identity: moduleproto.Identity{
				Name:              hp.Expected.Name,
				Version:           hp.Expected.Version,
				InstanceID:        hp.Expected.InstanceID,
				DesiredGeneration: hp.Expected.DesiredGeneration,
			},
			SurfaceSHA256: surface,
			Features:      nil,
			Ready:         false,
		}, nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "child: register handshake:", err)
		return 1
	}
	// Ready handler.
	if err := peer.Handle(moduleproto.MethodReady, func(context.Context, json.RawMessage) (any, error) {
		return moduleproto.ReadyResult{Ready: true}, nil
	}); err != nil {
		return 1
	}
	// HTTP handler.
	if err := peer.Handle(moduleproto.MethodHTTP, func(_ context.Context, params json.RawMessage) (any, error) {
		var p moduleproto.HTTPRequestParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		switch mode {
		case ModeSlow:
			// Sleep so a kill -9 lands mid-call.
			time.Sleep(2 * time.Second)
		case ModeFault:
			// A response to an id the host never sent faults the host peer.
			id := uint64(1 << 40)
			_ = codec.WriteFrame(&moduleproto.Frame{JSONRPC: "2.0", ID: &id, Result: json.RawMessage("1")})
		}
		body, _ := json.Marshal(map[string]any{"route": p.RouteID, "ok": true})
		return moduleproto.HTTPResponseResult{
			Status:  http.StatusOK,
			Headers: map[string]string{"X-Child": "demo"},
			Body: moduleproto.HTTPResponseBody{
				Kind:  moduleproto.BodyKindJSON,
				Value: body,
			},
		}, nil
	}); err != nil {
		return 1
	}
	// Drain handler.
	_ = peer.Handle(moduleproto.MethodDrain, func(context.Context, json.RawMessage) (any, error) {
		return moduleproto.DrainResult{Inflight: 0}, nil
	})
	peer.Start()
	if mode == ModeCrashExit {
		// Exit immediately on spawn. The host's exit watcher catches it.
		time.Sleep(20 * time.Millisecond) // let the read loop start
		os.Exit(1)
	}
	<-peer.Done()
	return 0
}

// MaybeChild runs [ChildMain] and exits when EnvName is set (a child
// re-exec); it reports false for the normal test run.
func MaybeChild() bool {
	if mode := Mode(os.Getenv(EnvName)); mode != "" {
		// This is a child re-exec. Run the child loop and exit.
		os.Exit(ChildMain(mode))
	}
	return false
}
