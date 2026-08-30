package framework

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
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

// TestDevMCPRefusesExposedStartLive pins the same property as the
// classifier and guard tests above, but end-to-end over a real listener:
// an exposed (non-loopback) dev Start must not serve the dev-implied
// control tools to a client that forges the loopback Host header. The
// transport's Host pin is a browser control, not a network control
// (core/mcp/transport.go documents exactly this gap), so the BIND is the
// only real boundary — and it has to hold on the live path, not just at
// bindIsLoopback and the a.mcpControl flag.
//
// The second case drives the documented pre-Start InitPlugins shape
// (its doc comment blesses the manual call, and TestHarness uses it):
// registerControlTools runs there gated only by a.mcpControl, and
// mcp.Server has no unregister API, so guardDevMCPBind's later flag
// flip cannot withdraw tools that already registered.
func TestDevMCPRefusesExposedStartLive(t *testing.T) {
	run := func(t *testing.T, earlyInitPlugins bool) {
		t.Helper()
		t.Setenv("GOFASTR_DOTENV", "off")
		t.Setenv("GOFASTR_DEV", "1")
		t.Setenv("GOFASTR_ENV", "")
		t.Setenv("GOFASTR_DEV_MCP", "")
		t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "")

		app := NewApp()
		ready := make(chan string, 1)
		app.OnReady(func(addr string) { ready <- addr })
		if earlyInitPlugins {
			if err := app.InitPlugins(); err != nil {
				t.Fatalf("pre-Start InitPlugins: %v", err)
			}
		}
		started := make(chan error, 1)
		go func() { started <- app.Start("0.0.0.0:0") }()

		var base string
		select {
		case addr := <-ready:
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				t.Fatalf("ready addr %q: %v", addr, err)
			}
			base = "http://127.0.0.1:" + port
		case err := <-started:
			t.Fatalf("Start returned before ready: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("Start never became ready")
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := app.Shutdown(ctx); err != nil {
				t.Errorf("shutdown: %v", err)
			}
			select {
			case err := <-started:
				if err != nil {
					t.Errorf("Start: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("Start did not return after Shutdown")
			}
		}()

		call := func(id int, method, params string) mcp.Response {
			t.Helper()
			body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, method, params)
			req, err := http.NewRequest(http.MethodPost, base+"/mcp", strings.NewReader(body))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			// The forged pin-pass: SetRequireLoopbackHost checks the
			// Host header, and a direct TCP client sets it freely.
			req.Host = "localhost"
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST /mcp: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("POST /mcp: status %d", resp.StatusCode)
			}
			var out mcp.Response
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			return out
		}

		// Disclosure surface: an anonymous tools/list must not name the
		// mutating control tools.
		list := call(1, "tools/list", `{}`)
		var listed struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		blob, err := json.Marshal(list.Result)
		if err != nil {
			t.Fatalf("marshal tools/list result: %v", err)
		}
		if err := json.Unmarshal(blob, &listed); err != nil {
			t.Fatalf("unmarshal tools/list: %v", err)
		}
		for _, tl := range listed.Tools {
			if strings.HasPrefix(tl.Name, "app_module_") {
				t.Errorf("SECURITY: [exposure] anonymous tools/list on an exposed dev bind disclosed %q", tl.Name)
			}
		}

		// Reach surface: tools/call must not reach the module store. The
		// module store's unknown-module error ("not registered",
		// TestMCPControlUnknownModuleErrors) is the signature that the
		// handler ran; both legitimate refusals read differently
		// (tool-not-found when the guard withheld registration,
		// auth-required if a gate refuses the call).
		res := call(2, "tools/call", `{"name":"app_module_disable","arguments":{"name":"no-such-module"}}`)
		msg := ""
		if res.Error != nil {
			msg = res.Error.Message
		}
		if strings.Contains(msg, "not registered") {
			t.Errorf("SECURITY: [exposure] app_module_disable reached its handler on an exposed dev bind via a forged loopback Host: %s", msg)
		}
	}

	t.Run("standard start order", func(t *testing.T) { run(t, false) })
	t.Run("InitPlugins before Start", func(t *testing.T) { run(t, true) })
}

// TestMCPRequireUserRefusesEmbedGrant pins requireMCPUser's second
// refusal. An embed grant resolves to the same context user a session
// does, so an identity check alone would let a credential that lives in
// a third party's page — readable by anyone with devtools — drive every
// gated tool, the control tools included. The refusal is implemented
// (mcp_control.go) but had no pin.
func TestMCPRequireUserRefusesEmbedGrant(t *testing.T) {
	plain := handler.SetUser(context.Background(), struct{ ID string }{ID: "page-user"})
	embedded := fembed.WithGrant(plain, fembed.Grant{})

	err := MCPRequireUser()(embedded)
	if !errors.Is(err, errEmbedNotAllowed) {
		t.Fatalf("SECURITY: [authz] resolved user on an embed grant must get the embed refusal, got %v", err)
	}

	// Specificity: the same user WITHOUT a grant passes, so the refusal
	// is about the credential class, not a blanket deny that would read
	// as protection while locking out the dev loop.
	if err := MCPRequireUser()(plain); err != nil {
		t.Fatalf("first-party authenticated caller refused: %v", err)
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
