package framework

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestDevMCPRefusesNonLoopbackBind pins that `gofastr dev` cannot expose
// its MUTATING MCP surface to the network.
//
// Attack: dev mode implies mount + introspection + the control tools,
// with no auth in front of them, and pins the transport to a loopback
// *Host header*. That pin stops DNS rebinding, a browser cannot forge
// Host, but it is a browser control, not a network control: any TCP
// client sets Host freely. Proof from the audit: an honest LAN Host got
// 403, `Host: localhost` got a full tools/list plus app_module_disable
// reaching its handler. `gofastr dev --addr 0.0.0.0:8080` on a shared
// network is therefore a remote, unauthenticated control plane.
//
// The Host pin has to be paired with a check on the BIND. The property:
// dev-implied control tools are registered only when the listener is
// loopback.
func TestDevMCPRefusesNonLoopbackBind(t *testing.T) {
	loopback := []string{
		"localhost:8080", "127.0.0.1:8080", "[::1]:8080",
		"127.0.0.1:0",
		// No address chosen at all, the server-side default is
		// localhost:8080.
		"",
	}
	// ":8080" and a bare "8080" bind EVERY interface in Go, so they
	// belong here, not with the loopback forms they superficially
	// resemble. That confusion is the whole reason this classifier is
	// worth a test.
	exposed := []string{
		"0.0.0.0:8080", "192.168.1.20:8080", "[::]:8080",
		"10.0.0.5:8080", "example.internal:8080",
		":8080", "8080",
	}
	for _, addr := range loopback {
		if !bindIsLoopback(addr) {
			t.Errorf("bind %q is loopback but was classified as exposed — dev MCP would be disabled for an ordinary local run", addr)
		}
	}
	for _, addr := range exposed {
		if bindIsLoopback(addr) {
			t.Errorf("SECURITY: [exposure] bind %q was classified as loopback — dev mode would register the mutating MCP control tools on a network-reachable listener", addr)
		}
	}
}

// The escape hatch must exist, be explicit, and be visible.
func TestDevMCPExposeOptInIsExplicit(t *testing.T) {
	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "")
	if devMCPExposeAllowed() {
		t.Error("SECURITY: [exposure] exposing the dev MCP is allowed by default")
	}
	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "1")
	if !devMCPExposeAllowed() {
		t.Error("GOFASTR_DEV_MCP_EXPOSE=1 did not enable the documented escape hatch")
	}
	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "0")
	if devMCPExposeAllowed() {
		t.Error("GOFASTR_DEV_MCP_EXPOSE=0 must not enable the escape hatch")
	}
}

// The refusal has to be loud: a silently-disabled tool set looks like a
// framework bug to whoever is debugging why their agent can't connect.
func TestDevMCPRefusalMessageNamesTheFix(t *testing.T) {
	msg := devMCPExposureWarning("0.0.0.0:8080")
	for _, want := range []string{"0.0.0.0:8080", "GOFASTR_DEV_MCP_EXPOSE", "loopback"} {
		if !strings.Contains(msg, want) {
			t.Errorf("dev MCP refusal message does not mention %q: %s", want, msg)
		}
	}
}

// TestControlToolsGatedInProduction pins that the MUTATING MCP control
// tools require an authenticated caller when they were opted into
// explicitly (WithMCPControl), i.e. on a production /mcp.
//
// mcp.Gated and auth.MCPUser/MCPRole exist but had zero production call
// sites: every shipped tool was ungated. For the read-only introspection
// tools that is a documented posture; for app_module_enable /
// app_module_disable, which change what the running app serves, it
// means a reachable /mcp is a control plane for whoever finds it.
//
// The dev loop is exempt: `gofastr dev` implies these tools with no auth
// configured at all, and its exposure is bounded by the loopback bind
// check instead (TestDevMCPRefusesNonLoopbackBind).
func TestControlToolsGatedInProduction(t *testing.T) {
	t.Run("explicit opt-in is gated", func(t *testing.T) {
		gate := controlToolGate(false /* devImplied */)
		if gate == nil {
			t.Fatal("SECURITY: [authz] WithMCPControl registers the mutating tools with no gate at all")
		}
		if err := gate(context.Background()); err == nil {
			t.Error("SECURITY: [authz] the control-tool gate admitted a caller with no identity on the context")
		}
		ctx := handler.SetUser(context.Background(), struct{ ID string }{ID: "u1"})
		if err := gate(ctx); err != nil {
			t.Errorf("the control-tool gate refused an authenticated caller: %v", err)
		}
	})

	t.Run("dev-implied is ungated", func(t *testing.T) {
		if gate := controlToolGate(true /* devImplied */); gate != nil {
			if err := gate(context.Background()); err != nil {
				t.Errorf("the dev loop must reach its own control tools without auth: %v", err)
			}
		}
	})
}

// guardDevMCPBind is the piece that actually withdraws the tools, and
// the classifier tests above only exercise its inputs. This drives the
// method: an exposed bind drops the dev-implied opt-in, a loopback bind
// keeps it, the documented env escape hatch restores it, and an
// EXPLICIT WithMCPControl is never touched, that is a deliberate
// production choice with its own gate, not the dev loop's convenience.
func TestGuardDevMCPBindWithdrawsOnlyDevImplied(t *testing.T) {
	devApp := func() *App {
		a := NewApp()
		a.mcpControl, a.mcpControlDevImplied = true, true
		return a
	}

	a := devApp()
	a.guardDevMCPBind("0.0.0.0:8080")
	if a.mcpControl {
		t.Error("SECURITY: [exposure] the dev-implied control tools survived a non-loopback bind")
	}

	a = devApp()
	a.guardDevMCPBind("localhost:8080")
	if !a.mcpControl {
		t.Error("a loopback bind must keep the dev loop's own control tools")
	}

	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "1")
	a = devApp()
	a.guardDevMCPBind("0.0.0.0:8080")
	if !a.mcpControl {
		t.Error("the documented escape hatch did not restore the control tools")
	}
	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "")

	// Explicit opt-in: not dev-implied, so the bind check does not apply.
	// Its own gate (TestControlToolsGatedInProduction) is what protects it.
	explicit := NewApp(WithMCPControl())
	explicit.guardDevMCPBind("0.0.0.0:8080")
	if !explicit.mcpControl {
		t.Error("an explicit WithMCPControl was withdrawn by the dev bind check")
	}
}

// warnUnresolvableRelations is a boot diagnostic, so its value is that
// it fires on the shape it exists for and stays quiet otherwise, a
// warning on every healthy boot is a warning nobody reads.
func TestWarnUnresolvableRelationsIsSelective(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	app := NewApp(WithLogger(logger))
	app.Entity("ur_posts", entity.EntityConfig{
		Table:  "ur_posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{
			{Name: "author", Type: entity.RelManyToOne, Entity: "ur_users", ForeignKey: "author_id"},
		},
	}.WithTimestamps(false))
	app.warnUnresolvableRelations()
	if !strings.Contains(buf.String(), "ur_users") {
		t.Errorf("boot did not name the unresolvable relation target: %s", buf.String())
	}

	buf.Reset()
	ok := NewApp(WithLogger(logger))
	ok.Entity("nt_users", entity.EntityConfig{
		Table:  "nt_users",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	ok.Entity("nt_posts", entity.EntityConfig{
		Table:  "nt_posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{
			{Name: "author", Type: entity.RelManyToOne, Entity: "nt_users", ForeignKey: "author_id"},
		},
	}.WithTimestamps(false))
	ok.warnUnresolvableRelations()
	if strings.Contains(buf.String(), "relation target") {
		t.Errorf("a fully-registered graph must boot silently: %s", buf.String())
	}
}
