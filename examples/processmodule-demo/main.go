// Package main is the processmodule-demo: a minimal but real third-party
// module that speaks the moduleproto protocol over stdio.
//
// It is the canonical example of a process-isolated module (see
// framework/docs/content/process-modules.md → "Building a module") AND the
// child the §10 go/no-go gate suite drives end to end
// (framework/processmodule_gate_test.go). It depends only on the gofastr
// module's [moduleproto] package + the standard library.
//
// On start it opens a [moduleproto.Codec] over stdin/stdout, constructs a
// [moduleproto.Peer] in the child role, registers handlers for every host →
// module method it serves, calls [Peer.Start], and blocks on [Peer.Done]
// (clean EOF on stdin). Its surface:
//
//   - module.handshake → echoes the host's expected identity (instance_id +
//     desired_generation) and surface digest; a mismatch would be terminal.
//   - module.ready     → ready:true (after an optional DEMO_READY_DELAY_MS so
//     the enabled-but-not-Ready → 503 window is exercisable).
//   - module.health    → ok:true, zero in-flight.
//   - module.http      → three routes:
//   - "hello" (GET /hello) → a ui.node.v1 body: a small valid tree.
//   - "tree"  (GET /tree)  → the SAME tree as a json body, so the host can
//     run the closed ui.node.v1 validator over the exact bytes the child
//     would have rendered (the proxy's render path is deferred; this route
//     makes the validator reachable from a test).
//   - "items" (GET /items) → makes a REVERSE host.entity.query for the
//     entity named by DEMO_QUERY_ENTITY and returns the result as json,
//     proving the reverse broker path end to end.
//   - module.drain     → zero in-flight.
//   - module.tool.list / module.tool.call → one tool ("ping") whose call body
//     also does a reverse host.entity.query (proves the tool → broker path).
//
// Misbehave knobs (env), each driving one adversarial gate test:
//
//   - DEMO_CRASH_ON=<routeID>  → os.Exit(1) mid module.http handler for that
//     route, so crash containment + the buffered-503 guarantee are provable.
//   - DEMO_FORGE_DATAFUI=1     → /hello and /tree return a tree that tries to
//     smuggle a data-fui-rpc prop; the closed validator must whole-tree
//     reject it (the host never renders a forged runtime attribute).
//   - DEMO_EXTRA_TOOL=1        → module.tool.list returns a tool the
//     descriptor did not approve, so the handshake byte-equality quarantine
//     fires (terminal Failed, no restart loop).
//   - DEMO_READY_DELAY_MS=<n>  → module.ready blocks ~n ms before reporting
//     ready, widening the enabled-but-not-Ready 503 window on demand.
//   - DEMO_QUERY_ENTITY=<name> → entity the /items route + the ping tool
//     reverse-query (default "articles").
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

func main() {
	os.Exit(run())
}

// run wires the codec + peer, serves until EOF, and returns the exit code.
func run() int {
	codec, err := moduleproto.NewCodec(os.Stdin, os.Stdout, moduleproto.DefaultMaxFrameBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo: codec:", err)
		return 1
	}
	peer := moduleproto.NewPeer(codec, moduleproto.RoleChild)

	if err := registerHandlers(peer); err != nil {
		fmt.Fprintln(os.Stderr, "demo: register:", err)
		return 1
	}

	peer.Start()
	<-peer.Done()
	return 0
}

// cfg reads the misbehave knobs once at startup.
var cfg = struct {
	crashOn      string
	forgeDataFUI bool
	extraTool    bool
	readyDelayMs int
	queryEntity  string
}{
	crashOn:      os.Getenv("DEMO_CRASH_ON"),
	forgeDataFUI: os.Getenv("DEMO_FORGE_DATAFUI") != "",
	extraTool:    os.Getenv("DEMO_EXTRA_TOOL") != "",
	readyDelayMs: envInt("DEMO_READY_DELAY_MS", 0),
	queryEntity:  envString("DEMO_QUERY_ENTITY", "articles"),
}

func envInt(key string, def int) int {
	if s := os.Getenv(key); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}

func envString(key, def string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return def
}

// registerHandlers installs every host → module method the demo serves,
// closing over the peer so the /items route and the ping tool can issue
// reverse host.entity.query calls. The peer's built-in module.cancel handler
// is NOT overwritten here.
func registerHandlers(peer *moduleproto.Peer) error {
	// module.handshake — this IS the initialize (design §4.7 step 3). Echo the
	// host's expected identity + surface digest exactly; a divergence is a
	// terminal integrity fault on the host side.
	if err := peer.Handle(moduleproto.MethodHandshake, func(_ context.Context, params json.RawMessage) (any, error) {
		var hp moduleproto.HandshakeParams
		_ = json.Unmarshal(params, &hp)
		return moduleproto.HandshakeResult{
			Proto: moduleproto.ProtoRange{Min: 1, Max: 1},
			Identity: moduleproto.Identity{
				Name:              hp.Expected.Name,
				Version:           hp.Expected.Version,
				InstanceID:        hp.Expected.InstanceID,
				DesiredGeneration: hp.Expected.DesiredGeneration,
			},
			SurfaceSHA256: hp.Expected.SurfaceSHA256,
			Ready:         false,
		}, nil
	}); err != nil {
		return err
	}

	// module.ready — the warmup gate. The optional delay widens the
	// enabled-but-not-Ready → 503 window so a test can observe it reliably.
	if err := peer.Handle(moduleproto.MethodReady, func(context.Context, json.RawMessage) (any, error) {
		if cfg.readyDelayMs > 0 {
			time.Sleep(time.Duration(cfg.readyDelayMs) * time.Millisecond)
		}
		return moduleproto.ReadyResult{Ready: true}, nil
	}); err != nil {
		return err
	}

	// module.health — ongoing liveness ping.
	if err := peer.Handle(moduleproto.MethodHealth, func(context.Context, json.RawMessage) (any, error) {
		return moduleproto.HealthResult{OK: true, Inflight: 0}, nil
	}); err != nil {
		return err
	}

	// module.http — the proxied HTTP surface. DEMO_CRASH_ON matches a RouteID
	// and os.Exit(1)s mid-call so the host observes an in-flight crash (the
	// buffered-503 guarantee, design §8).
	if err := peer.Handle(moduleproto.MethodHTTP, func(ctx context.Context, params json.RawMessage) (any, error) {
		var p moduleproto.HTTPRequestParams
		_ = json.Unmarshal(params, &p)

		if cfg.crashOn != "" && (cfg.crashOn == p.RouteID || cfg.crashOn == p.RequestID) {
			// Sleep briefly so the host's Call is genuinely in-flight when
			// we die; an instantaneous exit could race the response write.
			time.Sleep(20 * time.Millisecond)
			os.Exit(1)
		}

		switch p.RouteID {
		case "hello":
			return moduleproto.HTTPResponseResult{
				Status:  200,
				Headers: map[string]string{"X-Module": "demo"},
				Body: moduleproto.HTTPResponseBody{
					Kind:  moduleproto.BodyKindUINodeV1,
					Value: screenTree(),
				},
			}, nil
		case "tree":
			// Same tree, json body kind — the proxy passes json through, so a
			// test can run the closed ui.node.v1 validator over the exact
			// bytes the child would have rendered.
			return moduleproto.HTTPResponseResult{
				Status:  200,
				Headers: map[string]string{"X-Module": "demo"},
				Body: moduleproto.HTTPResponseBody{
					Kind:  moduleproto.BodyKindJSON,
					Value: screenTree(),
				},
			}, nil
		case "items":
			rows, total, err := reverseQuery(ctx, peer, p.Caller)
			if err != nil {
				// A denial from the broker surfaces as a per-call error;
				// report it as a 403 so a test can distinguish "denied"
				// from "unavailable".
				body, _ := json.Marshal(map[string]string{"error": err.Error()})
				return moduleproto.HTTPResponseResult{
					Status: 403,
					Body:   moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindJSON, Value: body},
				}, nil
			}
			body, _ := json.Marshal(map[string]any{"entity": cfg.queryEntity, "total": total, "rows": rows})
			return moduleproto.HTTPResponseResult{
				Status: 200,
				Body:   moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindJSON, Value: body},
			}, nil
		default:
			body, _ := json.Marshal(map[string]string{"error": "unknown route " + p.RouteID})
			return moduleproto.HTTPResponseResult{
				Status: 404,
				Body:   moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindJSON, Value: body},
			}, nil
		}
	}); err != nil {
		return err
	}

	// module.drain — finish in-flight, report zero.
	if err := peer.Handle(moduleproto.MethodDrain, func(context.Context, json.RawMessage) (any, error) {
		return moduleproto.DrainResult{Inflight: 0}, nil
	}); err != nil {
		return err
	}

	// module.tool.list — the optional MCP tool surface. Byte-equality with
	// the descriptor digests is enforced by the host at handshake; the
	// DEMO_EXTRA_TOOL knob adds an unapproved tool so that quarantine fires.
	if err := peer.Handle(moduleproto.MethodToolList, func(context.Context, json.RawMessage) (any, error) {
		tools := []moduleproto.Tool{pingTool()}
		if cfg.extraTool {
			tools = append(tools, moduleproto.Tool{
				ID:          "rogue",
				Name:        "module.demo.rogue",
				Description: "not in the descriptor — must be rejected",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			})
		}
		return moduleproto.ToolListResult{Tools: tools}, nil
	}); err != nil {
		return err
	}

	// module.tool.call — invoke one tool. The ping tool makes a reverse
	// host.entity.query (proving the tool → broker path) and returns the row
	// count as its result.
	if err := peer.Handle(moduleproto.MethodToolCall, func(ctx context.Context, params json.RawMessage) (any, error) {
		var p moduleproto.ToolCallParams
		_ = json.Unmarshal(params, &p)
		if p.ToolID != "ping" {
			return nil, fmt.Errorf("unknown tool %q", p.ToolID)
		}
		rows, total, err := reverseQuery(ctx, peer, p.Caller)
		if err != nil {
			return nil, err
		}
		result, _ := json.Marshal(map[string]any{"entity": cfg.queryEntity, "total": total, "rows": rows, "caller": p.Caller.Subject})
		return moduleproto.ToolCallResult{Result: result}, nil
	}); err != nil {
		return err
	}

	return nil
}

// reverseQuery issues a host.entity.query for the configured entity, echoing
// the caller block so the host re-attaches the originating end-user's context
// to the CRUD re-dispatch. A broker denial returns as a per-call error.
func reverseQuery(ctx context.Context, peer *moduleproto.Peer, caller moduleproto.Caller) (json.RawMessage, int, error) {
	qp := moduleproto.EntityQueryParams{
		Entity: cfg.queryEntity,
		Limit:  100,
		Caller: caller,
	}
	raw, err := peer.Call(ctx, moduleproto.MethodHostEntityQuery, qp)
	if err != nil {
		return nil, 0, err
	}
	var res moduleproto.EntityQueryResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, 0, err
	}
	return res.Rows, res.Total, nil
}

// screenTree returns the ui.node.v1 tree the /hello and /tree routes serve.
// The clean tree is a small valid tree (card → heading + paragraph + button
// with an action_ref). Under DEMO_FORGE_DATAFUI it returns a tree that tries
// to smuggle a data-fui-rpc prop through CardProps; the closed validator
// rejects the whole tree because the prop is unrepresentable.
func screenTree() json.RawMessage {
	if cfg.forgeDataFUI {
		// The data-fui-rpc key is NOT a CardProps field → strictDecode
		// rejects it → whole-tree fail-closed (design §9).
		return json.RawMessage(`{` +
			`"component":"card",` +
			`"props":{"title":"x","data-fui-rpc":"/auth/logout"},` +
			`"children":[{"component":"paragraph","props":{"text":"smuggled"}}]` +
			`}`)
	}
	return json.RawMessage(`{` +
		`"component":"card",` +
		`"props":{"title":"Demo module"},` +
		`"children":[` +
		`{"component":"heading","props":{"level":1,"text":"Hello from the demo module"}},` +
		`{"component":"paragraph","props":{"text":"This screen came from a validated ui.node.v1 tree returned by an out-of-process child over moduleproto."}},` +
		`{"component":"button","props":{"label":"Refresh","variant":"primary"},"action_ref":"refresh"}` +
		`]}`)
}

// pingTool is the one MCP tool the demo exposes. Its canonical digest is what
// the host byte-compares against the descriptor at handshake
// (framework.ModuleToolDigest).
func pingTool() moduleproto.Tool {
	return moduleproto.Tool{
		ID:          "ping",
		Name:        "module.demo.ping",
		Description: "Reverse-queries the granted host entity and reports the row count.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
	}
}
