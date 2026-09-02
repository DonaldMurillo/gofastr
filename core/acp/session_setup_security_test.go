package acp_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// recordingLoader returns a fakeLoadingAgent whose loader records that
// it ran (and with what cwd).
func recordingLoader(cwdSeen *atomic.Value) *fakeLoadingAgent {
	return &fakeLoadingAgent{loadFn: func(_ context.Context, _, cwd string, _ *acp.Client) (acp.Session, error) {
		cwdSeen.Store(cwd)
		return &fakeSession{id: "s1"}, nil
	}}
}

// Property: session-setup validation (cwd absolute, no mcpServers, no
// additionalDirectories) is a property of the SETUP step, not of one
// method — session/new and session/load share checkSessionSetup, so
// every malformed shape must be refused at BOTH entry points. The load
// path had no coverage before this file; a future refactor that
// revalidates only session/new would quietly reopen session/load.
//
// Each cwd shape is a distinct non-absolute class: empty, bare
// relative, dot-relative, home-tilde (expanded by shells, not the
// server), and a Windows drive path (not absolute on the host's GOOS).
func TestSessionSetupCwdParity(t *testing.T) {
	cwds := []string{
		"",
		"relative/dir",
		"./here",
		"~/proj",
		`C:\Users\x`,
	}

	for _, cwd := range cwds {
		t.Run("new+load cwd="+cwd, func(t *testing.T) {
			var loaded atomic.Value

			d := startDialog(t, recordingLoader(&loaded), nil)
			d.initialize()
			d.request(2, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
			e := respError(t, d.untilResponseID(2))
			if code, ok := e["code"].(float64); !ok || int(code) != acp.ErrInvalidParams {
				t.Errorf("SECURITY: [acp-setup] session/new accepted cwd %q (error %v); non-absolute cwd must be InvalidParams at every setup entry point.", cwd, e)
			}

			d.request(3, "session/load", map[string]any{"sessionId": "known", "cwd": cwd, "mcpServers": []any{}})
			e = respError(t, d.untilResponseID(3))
			if code, ok := e["code"].(float64); !ok || int(code) != acp.ErrInvalidParams {
				t.Errorf("SECURITY: [acp-setup] session/load accepted cwd %q (error %v); the shared setup check must refuse it like session/new does.", cwd, e)
			}
			if loaded.Load() != nil {
				t.Error("loader ran despite an invalid cwd — validation must gate the embedder call")
			}
		})
	}
}

// Property: capabilities this server does not advertise (MCP server
// connections, additionalDirectories) are refused at session/load too,
// not silently accepted-and-dropped — accepting them would promise the
// client a confinement boundary (extra directories) that never exists:
// file access the client believed scoped would happen with none of the
// scoping.
func TestSessionLoadRejectsUnadvertisedParams(t *testing.T) {
	params := map[string]map[string]any{
		"mcpServers": {
			"cwd":        "/tmp/proj",
			"mcpServers": []any{map[string]any{"name": "evil"}},
		},
		"additionalDirectories": {
			"cwd":                   "/tmp/proj",
			"additionalDirectories": []string{"/etc"},
		},
	}
	for name, p := range params {
		t.Run(name, func(t *testing.T) {
			var loaded atomic.Value
			d := startDialog(t, recordingLoader(&loaded), nil)
			d.initialize()

			full := map[string]any{"sessionId": "known"}
			for k, v := range p {
				full[k] = v
			}
			d.request(2, "session/load", full)
			e := respError(t, d.untilResponseID(2))
			if code, ok := e["code"].(float64); !ok || int(code) != acp.ErrInvalidParams {
				t.Errorf("SECURITY: [acp-setup] session/load accepted %s (error %v); unadvertised capability params must be InvalidParams.", name, e)
			}
			if loaded.Load() != nil {
				t.Errorf("loader ran despite %s being passed", name)
			}
		})
	}
}

// Property: a session created under one (valid) cwd cannot be re-loaded
// under a DIFFERENT invalid cwd — the load path revalidates its own
// params; a known sessionId must not relax cwd validation.
func TestSessionLoadCwdValidatedPerCall(t *testing.T) {
	var loaded atomic.Value
	agent := &fakeLoadingAgent{loadFn: func(_ context.Context, id, _ string, _ *acp.Client) (acp.Session, error) {
		loaded.Store(id)
		return &fakeSession{id: id}, nil
	}}
	d := startDialog(t, agent, nil)
	id := d.newSession("/tmp/proj")

	d.request(3, "session/load", map[string]any{"sessionId": id, "cwd": "elsewhere/relative", "mcpServers": []any{}})
	e := respError(t, d.untilResponseID(3))
	if code, ok := e["code"].(float64); !ok || int(code) != acp.ErrInvalidParams {
		t.Errorf("SECURITY: [acp-setup] session/load for a known session accepted a relative cwd (error %v); known sessionId must not relax cwd validation.", e)
	}
}

// Guard against overreach: the absoluteness gate must accept legitimate
// absolute cwds (trailing slash, deeply nested) unchanged, or embedders
// would route around the server's validation entirely.
func TestSessionSetupAcceptsLegitAbsoluteCwds(t *testing.T) {
	for _, cwd := range []string{"/tmp/proj", "/tmp/proj/", "/a/deeply/nested/path"} {
		t.Run(cwd, func(t *testing.T) {
			var got atomic.Value
			agent := &fakeAgent{newFn: func(_ context.Context, cwd string) (acp.Session, error) {
				got.Store(cwd)
				return &fakeSession{id: "s1"}, nil
			}}
			d := startDialog(t, agent, nil)
			d.initialize()
			d.request(2, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
			resp := d.untilResponseID(2)
			if resp["error"] != nil {
				t.Errorf("session/new rejected legitimate absolute cwd %q: %v", cwd, resp["error"])
				return
			}
			if seen, _ := got.Load().(string); seen != cwd {
				t.Errorf("agent received cwd %q, want the validated %q passed through verbatim", seen, cwd)
			}
		})
	}
}
