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
			render.Raw(`<p class="muted">No RBAC policy wired.</p>`))
		return
	}
	roles := b.cfg.Policy.Roles()
	capabilities := b.cfg.Policy.Capabilities()
	capabilitySet := make(map[access.Permission]struct{}, len(capabilities))
	for _, capability := range capabilities {
		capabilitySet[capability] = struct{}{}
	}

	csrf := middleware.TokenFromContext(r.Context())

	var sb strings.Builder
	sb.WriteString(`<table><thead><tr><th>Role</th><th>Permissions</th><th>Actions</th></tr></thead><tbody>`)
	for _, role := range roles {
		perms := b.cfg.Policy.PermissionsOf(role)
		permLabels := make([]string, len(perms))
		for i, p := range perms {
			permLabels[i] = string(p)
		}
		sort.Strings(permLabels)

		// Permission badges with revoke buttons. Once a registry exists, any
		// non-global grant outside it is dead configuration and is called out.
		var badges strings.Builder
		for _, p := range permLabels {
			flag := ""
			if _, known := capabilitySet[access.Permission(p)]; len(capabilities) > 0 && !known && access.Permission(p) != access.Wildcard {
				flag = " " + string(html.Span(html.TextConfig{
					Class: "err",
					ExtraAttrs: html.Attrs{
						"role":       "status",
						"aria-label": "Unknown capability; this grant will never match",
					},
				}, render.Text("unknown/dead")))
			}
			if b.cfg.GrantStore != nil {
				fmt.Fprintf(&badges, `<span class="badge">%s%s <form method="post" action="%s/rbac/_revoke" class="inline-form">`+
					`<input type="hidden" name="_csrf" value="%s">`+
					`<input type="hidden" name="role" value="%s">`+
					`<input type="hidden" name="permission" value="%s">`+
					`<button type="submit" class="badge-remove" aria-label="Revoke %s from %s">✕</button>`+
					`</form></span> `,
					render.Escape(p), flag, b.cfg.PathPrefix, render.Escape(csrf),
					render.Escape(role), render.Escape(p),
					render.Escape(p), render.Escape(role))
			} else {
				fmt.Fprintf(&badges, `<span class="badge">%s%s</span> `, render.Escape(p), flag)
			}
		}
		if len(permLabels) == 0 {
			badges.WriteString(`<span class="muted">—</span>`)
		}

		// Grant form: a free-text input backed by the registry datalist when
		// capabilities are known.
		var grantForm strings.Builder
		if b.cfg.GrantStore != nil {
			grantForm.WriteString(string(html.Form(html.FormConfig{
				Method: "post",
				Action: b.cfg.PathPrefix + "/rbac/_grant",
				Class:  "inline-form",
			},
				html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrf}),
				html.Input(html.InputConfig{Type: "hidden", Name: "role", Value: role}),
				capabilityInput(capabilities, "new:perm", false),
				html.Button(html.ButtonConfig{Type: "submit", Label: "Grant"}),
			)))
		}

		fmt.Fprintf(&sb, `<tr><td><code>%s</code></td><td>%s</td><td>%s</td></tr>`,
			render.Escape(role), badges.String(), grantForm.String())
	}
	sb.WriteString(`</tbody></table>`)

	// Add-role form (creates a role with an initial permission).
	if b.cfg.GrantStore != nil {
		sb.WriteString(`<h3>Add role</h3>`)
		sb.WriteString(string(html.Form(html.FormConfig{
			Method: "post",
			Action: b.cfg.PathPrefix + "/rbac/_grant",
		},
			html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrf}),
			html.Input(html.InputConfig{
				Type:        "text",
				Name:        "role",
				Placeholder: "role-name",
				ExtraAttrs:  html.Attrs{"required": "required"},
			}),
			render.Text(" "),
			capabilityInput(capabilities, "perm:verb", true),
			render.Text(" "),
			html.Button(html.ButtonConfig{Type: "submit", Label: "Grant"}),
		)))
	}
	if len(capabilities) > 0 {
		sb.WriteString(string(capabilityDatalist(capabilities)))
	}

	body := section("Roles & Permissions", render.Raw(sb.String()))
	b.writePage(w, b.cfg.Title, "Roles", body)
}

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
		Class:       "perm-input",
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
			render.Raw(`<p class="muted">No auth manager wired.</p>`))
		return
	}
	users, total, err := b.cfg.Auth.ListUsers(r.Context(), listUsersOpts(r))
	if err != nil {
		b.writePage(w, b.cfg.Title, "User roles",
			render.Raw(`<p class="err">Could not load users. Check the server logs.</p>`))
		return
	}

	// Known roles for the dropdown suggestions.
	var knownRoles []string
	if b.cfg.Policy != nil {
		knownRoles = b.cfg.Policy.Roles()
	}

	csrf := middleware.TokenFromContext(r.Context())

	var sb strings.Builder
	sb.WriteString(`<table><thead><tr><th>Email</th><th>Current roles</th><th>Set roles</th></tr></thead><tbody>`)
	for _, u := range users {
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

		fmt.Fprintf(&sb, `<tr><td>%s</td><td>%s</td><td>`+
			`<form method="post" action="%s/rbac/_assign" class="inline-form">`+
			`<input type="hidden" name="_csrf" value="%s">`+
			`<input type="hidden" name="user_id" value="%s">`+
			`<input type="text" name="roles" value="%s" placeholder="role1,role2" list="known-roles">`+
			`<button type="submit">Save</button></form></td></tr>`,
			render.Escape(u.GetEmail()),
			render.Escape(displayRoles),
			b.cfg.PathPrefix, render.Escape(csrf),
			render.Escape(u.GetID()),
			render.Escape(directRolesStr))
	}
	sb.WriteString(`</tbody></table>`)

	// Datalist of known roles for autocomplete.
	if len(knownRoles) > 0 {
		sb.WriteString(`<datalist id="known-roles">`)
		for _, r := range knownRoles {
			fmt.Fprintf(&sb, `<option value="%s">`, render.Escape(r))
		}
		sb.WriteString(`</datalist>`)
	}

	if total > len(users) {
		fmt.Fprintf(&sb, `<p class="muted">Showing %d of %d users.</p>`, len(users), total)
	}

	body := section("User Roles", render.Raw(sb.String()))
	b.writePage(w, b.cfg.Title, "User roles", body)
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
		var unknown *access.UnknownCapabilityError
		if errors.As(err, &unknown) {
			http.Error(w, unknown.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "grant failed; check server logs", http.StatusInternalServerError)
		return
	}
	// Audit: op="grant", subject=role, diff={permission:perm}.
	actor := adminActorID(r.Context())
	_ = framework.AppendAuditEvent(r.Context(), b.effectiveDB(), b.cfg.AuditTable,
		"access", "grant", role, actor,
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
	_ = framework.AppendAuditEvent(r.Context(), b.effectiveDB(), b.cfg.AuditTable,
		"access", "revoke", role, actor,
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
	for _, r := range strings.Split(rolesRaw, ",") {
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
	_ = framework.AppendAuditEvent(r.Context(), b.effectiveDB(), b.cfg.AuditTable,
		"access", "assign-roles", userID, actor,
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
