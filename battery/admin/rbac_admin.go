package admin

// RBAC admin screens: a role→permission matrix and a user→role assignment
// page. Both are standalone server-rendered HTML (same pipeline as the
// queue/audit ops dashboards) gated by the admin default-deny gate. Every
// mutation (grant, revoke, assign-roles) writes an audit row via
// framework.AppendAuditEvent so changes land in audit_log and show at
// /admin/audit.
//
// Security: the screens + RPC routes are behind b.gate, which requires an
// authenticated admin. Role and permission strings are bound as $n params
// in GrantStore (never interpolated). There is no unauthenticated or
// self-service grant path.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	html "github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// adminActorID extracts the authenticated admin's user ID from the request
// context for audit logging. Returns "unknown" when no user is present
// (should not happen past the gate, but audit must never panic).
func adminActorID(ctx context.Context) string {
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return "unknown"
	}
	type ider interface{ GetID() string }
	if id, ok := u.(ider); ok {
		return id.GetID()
	}
	return "unknown"
}

// ----- role → permission matrix -------------------------------------------

// handleRBACRoles renders the role→permission matrix screen. Lists every
// role from Policy.Roles() with its granted permissions, plus forms to
// grant/revoke. A non-empty capability registry feeds the grant inputs'
// datalist while preserving free-text entry for backward compatibility.
func (b *Battery) handleRBACRoles(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Policy == nil {
		b.writePage(w, b.cfg.Title, "Roles",
			ui.Muted(render.Text("No RBAC policy wired.")))
		return
	}
	roles := b.cfg.Policy.Roles()
	capabilities := b.cfg.Policy.Capabilities()
	capabilitySet := make(map[access.Permission]struct{}, len(capabilities))
	for _, capability := range capabilities {
		capabilitySet[capability] = struct{}{}
	}

	csrf := middleware.TokenFromContext(r.Context())

	cols := []ui.Column{
		{Key: "role", Header: "Role"},
		{Key: "permissions", Header: "Permissions"},
		{Key: "actions", Header: "Actions"},
	}
	rows := make([]ui.Row, 0, len(roles))
	for _, role := range roles {
		perms := b.cfg.Policy.PermissionsOf(role)
		permLabels := make([]string, len(perms))
		for i, p := range perms {
			permLabels[i] = string(p)
		}
		sort.Strings(permLabels)
		rows = append(rows, ui.Row{Cells: map[string]render.HTML{
			"role":        monoCell(role),
			"permissions": permissionChips(role, permLabels, capabilitySet, capabilities, b.cfg.GrantStore, b.cfg.PathPrefix, csrf),
			"actions":     grantForm(b.cfg.PathPrefix, csrf, role, capabilities, b.cfg.GrantStore),
		}})
	}

	parts := []render.HTML{
		ui.DataTable(ui.DataTableConfig{
			Columns: cols,
			Rows:    rows,
			Empty:   ui.EmptyStateConfig{Title: "No roles", Description: "Define roles via your policy.", HeadingLevel: 3},
		}),
	}
	// Add-role form (creates a role with an initial permission).
	if b.cfg.GrantStore != nil {
		parts = append(parts, ui.Stack(ui.StackConfig{Gap: ui.GapSM},
			html.Heading(html.HeadingConfig{Level: 3}, render.Text("Add role")),
			addRoleForm(b.cfg.PathPrefix, csrf, capabilities),
		))
	}
	if len(capabilities) > 0 {
		parts = append(parts, capabilityDatalist(capabilities))
	}

	b.writePage(w, b.cfg.Title, "Roles",
		adminSection("Roles & Permissions", ui.Stack(ui.StackConfig{Gap: ui.GapMD}, parts...)))
}

// permissionChips renders one role's permissions as a Cluster of ui.Tag chips.
// A grant outside the capability registry (when one exists) is flagged with a
// danger StatusBadge; when grants persist, each chip is followed by a CSRF'd
// inline Revoke form. Replaces the orphan .badge/.badge-remove markup (which
// had no CSS anywhere) with real design-system components.
func permissionChips(role string, perms []string, capabilitySet map[access.Permission]struct{},
	capabilities []access.Permission, store *access.GrantStore, prefix, csrf string) render.HTML {
	if len(perms) == 0 {
		return ui.Muted(render.Text("—"))
	}
	items := make([]render.HTML, 0, len(perms)*3)
	for _, p := range perms {
		items = append(items, ui.Tag(ui.TagConfig{Label: p, Variant: ui.StatusNeutral}))
		// Once a registry exists, any non-global grant outside it is dead
		// configuration and is called out.
		if len(capabilities) > 0 {
			if _, known := capabilitySet[access.Permission(p)]; !known && access.Permission(p) != access.Wildcard {
				items = append(items, ui.StatusBadge(ui.StatusBadgeConfig{
					Label:   "unknown/dead",
					Variant: ui.StatusDanger,
				}))
			}
		}
		if store != nil {
			items = append(items, render.HTML(html.Form(html.FormConfig{
				Method: "post",
				Action: prefix + "/rbac/_revoke",
				Class:  "admin-inline",
			},
				html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrf}),
				html.Input(html.InputConfig{Type: "hidden", Name: "role", Value: role}),
				html.Input(html.InputConfig{Type: "hidden", Name: "permission", Value: p}),
				ui.Button(ui.ButtonConfig{
					Label:      "Revoke",
					Type:       "submit",
					Variant:    ui.ButtonGhost,
					Size:       ui.ButtonSizeSmall,
					ExtraAttrs: html.Attrs{"aria-label": "Revoke " + p + " from " + role},
				}),
			)))
		}
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS}, items...)
}

// grantForm renders the per-row "grant a permission" inline form, or nothing
// when grants do not persist.
func grantForm(prefix, csrf, role string, capabilities []access.Permission, store *access.GrantStore) render.HTML {
	if store == nil {
		return render.Text("")
	}
	return render.HTML(html.Form(html.FormConfig{
		Method: "post",
		Action: prefix + "/rbac/_grant",
		Class:  "admin-inline",
	},
		html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrf}),
		html.Input(html.InputConfig{Type: "hidden", Name: "role", Value: role}),
		capabilityInput(capabilities, "new:perm", false),
		ui.Button(ui.ButtonConfig{Label: "Grant", Type: "submit", Size: ui.ButtonSizeSmall}),
	))
}

// addRoleForm renders the "create a role with an initial permission" form.
func addRoleForm(prefix, csrf string, capabilities []access.Permission) render.HTML {
	return render.HTML(html.Form(html.FormConfig{
		Method: "post",
		Action: prefix + "/rbac/_grant",
		Class:  "admin-inline",
	},
		html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrf}),
		html.Input(html.InputConfig{
			Type:        "text",
			Name:        "role",
			Placeholder: "role-name",
			Class:       "admin-input",
			ExtraAttrs:  html.Attrs{"required": "required", "aria-label": "New role name"},
		}),
		render.Text(" "),
		capabilityInput(capabilities, "perm:verb", true),
		render.Text(" "),
		ui.Button(ui.ButtonConfig{Label: "Grant", Type: "submit"}),
	))
}

// capabilityInput renders the free-text permission input, wired to the
// capability datalist when capabilities are known.
func capabilityInput(capabilities []access.Permission, placeholder string, required bool) render.HTML {
	attrs := html.Attrs{}
	if len(capabilities) > 0 {
		attrs["list"] = "known-capabilities"
	}
	if required {
		attrs["required"] = "required"
	}
	return html.Input(html.InputConfig{
		Type:        "text",
		Name:        "permission",
		Placeholder: placeholder,
		Class:       "admin-input",
		ExtraAttrs:  attrs,
	})
}

func capabilityDatalist(capabilities []access.Permission) render.HTML {
	options := make([]render.HTML, 0, len(capabilities))
	for _, capability := range capabilities {
		options = append(options, html.Option(string(capability), "", false))
	}
	return render.Tag("datalist", map[string]string{"id": "known-capabilities"}, options...)
}

// ----- user → role assignment ---------------------------------------------

// handleRBACUsers renders the user→role assignment screen. Lists users
// (via AuthManager.ListUsers) with their current roles and a form to
// replace them via SetUserRoles.
func (b *Battery) handleRBACUsers(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Auth == nil {
		b.writePage(w, b.cfg.Title, "User roles",
			ui.Muted(render.Text("No auth manager wired.")))
		return
	}
	users, total, err := b.cfg.Auth.ListUsers(r.Context(), listUsersOpts(r))
	if err != nil {
		b.writePage(w, b.cfg.Title, "User roles",
			adminError("Could not load users. Check the server logs."))
		return
	}

	// Known roles for the dropdown suggestions.
	var knownRoles []string
	if b.cfg.Policy != nil {
		knownRoles = b.cfg.Policy.Roles()
	}

	csrf := middleware.TokenFromContext(r.Context())

	cols := []ui.Column{
		{Key: "email", Header: "Email"},
		{Key: "roles", Header: "Current roles"},
		{Key: "set", Header: "Set roles"},
	}
	rows := make([]ui.Row, len(users))
	for i, u := range users {
		directRoles := u.GetRoles()
		directRolesStr := strings.Join(directRoles, ", ")
		displayRoles := directRolesStr
		if b.cfg.EffectiveRoles != nil {
			effective := b.cfg.EffectiveRoles(r.Context(), u.GetID())
			displayRoles = strings.Join(roleOriginLabels(directRoles, effective), ", ")
		}
		if displayRoles == "" {
			displayRoles = "—"
		}
		rolesInput := html.InputConfig{
			Type: "text", Name: "roles", Value: directRolesStr,
			Placeholder: "role1,role2", Class: "admin-input",
		}
		if len(knownRoles) > 0 {
			rolesInput.ExtraAttrs = html.Attrs{"list": "known-roles"}
		}
		rows[i] = ui.Row{Cells: map[string]render.HTML{
			"email": render.Text(u.GetEmail()),
			"roles": render.Text(displayRoles),
			"set": render.HTML(html.Form(html.FormConfig{
				Method: "post",
				Action: b.cfg.PathPrefix + "/rbac/_assign",
				Class:  "admin-inline",
			},
				html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrf}),
				html.Input(html.InputConfig{Type: "hidden", Name: "user_id", Value: u.GetID()}),
				html.Input(rolesInput),
				ui.Button(ui.ButtonConfig{Label: "Save", Type: "submit", Size: ui.ButtonSizeSmall}),
			)),
		}}
	}

	parts := []render.HTML{
		ui.DataTable(ui.DataTableConfig{
			Columns: cols,
			Rows:    rows,
			Empty:   ui.EmptyStateConfig{Title: "No users", HeadingLevel: 3},
		}),
	}
	if total > len(users) {
		parts = append(parts, ui.Muted(render.Text(fmt.Sprintf("Showing %d of %d users.", len(users), total))))
	}
	// Datalist of known roles for autocomplete.
	if len(knownRoles) > 0 {
		opts := make([]render.HTML, 0, len(knownRoles))
		for _, rl := range knownRoles {
			opts = append(opts, html.Option(rl, "", false))
		}
		parts = append(parts, render.Tag("datalist", map[string]string{"id": "known-roles"}, opts...))
	}

	b.writePage(w, b.cfg.Title, "User roles",
		adminSection("User Roles", ui.Stack(ui.StackConfig{Gap: ui.GapMD}, parts...)))
}

func roleOriginLabels(direct []string, effective []access.RoleWithOrigin) []string {
	roles := make([]access.RoleWithOrigin, 0, len(direct)+len(effective))
	for _, role := range direct {
		if role != "" {
			roles = append(roles, access.RoleWithOrigin{Role: role, Origin: "direct"})
		}
	}
	for _, role := range effective {
		if role.Role == "" {
			continue
		}
		if role.Origin == "" {
			role.Origin = "resolved"
		}
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].Role == roles[j].Role {
			return roles[i].Origin < roles[j].Origin
		}
		return roles[i].Role < roles[j].Role
	})

	labels := make([]string, 0, len(roles))
	seen := make(map[access.RoleWithOrigin]struct{}, len(roles))
	for _, role := range roles {
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		labels = append(labels, fmt.Sprintf("%s (%s)", role.Role, role.Origin))
	}
	return labels
}

func listUsersOpts(r *http.Request) auth.ListUsersOptions {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := fmtAtoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := fmtAtoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return auth.ListUsersOptions{Limit: limit, Offset: offset}
}

func fmtAtoi(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// ----- RPC handlers --------------------------------------------------------

// handleRBACGrant grants a permission to a role via GrantStore.Grant. The
// role and permission are user-supplied strings — GrantStore binds them as
// $n parameters, never interpolating them into SQL. Writes an audit row.
func (b *Battery) handleRBACGrant(w http.ResponseWriter, r *http.Request) {
	if b.cfg.GrantStore == nil {
		http.Error(w, "grant store not wired", http.StatusNotImplemented)
		return
	}
	role := strings.TrimSpace(r.FormValue("role"))
	perm := strings.TrimSpace(r.FormValue("permission"))
	if role == "" || perm == "" {
		http.Error(w, "role and permission required", http.StatusBadRequest)
		return
	}
	if err := b.cfg.GrantStore.Grant(r.Context(), role, access.Permission(perm)); err != nil {
		// A strict-mode unknown capability is the admin's typo, not a
		// server fault — surface the reason instead of a generic 500.
		if unknown, ok := errors.AsType[*access.UnknownCapabilityError](err); ok {
			http.Error(w, unknown.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "grant failed; check server logs", http.StatusInternalServerError)
		return
	}
	// Audit: op="grant", subject=role, diff={permission:perm}. The grant
	// already committed; a failed audit row is logged, not swallowed.
	actor := adminActorID(r.Context())
	b.appendAudit(r.Context(), "access", "grant", role, actor,
		map[string]any{"permission": perm})
	http.Redirect(w, r, b.cfg.PathPrefix+"/rbac/roles", http.StatusSeeOther)
}

// handleRBACRevoke revokes a permission from a role via GrantStore.Revoke.
// Writes an audit row.
func (b *Battery) handleRBACRevoke(w http.ResponseWriter, r *http.Request) {
	if b.cfg.GrantStore == nil {
		http.Error(w, "grant store not wired", http.StatusNotImplemented)
		return
	}
	role := strings.TrimSpace(r.FormValue("role"))
	perm := strings.TrimSpace(r.FormValue("permission"))
	if role == "" || perm == "" {
		http.Error(w, "role and permission required", http.StatusBadRequest)
		return
	}
	if err := b.cfg.GrantStore.Revoke(r.Context(), role, access.Permission(perm)); err != nil {
		http.Error(w, "revoke failed; check server logs", http.StatusInternalServerError)
		return
	}
	actor := adminActorID(r.Context())
	b.appendAudit(r.Context(), "access", "revoke", role, actor,
		map[string]any{"permission": perm})
	http.Redirect(w, r, b.cfg.PathPrefix+"/rbac/roles", http.StatusSeeOther)
}

// handleRBACAssign replaces a user's roles via AuthManager.SetUserRoles.
// The roles are OPERATOR input from the admin screen — never request data
// sourced from the user being edited. Writes an audit row.
func (b *Battery) handleRBACAssign(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Auth == nil {
		http.Error(w, "auth manager not wired", http.StatusNotImplemented)
		return
	}
	userID := strings.TrimSpace(r.FormValue("user_id"))
	rolesRaw := strings.TrimSpace(r.FormValue("roles"))
	if userID == "" {
		http.Error(w, "user_id required", http.StatusBadRequest)
		return
	}
	// Parse comma-separated roles.
	var roles []string
	for r := range strings.SplitSeq(rolesRaw, ",") {
		r = strings.TrimSpace(r)
		if r != "" {
			roles = append(roles, r)
		}
	}
	if err := b.cfg.Auth.SetUserRoles(r.Context(), userID, roles); err != nil {
		http.Error(w, "assign failed; check server logs", http.StatusInternalServerError)
		return
	}
	actor := adminActorID(r.Context())
	b.appendAudit(r.Context(), "access", "assign-roles", userID, actor,
		map[string]any{"roles": roles})
	http.Redirect(w, r, b.cfg.PathPrefix+"/rbac/users", http.StatusSeeOther)
}

// effectiveDB returns the DB for audit writes: cfg.DB when set, else b.db
// (set by Init from app.DB).
func (b *Battery) effectiveDB() *sql.DB {
	if b.cfg.DB != nil {
		return b.cfg.DB
	}
	return b.db
}

// appendAudit writes a security/compliance audit row and surfaces a write
// failure via the configured logger instead of discarding it. The mutation
// that triggered the row has ALREADY committed by the time this runs (a grant
// was applied, a module enabled) — there is nothing to roll back and the
// client already got its answer — so failing the request would mislead the
// operator into believing the action did not take effect. But an unrecorded
// mutation is a silent security gap, so the failed write MUST be logged.
func (b *Battery) appendAudit(ctx context.Context, entity, op, recordID, actorID string, diff map[string]any) {
	if err := framework.AppendAuditEvent(ctx, b.effectiveDB(), b.cfg.AuditTable, entity, op, recordID, actorID, diff); err != nil {
		b.logger().Error("admin: audit write failed (mutation already applied)",
			"entity", entity, "op", op, "record_id", recordID, "actor_id", actorID, "error", err)
	}
}
