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
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/auth"
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
// grant/revoke. The set of selectable permissions is the union of all
// currently-granted permissions (there is no capability catalog) plus
// free-text entry for new permission strings.
func (b *Battery) handleRBACRoles(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Policy == nil {
		b.writePage(w, b.cfg.Title, "Roles",
			render.Raw(`<p class="muted">No RBAC policy wired.</p>`))
		return
	}
	roles := b.cfg.Policy.Roles()

	// Union of all granted permissions — the selectable set shown in the
	// grant dropdown. There is no capability catalog; this set is
	// "permissions already in use". Free-text add is also allowed.
	permSet := make(map[string]bool)
	for _, role := range roles {
		for _, p := range b.cfg.Policy.PermissionsOf(role) {
			permSet[string(p)] = true
		}
	}
	allPerms := make([]string, 0, len(permSet))
	for p := range permSet {
		allPerms = append(allPerms, p)
	}
	sort.Strings(allPerms)

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

		// Permission badges with revoke buttons.
		var badges strings.Builder
		for _, p := range permLabels {
			if b.cfg.GrantStore != nil {
				fmt.Fprintf(&badges, `<span class="badge">%s <form method="post" action="%s/rbac/_revoke" class="inline-form">`+
					`<input type="hidden" name="_csrf" value="%s">`+
					`<input type="hidden" name="role" value="%s">`+
					`<input type="hidden" name="permission" value="%s">`+
					`<button type="submit" class="badge-remove" aria-label="Revoke %s from %s">✕</button>`+
					`</form></span> `,
					render.Escape(p), b.cfg.PathPrefix, render.Escape(csrf),
					render.Escape(role), render.Escape(p),
					render.Escape(p), render.Escape(role))
			} else {
				fmt.Fprintf(&badges, `<span class="badge">%s</span> `, render.Escape(p))
			}
		}
		if len(permLabels) == 0 {
			badges.WriteString(`<span class="muted">—</span>`)
		}

		// Grant form: dropdown of known perms + free-text input.
		var grantForm strings.Builder
		if b.cfg.GrantStore != nil {
			grantForm.WriteString(fmt.Sprintf(`<form method="post" action="%s/rbac/_grant" class="inline-form">`,
				b.cfg.PathPrefix))
			fmt.Fprintf(&grantForm, `<input type="hidden" name="_csrf" value="%s">`, render.Escape(csrf))
			fmt.Fprintf(&grantForm, `<input type="hidden" name="role" value="%s">`, render.Escape(role))
			grantForm.WriteString(`<select name="permission">`)
			for _, p := range allPerms {
				fmt.Fprintf(&grantForm, `<option value="%s">%s</option>`, render.Escape(p), render.Escape(p))
			}
			grantForm.WriteString(`</form>`)
			grantForm.WriteString(`<input type="text" name="permission" placeholder="new:perm" class="perm-input">`)
			grantForm.WriteString(`<button type="submit">Grant</button>`)
		}

		fmt.Fprintf(&sb, `<tr><td><code>%s</code></td><td>%s</td><td>%s</td></tr>`,
			render.Escape(role), badges.String(), grantForm.String())
	}
	sb.WriteString(`</tbody></table>`)

	// Add-role form (creates a role with an initial permission).
	if b.cfg.GrantStore != nil {
		fmt.Fprintf(&sb, `<h3>Add role</h3>`+
			`<form method="post" action="%s/rbac/_grant">`+
			`<input type="hidden" name="_csrf" value="%s">`+
			`<input type="text" name="role" placeholder="role-name" required> `+
			`<input type="text" name="permission" placeholder="perm:verb" required> `+
			`<button type="submit">Grant</button></form>`,
			b.cfg.PathPrefix, render.Escape(csrf))
	}

	body := section("Roles & Permissions", render.Raw(sb.String()))
	b.writePage(w, b.cfg.Title, "Roles", body)
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
		roles := u.GetRoles()
		rolesStr := strings.Join(roles, ", ")
		if rolesStr == "" {
			rolesStr = "—"
		}

		fmt.Fprintf(&sb, `<tr><td>%s</td><td>%s</td><td>`+
			`<form method="post" action="%s/rbac/_assign" class="inline-form">`+
			`<input type="hidden" name="_csrf" value="%s">`+
			`<input type="hidden" name="user_id" value="%s">`+
			`<input type="text" name="roles" value="%s" placeholder="role1,role2" list="known-roles">`+
			`<button type="submit">Save</button></form></td></tr>`,
			render.Escape(u.GetEmail()),
			render.Escape(rolesStr),
			b.cfg.PathPrefix, render.Escape(csrf),
			render.Escape(u.GetID()),
			render.Escape(rolesStr))
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
