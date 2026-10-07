package admin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// adminActorID is the signed-in admin's user id, for audit rows.
// "unknown" when there is none, which the gate never lets through.
func adminActorID(ctx context.Context) string {
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return "unknown"
	}
	if id, ok := u.(interface{ GetID() string }); ok {
		return id.GetID()
	}
	return "unknown"
}

// ----- roles ------------------------------------------------------------------

// renderRoles draws the Roles page: each role with its permissions, and
// with a GrantStore, grant, revoke and a form that adds a role.
func (b *Battery) renderRoles(ctx context.Context, _ map[string]string) render.HTML {
	p := b.cfg.Policy
	caps := p.Capabilities()
	page := b.cfg.PathPrefix + "/rbac/roles"
	cols := []ui.Column{
		{Key: "role", Header: i18nui.T(ctx, i18nui.KeyAdminColRole)},
		{Key: "permissions", Header: i18nui.T(ctx, i18nui.KeyAdminColPermissions)},
	}
	if b.cfg.GrantStore != nil {
		cols = append(cols, ui.Column{Key: "actions", Header: i18nui.T(ctx, i18nui.KeyAdminGrant)})
	}
	roles := p.Roles()
	rows := make([]ui.Row, 0, len(roles))
	for _, role := range roles {
		cells := map[string]render.HTML{
			"role":        html.Code(html.TextConfig{}, render.Text(role)),
			"permissions": b.permissionChips(ctx, role, caps, page),
		}
		if b.cfg.GrantStore != nil {
			cells["actions"] = b.opForm(ctx, opSpec{
				path: b.cfg.PathPrefix + "/rbac/_grant", page: page,
				label: i18nui.T(ctx, i18nui.KeyAdminGrant), variant: ui.ButtonSecondary, small: true,
				fields: map[string]string{"role": role},
				body:   []render.HTML{permissionInput(ctx, caps, role)},
			})
		}
		rows = append(rows, ui.Row{ID: role, Cells: cells})
	}
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.T(ctx, i18nui.KeyAdminRoles),
			Subtitle: i18nui.T(ctx, i18nui.KeyAdminRolesSub),
		}),
		resultNotice(ctx),
		ui.DataTable(ui.DataTableConfig{
			Columns:       cols,
			Rows:          rows,
			Caption:       i18nui.T(ctx, i18nui.KeyAdminRoles),
			CaptionHidden: true,
			Responsive:    ui.ResponsiveScroll,
			Ctx:           ctx,
			Empty: ui.EmptyStateConfig{
				Title:        i18nui.T(ctx, i18nui.KeyAdminNoRoles),
				Description:  i18nui.T(ctx, i18nui.KeyAdminNoRolesDesc),
				HeadingLevel: 2,
			},
		}),
	}
	if b.cfg.GrantStore != nil {
		parts = append(parts, ui.Section(ui.SectionConfig{Heading: i18nui.T(ctx, i18nui.KeyAdminAddRole)},
			b.opForm(ctx, opSpec{
				path: b.cfg.PathPrefix + "/rbac/_grant", page: page,
				label: i18nui.T(ctx, i18nui.KeyAdminGrant), variant: ui.ButtonPrimary,
				body: []render.HTML{
					ui.TextField(ui.TextFieldConfig{Name: "role", Label: i18nui.T(ctx, i18nui.KeyAdminNewRole), Required: true}),
					permissionInput(ctx, caps, ""),
				},
			})))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, parts...)
}

// permissionChips draws a role's permissions as tags. With a capability
// registry, a grant outside it (other than Wildcard) is flagged; with a
// GrantStore each one has a Revoke.
func (b *Battery) permissionChips(ctx context.Context, role string, caps []access.Permission, page string) render.HTML {
	perms := b.cfg.Policy.PermissionsOf(role)
	if len(perms) == 0 {
		return ui.Muted(render.Text("—"))
	}
	slices.Sort(perms)
	items := make([]render.HTML, 0, len(perms))
	for _, perm := range perms {
		chip := []render.HTML{ui.Tag(ui.TagConfig{Label: string(perm)})}
		if len(caps) > 0 && perm != access.Wildcard && !slices.Contains(caps, perm) {
			chip = append(chip, ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.T(ctx, i18nui.KeyAdminUndeclared), Variant: ui.StatusDanger}))
		}
		if b.cfg.GrantStore != nil {
			chip = append(chip, b.opForm(ctx, opSpec{
				path: b.cfg.PathPrefix + "/rbac/_revoke", page: page,
				label:     i18nui.T(ctx, i18nui.KeyAdminRevoke),
				ariaLabel: i18nui.TVars(ctx, i18nui.KeyAdminRevokeLabel, map[string]string{"permission": string(perm), "role": role}),
				variant:   ui.ButtonGhost, small: true,
				fields: map[string]string{"role": role, "permission": string(perm)},
			}))
		}
		items = append(items, ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, Align: ui.AlignCenter, NoWrap: true}, chip...))
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, items...)
}

// permissionInput is the permission a grant names: a choice of the
// declared capabilities when the app declares them, else free text.
// suffix keeps each row's field id unique.
func permissionInput(ctx context.Context, caps []access.Permission, suffix string) render.HTML {
	label := i18nui.T(ctx, i18nui.KeyAdminPermission)
	id := "admin-perm"
	if suffix != "" {
		id += "-" + suffix
	}
	if len(caps) == 0 {
		return ui.TextField(ui.TextFieldConfig{Name: "permission", Label: label, ID: id, Required: true})
	}
	opts := make([]ui.SelectOption, len(caps))
	for i, c := range caps {
		opts[i] = ui.SelectOption{Value: string(c), Text: string(c)}
	}
	return ui.Select(ui.SelectConfig{Name: "permission", Label: label, ID: id, Options: opts, Required: true})
}

// ----- user roles ---------------------------------------------------------------

// usersPageSize is the User roles page's default page of users.
const usersPageSize = 50

// renderUsers draws the User roles page: each user and the roles they
// hold (with their origin when EffectiveRoles resolves more), and at
// the row's end an Edit roles dropdown whose form sets their direct
// roles. The roles show once; the form lives behind the row's action.
func (b *Battery) renderUsers(ctx context.Context, _ map[string]string) render.HTML {
	r := appui.RequestFromContext(ctx)
	opts := listUsersOpts(r)
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.T(ctx, i18nui.KeyAdminUserRoles),
		Subtitle: i18nui.T(ctx, i18nui.KeyAdminUserRolesSub),
	})
	users, total, err := b.cfg.Auth.ListUsers(ctx, opts)
	if err != nil {
		b.logger().Error("admin: list users", "error", err)
		return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, header,
			ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.T(ctx, i18nui.KeyAdminUsersLoadFailed))))
	}
	page := b.cfg.PathPrefix + "/rbac/users"
	if r != nil && r.URL.RawQuery != "" {
		page += "?" + r.URL.Query().Encode()
	}
	var known []string
	if b.cfg.Policy != nil {
		known = b.cfg.Policy.Roles()
	}
	cols := []ui.Column{
		{Key: "user", Header: i18nui.T(ctx, i18nui.KeyAdminColUser)},
		{Key: "roles", Header: i18nui.T(ctx, i18nui.KeyAdminColRoles)},
		{Key: "set", Header: i18nui.T(ctx, i18nui.KeyAdminColActions), Align: "end"},
	}
	rows := make([]ui.Row, len(users))
	for i, u := range users {
		direct := u.GetRoles()
		labels := direct
		if b.cfg.EffectiveRoles != nil {
			labels = roleOriginLabels(direct, b.cfg.EffectiveRoles(ctx, u.GetID()))
		}
		held := make([]render.HTML, 0, len(labels))
		for _, l := range labels {
			held = append(held, ui.Tag(ui.TagConfig{Label: l}))
		}
		heldCell := ui.Muted(render.Text("—"))
		if len(held) > 0 {
			heldCell = ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS}, held...)
		}
		rows[i] = ui.Row{ID: u.GetID(), Cells: map[string]render.HTML{
			"user":  render.Text(u.GetEmail()),
			"roles": heldCell,
			"set": ui.Dropdown(ui.DropdownConfig{
				ID:    "admin-roles-edit-" + u.GetID(),
				Label: i18nui.T(ctx, i18nui.KeyAdminEditRoles),
				Align: ui.DropdownEnd,
				Content: b.opForm(ctx, opSpec{
					path: b.cfg.PathPrefix + "/rbac/_assign", page: page,
					label: i18nui.T(ctx, i18nui.KeyAdminSaveRoles), variant: ui.ButtonPrimary,
					fields:  map[string]string{"user_id": u.GetID()},
					body:    []render.HTML{rolesInput(ctx, u.GetID(), direct, known)},
					stacked: true,
				}),
			}),
		}}
	}
	parts := []render.HTML{header, resultNotice(ctx),
		ui.DataTable(ui.DataTableConfig{
			Columns:       cols,
			Rows:          rows,
			Caption:       i18nui.T(ctx, i18nui.KeyAdminUserRoles),
			CaptionHidden: true,
			Responsive:    ui.ResponsiveScroll,
			Ctx:           ctx,
			Empty:         ui.EmptyStateConfig{Title: i18nui.T(ctx, i18nui.KeyAdminNoUsers), HeadingLevel: 2},
		}),
	}
	if total > len(users) {
		parts = append(parts, ui.Muted(render.Text(i18nui.TVars(ctx, i18nui.KeyAdminUsersShown,
			map[string]string{"shown": strconv.Itoa(len(users)), "total": strconv.Itoa(total)}))))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, parts...)
}

// rolesInput is a user's direct roles as a form field: a checkbox per
// role with a policy (the user's undeclared roles included, so a save
// never drops one unseen), comma-separated text without one.
func rolesInput(ctx context.Context, userID string, direct, known []string) render.HTML {
	label := i18nui.T(ctx, i18nui.KeyAdminColRoles)
	if len(known) == 0 {
		return ui.TextField(ui.TextFieldConfig{Name: "roles", Label: label, ID: "admin-roles-" + userID, Value: strings.Join(direct, ", ")})
	}
	all := slices.Clone(known)
	for _, r := range direct {
		if !slices.Contains(all, r) {
			all = append(all, r)
		}
	}
	slices.Sort(all)
	opts := make([]ui.MultiSelectOption, len(all))
	for i, r := range all {
		opts[i] = ui.MultiSelectOption{Value: r, Label: r, Selected: slices.Contains(direct, r)}
	}
	return ui.MultiSelect(ui.MultiSelectConfig{Name: "roles", Label: label, ID: "admin-roles-" + userID, Options: opts, Ctx: ctx})
}

// roleOriginLabels joins a user's direct roles with resolved ones as
// "role (origin)", sorted and deduplicated.
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
	slices.SortFunc(roles, func(a, b access.RoleWithOrigin) int {
		return cmp.Or(cmp.Compare(a.Role, b.Role), cmp.Compare(a.Origin, b.Origin))
	})
	roles = slices.Compact(roles)
	labels := make([]string, len(roles))
	for i, role := range roles {
		labels[i] = fmt.Sprintf("%s (%s)", role.Role, role.Origin)
	}
	return labels
}

// listUsersOpts reads ?limit= (1–500, default 50) and ?offset=.
func listUsersOpts(r *http.Request) auth.ListUsersOptions {
	opts := auth.ListUsersOptions{Limit: usersPageSize}
	if r == nil {
		return opts
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
		opts.Limit = n
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n >= 0 {
		opts.Offset = n
	}
	return opts
}

// ----- posts --------------------------------------------------------------------

// handleGrant grants a permission to a role through the GrantStore. A
// caller may grant only a permission its own roles hold; a refusal is
// audited too.
func (b *Battery) handleGrant(w http.ResponseWriter, r *http.Request) {
	b.grantOrRevoke(w, r, "grant", "granted", b.cfg.GrantStore.Grant)
}

// handleRevoke revokes a permission from a role, under the same rule.
func (b *Battery) handleRevoke(w http.ResponseWriter, r *http.Request) {
	b.grantOrRevoke(w, r, "revoke", "revoked", b.cfg.GrantStore.Revoke)
}

func (b *Battery) grantOrRevoke(w http.ResponseWriter, r *http.Request, op, ok string,
	call func(ctx context.Context, role string, perms ...access.Permission) error) {
	page := b.cfg.PathPrefix + "/rbac/roles"
	vals, read := b.readOps(w, r, page)
	if !read {
		return
	}
	role := strings.TrimSpace(vals.Get("role"))
	perm := access.Permission(strings.TrimSpace(vals.Get("permission")))
	if role == "" || perm == "" {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return
	}
	actor := adminActorID(r.Context())
	diff := map[string]any{"permission": string(perm)}
	if !b.callerHoldsPermission(r.Context(), perm) {
		b.appendAudit(r.Context(), "access", op+"-refused", role, actor, diff)
		b.refuse(w, r, page, http.StatusForbidden, "grant-refused")
		return
	}
	if err := call(r.Context(), role, perm); err != nil {
		if _, unknown := errors.AsType[*access.UnknownCapabilityError](err); unknown {
			b.refuse(w, r, page, http.StatusBadRequest, "unknown-capability")
			return
		}
		b.logger().Error("admin: "+op, "role", textsafe.ScrubControlBytes(role), "permission", textsafe.ScrubControlBytes(string(perm)), "error", textsafe.ScrubControlBytes(err.Error()))
		b.refuse(w, r, page, http.StatusInternalServerError, "failed")
		return
	}
	b.appendAudit(r.Context(), "access", op, role, actor, diff)
	b.done(w, r, page, ok)
}

// handleAssign replaces a user's direct roles. A caller may assign only
// roles its own tier covers (see nonAssignableRole); a refusal is
// audited with the role that stopped it.
func (b *Battery) handleAssign(w http.ResponseWriter, r *http.Request) {
	page := b.cfg.PathPrefix + "/rbac/users"
	vals, read := b.readOps(w, r, page)
	if !read {
		return
	}
	userID := strings.TrimSpace(vals.Get("user_id"))
	if userID == "" {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return
	}
	// One value per checkbox, or one comma-separated text field.
	var roles []string
	for _, v := range vals["roles"] {
		for role := range strings.SplitSeq(v, ",") {
			if role = strings.TrimSpace(role); role != "" && !slices.Contains(roles, role) {
				roles = append(roles, role)
			}
		}
	}
	actor := adminActorID(r.Context())
	if offending := b.nonAssignableRole(r.Context(), roles); offending != "" {
		b.appendAudit(r.Context(), "access", "assign-roles-refused", userID, actor,
			map[string]any{"roles": roles, "refused_role": offending})
		b.refuse(w, r, page, http.StatusForbidden, "assign-refused")
		return
	}
	if err := b.cfg.Auth.SetUserRoles(r.Context(), userID, roles); err != nil {
		b.logger().Error("admin: assign roles", "user", textsafe.ScrubControlBytes(userID), "error", textsafe.ScrubControlBytes(err.Error()))
		b.refuse(w, r, page, http.StatusInternalServerError, "failed")
		return
	}
	b.appendAudit(r.Context(), "access", "assign-roles", userID, actor, map[string]any{"roles": roles})
	b.done(w, r, page, "roles-saved")
}

// ----- tiers --------------------------------------------------------------------

// nonAssignableRole returns the first requested role the caller may not
// write onto an account, or "" when all are assignable. A role is
// assignable when the caller holds it, or when the caller's own
// permissions cover every permission the policy grants it (Wildcard
// covers everything). With no policy only held roles are assignable.
func (b *Battery) nonAssignableRole(ctx context.Context, requested []string) string {
	held := callerHeldRoles(ctx)
	callerPerms := map[access.Permission]bool{}
	if b.cfg.Policy != nil {
		for _, role := range held {
			for _, p := range b.cfg.Policy.PermissionsOf(role) {
				callerPerms[p] = true
			}
		}
	}
	for _, want := range requested {
		if slices.Contains(held, want) || callerPerms[access.Wildcard] {
			continue
		}
		if b.cfg.Policy == nil {
			return want
		}
		for _, p := range b.cfg.Policy.PermissionsOf(want) {
			if !callerPerms[p] {
				return want
			}
		}
	}
	return ""
}

// callerHoldsPermission reports whether the caller's own roles grant
// perm or Wildcard. With no policy nothing can be proven held, so it
// refuses.
func (b *Battery) callerHoldsPermission(ctx context.Context, perm access.Permission) bool {
	if b.cfg.Policy == nil {
		return false
	}
	for _, role := range callerHeldRoles(ctx) {
		for _, p := range b.cfg.Policy.PermissionsOf(role) {
			if p == access.Wildcard || p == perm {
				return true
			}
		}
	}
	return false
}

// callerHeldRoles is the signed-in user's own roles, read from the user
// (GetRoles), never from the context.
func callerHeldRoles(ctx context.Context) []string {
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return nil
	}
	rh, ok := u.(interface{ GetRoles() []string })
	if !ok {
		return nil
	}
	return rh.GetRoles()
}
