package admin

// Process-module operator lifecycle screen (issue #37, design §5/§8). Lists
// every supervised process module with its introspection fields and exposes
// the four operator levers: enable, disable, bump-generation (the circuit
// reset / recovery lever), and per-grant revoke. Every mutation is a CSRF'd
// POST that writes an audit row (via framework.AppendAuditEvent, exactly as
// the RBAC grant/revoke handlers do) and 303-redirects back to the list.
//
// The screen is the same standalone SSR pipeline as the RBAC screens —
// b.writePage + section(...) + core-ui/html typed configs — and gates behind
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
// panic — the generated-app rule). A controller error on a prior POST is
// surfaced via the ?err= query param as a <p class="err"> flash above the
// table — never a raw 500 or JSON leak.
func (b *Battery) handleProcessModules(w http.ResponseWriter, r *http.Request) {
	modules := b.cfg.ProcessModules.List()

	csrf := middleware.TokenFromContext(r.Context())

	var sb strings.Builder
	if errMsg := strings.TrimSpace(r.URL.Query().Get("err")); errMsg != "" {
		// Operator-facing flash. The message is operator-safe (controller
		// errors name the module/state, never secrets); render.Escape keeps
		// it HTML-safe.
		fmt.Fprintf(&sb, `<p class="err" role="alert">%s</p>`, render.Escape(errMsg))
	}

	if len(modules) == 0 {
		sb.WriteString(`<p class="muted">No process modules registered.</p>`)
		body := section("Process Modules", render.Raw(sb.String()))
		b.writePage(w, b.cfg.Title, "Modules", body)
		return
	}

	sb.WriteString(`<table><thead><tr>` +
		`<th>Module</th><th>Trust</th><th>State</th><th>Generation</th>` +
		`<th>Restarts</th><th>Routes / Tools</th><th>Last exit</th><th>Actions</th>` +
		`</tr></thead><tbody>`)
	for _, m := range modules {
		fmt.Fprintf(&sb, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%d / %d</td><td>%s</td><td>%s</td></tr>`,
			moduleNameCell(m),
			render.Escape(m.TrustTier.String()),
			moduleStateCell(m),
			moduleGenerationCell(m),
			m.RestartCount,
			m.RouteCount, m.ToolCount,
			moduleLastExitCell(m.LastExit),
			moduleActionsCell(b.cfg.PathPrefix, csrf, m),
		)
	}
	sb.WriteString(`</tbody></table>`)

	body := section("Process Modules", render.Raw(sb.String()))
	b.writePage(w, b.cfg.Title, "Modules", body)
}

// moduleNameCell renders the name (monospace via <code>, matching the RBAC
// role cell) plus a quiet version line when the descriptor carries one.
func moduleNameCell(m framework.ProcessModuleInfo) render.HTML {
	if m.Version == "" {
		return render.HTML(fmt.Sprintf(`<code>%s</code>`, render.Escape(m.Name)))
	}
	return render.HTML(fmt.Sprintf(`<code>%s</code> <small class="muted">%s</small>`,
		render.Escape(m.Name), render.Escape(m.Version)))
}

// moduleStateCell renders the state name plus the 404-vs-503 meaning
// (design §8 decision D — the operator must read a disabled module
// differently from a crashed one). Circuit-open and lease-failing are shown
// prominently (red, role=status) since both mean serving failures right now.
func moduleStateCell(m framework.ProcessModuleInfo) render.HTML {
	var sb strings.Builder
	stateCls := "muted"
	switch m.State {
	case framework.StateReady:
		stateCls = ""
	case framework.StateInstalledDisabled, framework.StateDrainingDisable, framework.StateAbsent:
		stateCls = "muted"
	default:
		// Starting/Handshaking/Crashed/Backoff/DrainingUpgrade/Failed —
		// enabled but not serving; reads as trouble.
		stateCls = "err"
	}
	if stateCls != "" {
		fmt.Fprintf(&sb, `<span class="%s">%s</span>`, stateCls, render.Escape(m.State.String()))
	} else {
		fmt.Fprintf(&sb, `<span>%s</span>`, render.Escape(m.State.String()))
	}

	// The 404-vs-503 semantics, in copy. This is the operator-readable
	// signal that distinguishes "uninstalled-looking" from "retryable".
	_, code := moduleHTTPSemantics(m.State)
	fmt.Fprintf(&sb, ` <small class="muted">%s</small>`, render.Escape(code))

	if m.CircuitOpen {
		sb.WriteString(` <span class="err" role="status">Circuit open</span>`)
	}
	if m.LeaseFailing {
		// Lease-failing means fail-closed / serving 503 right now — the
		// loudest signal on the page short of a crash.
		sb.WriteString(` <span class="err" role="status">Lease failing</span>`)
	}
	return render.HTML(sb.String())
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
	if m.ObservedGeneration < m.DesiredGeneration {
		return render.HTML(fmt.Sprintf(`<span class="err">%d / %d</span>`,
			m.DesiredGeneration, m.ObservedGeneration))
	}
	return render.HTML(fmt.Sprintf(`<span>%d / %d</span>`,
		m.DesiredGeneration, m.ObservedGeneration))
}

// moduleLastExitCell renders the last exit reason quietly, or an em-dash when
// the module has never exited.
func moduleLastExitCell(last string) render.HTML {
	if strings.TrimSpace(last) == "" {
		return render.HTML(`<span class="muted">—</span>`)
	}
	return render.HTML(fmt.Sprintf(`<span class="muted">%s</span>`, render.Escape(last)))
}

// moduleActionsCell renders the per-row lifecycle levers as CSRF'd inline
// POST forms (same shape as the RBAC grant/revoke forms). Disable and Revoke
// carry data-fui-confirm — the existing destructive-action affordance, no
// new JS. Enable/Disable choose based on state so the operator is offered
// the action that actually changes something.
func moduleActionsCell(prefix, csrf string, m framework.ProcessModuleInfo) render.HTML {
	var sb strings.Builder

	if moduleIsDisabled(m.State) {
		sb.WriteString(string(moduleActionForm(prefix, csrf, m.Name, "enable", "Enable", "", false)))
	} else {
		sb.WriteString(string(moduleActionForm(prefix, csrf, m.Name, "disable", "Disable",
			"Disable module "+m.Name+"? It will drain and stop serving.", true)))
	}

	// Bump generation = the recovery / circuit-reset lever (design §8).
	sb.WriteString(string(moduleActionForm(prefix, csrf, m.Name, "bump", "Bump generation", "", false)))

	// Revoke a single capability (free-text resource:verb; bumps generation).
	sb.WriteString(string(moduleRevokeForm(prefix, csrf, m.Name)))
	return render.HTML(sb.String())
}

// moduleIsDisabled reports whether the current state means "not serving,
// route gate 404s" — i.e. Enable is the meaningful action.
func moduleIsDisabled(state framework.ProcessState) bool {
	switch state {
	case framework.StateInstalledDisabled, framework.StateDrainingDisable, framework.StateAbsent, framework.StateFailed:
		return true
	}
	return false
}

// moduleActionForm renders a single-submit inline form for a named action.
// confirm, when non-empty, sets data-fui-confirm (the existing runtime
// affordance — no new JS). danger switches the button to the link-danger
// style for destructive actions.
func moduleActionForm(prefix, csrf, name, action, label, confirm string, danger bool) render.HTML {
	btnClass := ""
	if danger {
		btnClass = "link-danger"
	}
	attrs := html.Attrs{}
	if confirm != "" {
		attrs["data-fui-confirm"] = confirm
	}
	return html.Form(html.FormConfig{
		Method: "post",
		Action: prefix + modulesListPath + "/_" + action,
		Class:  "inline-form",
	},
		html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrf}),
		html.Input(html.InputConfig{Type: "hidden", Name: "module", Value: name}),
		html.Button(html.ButtonConfig{Type: "submit", Label: label, Class: btnClass, ExtraAttrs: attrs}),
	)
}

// moduleRevokeForm renders the revoke-grant inline form: a free-text
// resource:verb input (same shape as the RBAC grant input) plus the revoke
// submit. Destructive → data-fui-confirm.
func moduleRevokeForm(prefix, csrf, name string) render.HTML {
	return html.Form(html.FormConfig{
		Method: "post",
		Action: prefix + modulesListPath + "/_revoke",
		Class:  "inline-form",
	},
		html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrf}),
		html.Input(html.InputConfig{Type: "hidden", Name: "module", Value: name}),
		html.Input(html.InputConfig{
			Type:        "text",
			Name:        "grant",
			Placeholder: "resource:verb",
			Class:       "perm-input",
			ExtraAttrs:  html.Attrs{"required": "required", "aria-label": "Capability to revoke from " + name},
		}),
		html.Button(html.ButtonConfig{
			Type:       "submit",
			Label:      "Revoke",
			Class:      "link-danger",
			ExtraAttrs: html.Attrs{"data-fui-confirm": "Revoke this capability from " + name + "? Its generation bumps and the child restarts."},
		}),
	)
}

// ----- POST handlers --------------------------------------------------------
//
// Each handler validates the module name, calls the controller, writes an
// audit row, and 303-redirects to the list. On any error (validation,
// controller, unknown module) it 303-redirects with ?err=<message> so the
// failure surfaces as a flash on the list page — never a raw 500 or JSON
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
	_ = framework.AppendAuditEvent(r.Context(), b.effectiveDB(), b.cfg.AuditTable,
		modulesAuditEnt, opModuleEnable, name, actor, nil)
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
	_ = framework.AppendAuditEvent(r.Context(), b.effectiveDB(), b.cfg.AuditTable,
		modulesAuditEnt, opModuleDisable, name, actor, nil)
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
	_ = framework.AppendAuditEvent(r.Context(), b.effectiveDB(), b.cfg.AuditTable,
		modulesAuditEnt, opModuleBump, name, actor,
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
	_ = framework.AppendAuditEvent(r.Context(), b.effectiveDB(), b.cfg.AuditTable,
		modulesAuditEnt, opModuleRevoke, name, actor,
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
