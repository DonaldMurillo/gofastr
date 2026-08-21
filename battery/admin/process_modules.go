package admin

// Process-module operator lifecycle screen (issue #37, design §5/§8). Lists
// every supervised process module with its introspection fields and exposes
// the four operator levers: enable, disable, bump-generation (the circuit
// reset / recovery lever), and per-grant revoke. Every mutation is a CSRF'd
// POST that writes an audit row (via framework.AppendAuditEvent, exactly as
// the RBAC grant/revoke handlers do) and 303-redirects back to the list.
//
// The screen is the same standalone SSR pipeline as the RBAC screens,
// b.writePage + section(...) + core-ui/html typed configs, and gates behind
// b.gate (admin-only), so it inherits the admin battery's default-deny. No
// data brokering and no secrets are shown: operator control only.
//
// The supervisor itself is never spawned here. The screen consumes a narrow
// processModuleController interface satisfied by *framework.ProcessModuleSupervisor;
// tests inject a fake so the screen is exercised without a real child.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	html "github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// processModuleController is the seam the screen consumes, over exactly the
// supervisor methods the handlers call. The real *framework.ProcessModuleSupervisor
// satisfies it; tests pass a fake so no child process is spawned. nil on
// Config.ProcessModules means the screen is not mounted (route 404s).
type processModuleController interface {
	List() []framework.ProcessModuleInfo
	Enable(ctx context.Context, name string) error
	Disable(ctx context.Context, name string) error
	RevokeGrants(ctx context.Context, name string, grants []access.Permission) (uint64, error)
	BumpGeneration(ctx context.Context, name string) (uint64, error)
}

// Compile-time proof that the real supervisor satisfies the seam. If a
// framework edit changes a signature this package consumes, the build fails
// here rather than at a host wiring site.
var _ processModuleController = (*framework.ProcessModuleSupervisor)(nil)

// modulesBase is the action→audit-op mapping (entity "module"). Centralized
// so the POST handlers and any future introspection stay in lockstep.
const (
	modulesListPath = "/modules"
	opModuleEnable  = "module_enable"
	opModuleDisable = "module_disable"
	opModuleBump    = "module_bump"
	opModuleRevoke  = "module_revoke"
	modulesAuditEnt = "module"
)

// handleProcessModules renders the operator lifecycle screen. The list comes
// from the controller; an empty list renders a friendly empty state (never a
// panic, the generated-app rule). A controller error on a prior POST is
// surfaced via the ?err= query param as a danger Callout flash above the
// table, never a raw 500 or JSON leak.
func (b *Battery) handleProcessModules(w http.ResponseWriter, r *http.Request) {
	modules := b.cfg.ProcessModules.List()

	csrf := middleware.TokenFromContext(r.Context())

	var parts []render.HTML
	if errMsg := strings.TrimSpace(r.URL.Query().Get("err")); errMsg != "" {
		// Operator-facing flash. The message is operator-safe (controller
		// errors name the module/state, never secrets); the Callout renders
		// it HTML-escaped.
		parts = append(parts, adminError(errMsg))
	}

	if len(modules) == 0 {
		parts = append(parts, ui.Muted(render.Text("No process modules registered.")))
		b.writePage(w, b.cfg.Title, "Modules",
			adminSection("Process Modules", ui.Stack(ui.StackConfig{Gap: ui.GapMD}, parts...)))
		return
	}

	cols := []ui.Column{
		{Key: "module", Header: "Module"},
		{Key: "trust", Header: "Trust"},
		{Key: "state", Header: "State"},
		{Key: "gen", Header: "Generation"},
		{Key: "restarts", Header: "Restarts"},
		{Key: "routes", Header: "Routes / Tools"},
		{Key: "exit", Header: "Last exit"},
		{Key: "actions", Header: "Actions"},
	}
	rows := make([]ui.Row, len(modules))
	for i, m := range modules {
		rows[i] = ui.Row{Cells: map[string]render.HTML{
			"module":   moduleNameCell(m),
			"trust":    render.Text(m.TrustTier.String()),
			"state":    moduleStateCell(m),
			"gen":      moduleGenerationCell(m),
			"restarts": render.Text(fmt.Sprintf("%d", m.RestartCount)),
			"routes":   render.Text(fmt.Sprintf("%d / %d", m.RouteCount, m.ToolCount)),
			"exit":     moduleLastExitCell(m.LastExit),
			"actions":  moduleActionsCell(b.cfg.PathPrefix, csrf, m),
		}}
	}
	parts = append(parts, ui.DataTable(ui.DataTableConfig{
		Columns: cols,
		Rows:    rows,
		Empty:   ui.EmptyStateConfig{Title: "No modules", HeadingLevel: 3},
	}))
	b.writePage(w, b.cfg.Title, "Modules",
		adminSection("Process Modules", ui.Stack(ui.StackConfig{Gap: ui.GapMD}, parts...)))
}

// moduleNameCell renders the name (monospace, matching the RBAC role cell)
// plus a quiet version line when the descriptor carries one.
func moduleNameCell(m framework.ProcessModuleInfo) render.HTML {
	if m.Version == "" {
		return monoCell(m.Name)
	}
	return render.HTML(string(monoCell(m.Name)) + " " + string(ui.Muted(render.Text(m.Version))))
}

// moduleStateCell renders the state name plus the 404-vs-503 meaning
// (design §8 decision D, the operator must read a disabled module
// differently from a crashed one). Ready renders plain; disabled renders
// muted; enabled-but-not-serving renders a warning StatusBadge. Circuit-open
// and lease-failing render danger StatusBadges since both mean serving
// failures right now.
func moduleStateCell(m framework.ProcessModuleInfo) render.HTML {
	var items []render.HTML
	switch m.State {
	case framework.StateReady:
		items = append(items, render.Text(m.State.String()))
	case framework.StateInstalledDisabled, framework.StateDrainingDisable, framework.StateAbsent:
		items = append(items, ui.Muted(render.Text(m.State.String())))
	default:
		// Starting/Handshaking/Crashed/Backoff/DrainingUpgrade/Failed,
		// enabled but not serving; reads as trouble.
		items = append(items, ui.StatusBadge(ui.StatusBadgeConfig{
			Label: m.State.String(), Variant: ui.StatusWarning,
		}))
	}

	// The 404-vs-503 semantics, in copy. This is the operator-readable
	// signal that distinguishes "uninstalled-looking" from "retryable".
	_, code := moduleHTTPSemantics(m.State)
	items = append(items, ui.Muted(render.Text(code)))

	if m.CircuitOpen {
		items = append(items, ui.StatusBadge(ui.StatusBadgeConfig{Label: "Circuit open", Variant: ui.StatusDanger}))
	}
	if m.LeaseFailing {
		// Lease-failing means fail-closed / serving 503 right now, the
		// loudest signal on the page short of a crash.
		items = append(items, ui.StatusBadge(ui.StatusBadgeConfig{Label: "Lease failing", Variant: ui.StatusDanger}))
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS}, items...)
}

// moduleHTTPSemantics maps a ProcessState to the HTTP meaning an operator
// reasons about (design §8 decision D): disabled → 404 (indistinguishable
// from uninstalled); enabled-but-not-Ready → 503 + Retry-After; Ready →
// serving. Returns a short label and the code/phrase shown in copy.
func moduleHTTPSemantics(state framework.ProcessState) (label, code string) {
	switch state {
	case framework.StateInstalledDisabled, framework.StateDrainingDisable, framework.StateAbsent:
		return "disabled", "serves 404"
	case framework.StateReady:
		return "ready", "serving"
	case framework.StateFailed:
		return "failed", "serves 503"
	default:
		return "down", "serves 503"
	}
}

// moduleGenerationCell shows desired vs observed generation. A lagging
// observed generation means convergence is in flight (design §8).
func moduleGenerationCell(m framework.ProcessModuleInfo) render.HTML {
	text := fmt.Sprintf("%d / %d", m.DesiredGeneration, m.ObservedGeneration)
	if m.ObservedGeneration < m.DesiredGeneration {
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: text, Variant: ui.StatusDanger})
	}
	return render.Text(text)
}

// moduleLastExitCell renders the last exit reason quietly, or an em-dash when
// the module has never exited.
func moduleLastExitCell(last string) render.HTML {
	if strings.TrimSpace(last) == "" {
		return ui.Muted(render.Text("—"))
	}
	return ui.Muted(render.Text(last))
}

// moduleActionsCell renders the per-row lifecycle levers as CSRF'd inline
// POST forms (same shape as the RBAC grant/revoke forms). Disable and Revoke
// carry data-fui-confirm, the existing destructive-action affordance, no
// new JS. Enable/Disable choose based on state so the operator is offered
// the action that actually changes something.
func moduleActionsCell(prefix, csrf string, m framework.ProcessModuleInfo) render.HTML {
	var forms []render.HTML
	if moduleIsDisabled(m.State) {
		forms = append(forms, moduleActionForm(prefix, csrf, m.Name, "enable", "Enable", "", ui.ButtonSecondary))
	} else {
		forms = append(forms, moduleActionForm(prefix, csrf, m.Name, "disable", "Disable",
			"Disable module "+m.Name+"? It will drain and stop serving.", ui.ButtonDanger))
	}
	// Bump generation = the recovery / circuit-reset lever (design §8).
	forms = append(forms, moduleActionForm(prefix, csrf, m.Name, "bump", "Bump generation", "", ui.ButtonGhost))
	// Revoke a single capability (free-text resource:verb; bumps generation).
	forms = append(forms, moduleRevokeForm(prefix, csrf, m.Name))
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM}, forms...)
}

// moduleIsDisabled reports whether the current state means "not serving,
// route gate 404s", i.e. Enable is the meaningful action.
func moduleIsDisabled(state framework.ProcessState) bool {
	switch state {
	case framework.StateInstalledDisabled, framework.StateDrainingDisable, framework.StateAbsent, framework.StateFailed:
		return true
	}
	return false
}

// moduleActionForm renders a single-submit inline form for a named action.
// confirm, when non-empty, sets data-fui-confirm (the existing runtime
// affordance, no new JS). variant picks the ui.Button treatment.
func moduleActionForm(prefix, csrf, name, action, label, confirm string, variant ui.ButtonVariant) render.HTML {
	attrs := html.Attrs{}
	if confirm != "" {
		attrs["data-fui-confirm"] = confirm
	}
	return render.HTML(html.Form(html.FormConfig{
		Method: "post",
		Action: prefix + modulesListPath + "/_" + action,
		Class:  "admin-inline",
	},
		html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrf}),
		html.Input(html.InputConfig{Type: "hidden", Name: "module", Value: name}),
		ui.Button(ui.ButtonConfig{
			Label: label, Type: "submit", Variant: variant,
			Size:       ui.ButtonSizeSmall,
			ExtraAttrs: attrs,
		}),
	))
}

// moduleRevokeForm renders the revoke-grant inline form: a free-text
// resource:verb input plus the revoke submit. Destructive → data-fui-confirm.
func moduleRevokeForm(prefix, csrf, name string) render.HTML {
	return render.HTML(html.Form(html.FormConfig{
		Method: "post",
		Action: prefix + modulesListPath + "/_revoke",
		Class:  "admin-inline",
	},
		html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrf}),
		html.Input(html.InputConfig{Type: "hidden", Name: "module", Value: name}),
		html.Input(html.InputConfig{
			Type:        "text",
			Name:        "grant",
			Placeholder: "resource:verb",
			Class:       "admin-input",
			ExtraAttrs:  html.Attrs{"required": "required", "aria-label": "Capability to revoke from " + name},
		}),
		ui.Button(ui.ButtonConfig{
			Label:      "Revoke",
			Type:       "submit",
			Variant:    ui.ButtonDanger,
			Size:       ui.ButtonSizeSmall,
			ExtraAttrs: html.Attrs{"data-fui-confirm": "Revoke this capability from " + name + "? Its generation bumps and the child restarts."},
		}),
	))
}

// ----- POST handlers --------------------------------------------------------
//
// Each handler validates the module name, calls the controller, writes an
// audit row, and 303-redirects to the list. On any error (validation,
// controller, unknown module) it 303-redirects with ?err=<message> so the
// failure surfaces as a flash on the list page, never a raw 500 or JSON
// leak (generated-app rule).

func (b *Battery) handleModuleEnable(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("module"))
	if name == "" {
		moduleBounce(w, r, b.cfg.PathPrefix, "module name required")
		return
	}
	if err := b.cfg.ProcessModules.Enable(r.Context(), name); err != nil {
		moduleBounce(w, r, b.cfg.PathPrefix, moduleErrText("enable", name, err))
		return
	}
	actor := adminActorID(r.Context())
	b.appendAudit(r.Context(), modulesAuditEnt, opModuleEnable, name, actor, nil)
	http.Redirect(w, r, b.cfg.PathPrefix+modulesListPath, http.StatusSeeOther)
}

func (b *Battery) handleModuleDisable(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("module"))
	if name == "" {
		moduleBounce(w, r, b.cfg.PathPrefix, "module name required")
		return
	}
	if err := b.cfg.ProcessModules.Disable(r.Context(), name); err != nil {
		moduleBounce(w, r, b.cfg.PathPrefix, moduleErrText("disable", name, err))
		return
	}
	actor := adminActorID(r.Context())
	b.appendAudit(r.Context(), modulesAuditEnt, opModuleDisable, name, actor, nil)
	http.Redirect(w, r, b.cfg.PathPrefix+modulesListPath, http.StatusSeeOther)
}

func (b *Battery) handleModuleBump(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("module"))
	if name == "" {
		moduleBounce(w, r, b.cfg.PathPrefix, "module name required")
		return
	}
	gen, err := b.cfg.ProcessModules.BumpGeneration(r.Context(), name)
	if err != nil {
		moduleBounce(w, r, b.cfg.PathPrefix, moduleErrText("bump generation", name, err))
		return
	}
	actor := adminActorID(r.Context())
	b.appendAudit(r.Context(), modulesAuditEnt, opModuleBump, name, actor,
		map[string]any{"generation": gen})
	http.Redirect(w, r, b.cfg.PathPrefix+modulesListPath, http.StatusSeeOther)
}

func (b *Battery) handleModuleRevoke(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("module"))
	grant := strings.TrimSpace(r.FormValue("grant"))
	if name == "" || grant == "" {
		moduleBounce(w, r, b.cfg.PathPrefix, "module name and capability required")
		return
	}
	gen, err := b.cfg.ProcessModules.RevokeGrants(r.Context(), name, []access.Permission{access.Permission(grant)})
	if err != nil {
		moduleBounce(w, r, b.cfg.PathPrefix, moduleErrText("revoke", name, err))
		return
	}
	actor := adminActorID(r.Context())
	b.appendAudit(r.Context(), modulesAuditEnt, opModuleRevoke, name, actor,
		map[string]any{"grant": grant, "generation": gen})
	http.Redirect(w, r, b.cfg.PathPrefix+modulesListPath, http.StatusSeeOther)
}

// moduleBounce redirects (303) back to the list with an ?err= flash. The
// message is query-encoded; the GET handler renders it HTML-escaped.
func moduleBounce(w http.ResponseWriter, r *http.Request, prefix, msg string) {
	http.Redirect(w, r, prefix+modulesListPath+"?err="+url.QueryEscape(msg), http.StatusSeeOther)
}

// moduleErrText reduces a controller error to an operator-safe message. The
// framework already returns curated errors (e.g. ErrNoDesiredRow); we prefix
// the action so the flash reads "enable billing: no desired-state row".
func moduleErrText(action, name string, err error) string {
	return fmt.Sprintf("%s %s: %v", action, name, err)
}
