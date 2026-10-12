package admin

// The Modules page lists each supervised process module and offers the
// operator levers: enable, disable, bump generation (the circuit reset)
// and revoke one capability. Each lever needs the modulesManage
// permission on the caller's own roles, and each change, or refusal,
// writes an audit row. Outcomes come from the fixed result names, so
// the page never prints request text.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// processModuleController is what the page uses of the supervisor;
// *framework.ProcessModuleSupervisor satisfies it, and tests pass a fake
// so no child process is spawned.
type processModuleController interface {
	List() []framework.ProcessModuleInfo
	Enable(ctx context.Context, name string) error
	Disable(ctx context.Context, name string) error
	RevokeGrants(ctx context.Context, name string, grants []access.Permission) (uint64, error)
	BumpGeneration(ctx context.Context, name string) (uint64, error)
}

var _ processModuleController = (*framework.ProcessModuleSupervisor)(nil)

// modulesManage is the permission every module lever needs, held through
// the caller's own roles in Config.Policy (Wildcard counts). Without a
// policy nothing can be proven held, so every lever is refused.
const modulesManage access.Permission = "modules:manage"

// The audit entity and operations module changes record.
const (
	modulesAuditEnt = "module"
	opModuleEnable  = "module_enable"
	opModuleDisable = "module_disable"
	opModuleBump    = "module_bump"
	opModuleRevoke  = "module_revoke"
)

// renderModules draws the Modules page. A caller without modulesManage
// sees the table without its levers.
func (b *Battery) renderModules(ctx context.Context, _ map[string]string) render.HTML {
	modules := b.cfg.ProcessModules.List()
	manage := b.callerHoldsPermission(ctx, modulesManage)
	cols := []ui.Column{
		{Key: "module", Header: i18nui.T(ctx, i18nui.KeyAdminColModule)},
		{Key: "trust", Header: i18nui.T(ctx, i18nui.KeyAdminColTrust)},
		{Key: "state", Header: i18nui.T(ctx, i18nui.KeyAdminColState)},
		{Key: "gen", Header: i18nui.T(ctx, i18nui.KeyAdminColGeneration), Align: "end"},
		{Key: "restarts", Header: i18nui.T(ctx, i18nui.KeyAdminColRestarts), Align: "end"},
		{Key: "routes", Header: i18nui.T(ctx, i18nui.KeyAdminColRoutes), Align: "end"},
		{Key: "exit", Header: i18nui.T(ctx, i18nui.KeyAdminColLastExit)},
	}
	if manage {
		cols = append(cols, ui.Column{Key: "actions", Header: i18nui.T(ctx, i18nui.KeyAdminColActions)})
	}
	rows := make([]ui.Row, len(modules))
	for i, m := range modules {
		cells := map[string]render.HTML{
			"module":   moduleNameCell(m),
			"trust":    render.Text(m.TrustTier.String()),
			"state":    moduleStateCell(ctx, m),
			"gen":      moduleGenerationCell(m),
			"restarts": render.Text(strconv.Itoa(m.RestartCount)),
			"routes":   render.Text(fmt.Sprintf("%d / %d", m.RouteCount, m.ToolCount)),
			"exit":     moduleLastExitCell(m.LastExit),
		}
		if manage {
			cells["actions"] = b.moduleActions(ctx, m)
		}
		rows[i] = ui.Row{ID: m.Name, Cells: cells}
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.T(ctx, i18nui.KeyAdminModules),
			Subtitle: i18nui.T(ctx, i18nui.KeyAdminModulesSub),
		}),
		resultNotice(ctx),
		ui.DataTable(ui.DataTableConfig{
			Columns:       cols,
			Rows:          rows,
			Caption:       i18nui.T(ctx, i18nui.KeyAdminModules),
			CaptionHidden: true,
			Responsive:    ui.ResponsiveScroll,
			Ctx:           ctx,
			Empty:         ui.EmptyStateConfig{Title: i18nui.T(ctx, i18nui.KeyAdminNoModules), HeadingLevel: 2},
		}),
	)
}

// moduleNameCell is the name, with the version beside it when set.
func moduleNameCell(m framework.ProcessModuleInfo) render.HTML {
	name := html.Code(html.TextConfig{}, render.Text(m.Name))
	if m.Version == "" {
		return name
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, Align: ui.AlignCenter}, name, ui.Muted(render.Text(m.Version)))
}

// moduleStateCell is the state and what the module's routes answer
// while in it: a disabled module serves 404 (it reads as uninstalled), an
// enabled one that is not ready serves 503. An open circuit and a failing
// lease are flagged.
func moduleStateCell(ctx context.Context, m framework.ProcessModuleInfo) render.HTML {
	var items []render.HTML
	switch {
	case m.State == framework.StateReady:
		items = append(items, render.Text(m.State.String()))
	case moduleIsDisabled(m.State) && m.State != framework.StateFailed:
		items = append(items, ui.Muted(render.Text(m.State.String())))
	default:
		items = append(items, ui.StatusBadge(ui.StatusBadgeConfig{Label: m.State.String(), Variant: ui.StatusWarning}))
	}
	items = append(items, ui.Muted(render.Text(i18nui.T(ctx, moduleServes(m.State)))))
	if m.CircuitOpen {
		items = append(items, ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.T(ctx, i18nui.KeyAdminCircuitOpen), Variant: ui.StatusDanger}))
	}
	if m.LeaseFailing {
		items = append(items, ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.T(ctx, i18nui.KeyAdminLeaseFailing), Variant: ui.StatusDanger}))
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, Align: ui.AlignCenter}, items...)
}

// moduleServes is what a module's routes answer in state.
func moduleServes(state framework.ProcessState) i18nui.Key {
	switch state {
	case framework.StateInstalledDisabled, framework.StateDrainingDisable, framework.StateAbsent:
		return i18nui.KeyAdminServes404
	case framework.StateReady:
		return i18nui.KeyAdminServing
	}
	return i18nui.KeyAdminServes503
}

// moduleGenerationCell is desired / observed; a lagging observed
// generation means convergence is in flight.
func moduleGenerationCell(m framework.ProcessModuleInfo) render.HTML {
	text := fmt.Sprintf("%d / %d", m.DesiredGeneration, m.ObservedGeneration)
	if m.ObservedGeneration < m.DesiredGeneration {
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: text, Variant: ui.StatusDanger})
	}
	return render.Text(text)
}

// moduleLastExitCell is the last exit reason, or a dash.
func moduleLastExitCell(last string) render.HTML {
	if strings.TrimSpace(last) == "" {
		return ui.Muted(render.Text("—"))
	}
	return ui.Muted(render.Text(last))
}

// moduleIsDisabled reports a state in which Enable is the lever that
// changes something.
func moduleIsDisabled(state framework.ProcessState) bool {
	switch state {
	case framework.StateInstalledDisabled, framework.StateDrainingDisable, framework.StateAbsent, framework.StateFailed:
		return true
	}
	return false
}

// moduleActions are one module's levers.
func (b *Battery) moduleActions(ctx context.Context, m framework.ProcessModuleInfo) render.HTML {
	page := b.cfg.PathPrefix + "/modules"
	op := func(action string, label i18nui.Key, variant ui.ButtonVariant, confirm string) render.HTML {
		return b.opForm(ctx, opSpec{
			path: page + "/_" + action, page: page,
			label: i18nui.T(ctx, label), variant: variant, small: true, confirm: confirm,
			fields: map[string]string{"module": m.Name},
		})
	}
	vars := map[string]string{"module": m.Name}
	var forms []render.HTML
	if moduleIsDisabled(m.State) {
		forms = append(forms, op("enable", i18nui.KeyAdminEnable, ui.ButtonSecondary, ""))
	} else {
		forms = append(forms, op("disable", i18nui.KeyAdminDisable, ui.ButtonDanger, i18nui.TVars(ctx, i18nui.KeyAdminDisableConfirm, vars)))
	}
	forms = append(forms, op("bump", i18nui.KeyAdminBump, ui.ButtonGhost, ""))
	forms = append(forms, b.opForm(ctx, opSpec{
		path: page + "/_revoke", page: page,
		label: i18nui.T(ctx, i18nui.KeyAdminRevoke), variant: ui.ButtonDanger, small: true,
		confirm: i18nui.TVars(ctx, i18nui.KeyAdminRevokeConfirm, vars),
		fields:  map[string]string{"module": m.Name},
		body: []render.HTML{ui.TextField(ui.TextFieldConfig{
			Name: "grant", Label: i18nui.T(ctx, i18nui.KeyAdminCapability), ID: "admin-grant-" + m.Name,
			Placeholder: "resource:verb", Required: true,
		})},
	}))
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignEnd}, forms...)
}

// ----- posts --------------------------------------------------------------------

func (b *Battery) handleModuleEnable(w http.ResponseWriter, r *http.Request) {
	b.moduleOp(w, r, opModuleEnable, "module-enabled", func(ctx context.Context, name, _ string) (map[string]any, error) {
		return nil, b.cfg.ProcessModules.Enable(ctx, name)
	})
}

func (b *Battery) handleModuleDisable(w http.ResponseWriter, r *http.Request) {
	b.moduleOp(w, r, opModuleDisable, "module-disabled", func(ctx context.Context, name, _ string) (map[string]any, error) {
		return nil, b.cfg.ProcessModules.Disable(ctx, name)
	})
}

func (b *Battery) handleModuleBump(w http.ResponseWriter, r *http.Request) {
	b.moduleOp(w, r, opModuleBump, "module-bumped", func(ctx context.Context, name, _ string) (map[string]any, error) {
		gen, err := b.cfg.ProcessModules.BumpGeneration(ctx, name)
		return map[string]any{"generation": gen}, err
	})
}

func (b *Battery) handleModuleRevoke(w http.ResponseWriter, r *http.Request) {
	b.moduleOp(w, r, opModuleRevoke, "module-revoked", func(ctx context.Context, name, grant string) (map[string]any, error) {
		if grant == "" {
			return nil, errNoGrant
		}
		gen, err := b.cfg.ProcessModules.RevokeGrants(ctx, name, []access.Permission{access.Permission(grant)})
		return map[string]any{"grant": grant, "generation": gen}, err
	})
}

// errNoGrant is a revoke that names no capability.
var errNoGrant = errors.New("admin: revoke names no capability")

// moduleOp is the shared body of the module levers: read the post,
// require a module and modulesManage, run the call, audit, answer.
func (b *Battery) moduleOp(w http.ResponseWriter, r *http.Request, op, ok string,
	call func(ctx context.Context, name, grant string) (map[string]any, error)) {
	page := b.cfg.PathPrefix + "/modules"
	vals, read := b.readOps(w, r, page)
	if !read {
		return
	}
	name := strings.TrimSpace(vals.Get("module"))
	grant := strings.TrimSpace(vals.Get("grant"))
	if name == "" {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return
	}
	actor := adminActorID(r.Context())
	if !b.callerHoldsPermission(r.Context(), modulesManage) {
		b.appendAudit(r.Context(), modulesAuditEnt, op+"_refused", name, actor, nil)
		b.refuse(w, r, page, http.StatusForbidden, "module-refused")
		return
	}
	diff, err := call(r.Context(), name, grant)
	if errors.Is(err, errNoGrant) {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return
	}
	if err != nil {
		b.logger().Error("admin: module lever", "op", op, "module", textsafe.ScrubControlBytes(name), "error", textsafe.ScrubControlBytes(err.Error()))
		b.refuse(w, r, page, http.StatusConflict, "module-failed")
		return
	}
	b.appendAudit(r.Context(), modulesAuditEnt, op, name, actor, diff)
	b.done(w, r, page, ok)
}
