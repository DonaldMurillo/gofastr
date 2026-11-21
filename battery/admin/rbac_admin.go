package admin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

// for the caller's own account takes access.UserID.
func adminActorID(ctx context.Context) string {
	return cmp.Or(access.UserID(ctx), "unknown")
}

func (b *Battery) renderRoles(ctx context.Context, _ map[string]string) render.HTML {
	p := b.cfg.Policy
	caps := p.Capabilities()
	roles := p.Roles()
	var perms []access.Permission
	if len(roles) > 0 {
		perms = permissionRows(p, roles, caps)
	}
	editable := b.cfg.GrantStore != nil
	page := b.cfg.PathPrefix + "/rbac/roles"

	cols := []ui.Column{{Key: "permission", Header: i18nui.AdminPermission.Text(ctx)}}
	for i, role := range roles {
		cols = append(cols, ui.Column{Key: "role-" + strconv.Itoa(i), Header: role, Align: "center", Fit: true})
	}
	var posted []string
	col := make([]int, len(roles))
	for i, role := range roles {
		col[i] = -1
		if editable && !holdsWildcard(p, role) {
			col[i] = len(posted)
			posted = append(posted, role)
		}
	}
	rows := make([]ui.Row, len(perms))
	for j, perm := range perms {
		name := []render.HTML{ui.InlineCode(string(perm))}
		if len(caps) > 0 && !slices.Contains(caps, perm) {
			name = append(name, ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.AdminUndeclared.Text(ctx), Variant: ui.StatusDanger}))
		}
		cells := map[string]render.HTML{"permission": ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, Align: ui.AlignCenter}, name...)}
		for i, role := range roles {
			cell := ui.ToggleConfig{Ctx: ctx,
				Name:        "grant",
				ID:          "admin-grant-" + strconv.Itoa(i) + "-" + strconv.Itoa(j),
				LabelHidden: true,
				Label:       i18nui.AdminGrantCell.Text(ctx, map[string]any{"permission": string(perm), "role": role}),
				Checked:     slices.Contains(p.PermissionsOf(role), perm),
				Disabled:    col[i] < 0,
			}
			if holdsWildcard(p, role) {
				cell.Checked = true
				cell.Label = i18nui.AdminHoldsEvery.Text(ctx, map[string]any{"role": role})
			}
			if col[i] >= 0 {
				cell.Value = strconv.Itoa(col[i]) + ":" + strconv.Itoa(j)
			}
			cells["role-"+strconv.Itoa(i)] = ui.Checkbox(cell)
		}
		rows[j] = ui.Row{ID: string(perm), Cells: cells}
	}
	empty := ui.EmptyStateConfig{
		Title:        i18nui.AdminNoRoles.Text(ctx),
		Description:  i18nui.AdminNoRolesDesc.Text(ctx),
		HeadingLevel: 2,
	}
	if len(roles) > 0 {
		empty.Title = i18nui.AdminNoPermissions.Text(ctx)
		empty.Description = i18nui.AdminNoPermissionsDesc.Text(ctx)
	}
	grid := ui.DataTable(ui.DataTableConfig{
		Columns:       cols,
		Rows:          rows,
		Caption:       i18nui.AdminRoles.Text(ctx),
		CaptionHidden: true,
		Responsive:    ui.ResponsiveCards,
		Ctx:           ctx,
		Empty:         empty,
	})
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.AdminRoles.Text(ctx),
			Subtitle: i18nui.AdminRolesSub.Text(ctx),
		}),
		resultNotice(ctx),
	}
	if len(posted) == 0 || len(rows) == 0 {
		parts = append(parts, grid)
	} else {
		shown := make([]render.HTML, 0, len(posted)+len(perms)+1)
		for _, role := range posted {
			shown = append(shown, html.Input(html.InputConfig{Type: "hidden", Name: "role", Value: role}))
		}
		for _, perm := range perms {
			shown = append(shown, html.Input(html.InputConfig{Type: "hidden", Name: "permission", Value: string(perm)}))
		}
		parts = append(parts, b.opForm(ctx, opSpec{
			path: b.cfg.PathPrefix + "/rbac/_permissions", page: page,
			label: i18nui.AdminSavePermissions.Text(ctx), variant: ui.ButtonPrimary,
			body: append(shown, grid),
			wide: true,
		}))
	}
	if editable {
		parts = append(parts, ui.Section(ui.SectionConfig{Ctx: ctx, Heading: i18nui.AdminAddRole.Text(ctx)},
			b.opForm(ctx, opSpec{
				path: b.cfg.PathPrefix + "/rbac/_grant", page: page,
				label: i18nui.AdminGrant.Text(ctx), variant: ui.ButtonSecondary,
				body: []render.HTML{
					ui.TextField(ui.TextFieldConfig{Ctx: ctx, Name: "role", Label: i18nui.AdminNewRole.Text(ctx), Required: true}),
					permissionInput(ctx, caps, ""),
				},
			})))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, parts...)
}

func permissionRows(p *access.RolePolicy, roles []string, caps []access.Permission) []access.Permission {
	perms := slices.Clone(caps)
	for _, role := range roles {
		for _, perm := range p.PermissionsOf(role) {
			if perm != access.Wildcard && !slices.Contains(perms, perm) {
				perms = append(perms, perm)
			}
		}
	}
	slices.Sort(perms)
	return perms
}

func holdsWildcard(p *access.RolePolicy, role string) bool {
	return slices.Contains(p.PermissionsOf(role), access.Wildcard)
}

func permissionInput(ctx context.Context, caps []access.Permission, suffix string) render.HTML {
	label := i18nui.AdminPermission.Text(ctx)
	id := "admin-perm"
	if suffix != "" {
		id += "-" + suffix
	}
	if len(caps) == 0 {
		return ui.TextField(ui.TextFieldConfig{Ctx: ctx, Name: "permission", Label: label, ID: id, Required: true})
	}
	opts := make([]ui.SelectOption, len(caps))
	for i, c := range caps {
		opts[i] = ui.SelectOption{Value: string(c), Text: string(c)}
	}
	return ui.Select(ui.SelectConfig{Ctx: ctx, Name: "permission", Label: label, ID: id, Options: opts, Required: true})
}

const usersPageSize = 50

func (b *Battery) renderUsers(ctx context.Context, _ map[string]string) render.HTML {
	var query url.Values
	if r := appui.RequestFromContext(ctx); r != nil {
		query = r.URL.Query()
	}
	opts, pageNo := listUsersOpts(query)
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.AdminUserRoles.Text(ctx),
		Subtitle: i18nui.AdminUserRolesSub.Text(ctx),
	})
	users, total, err := b.cfg.Auth.ListUsers(ctx, opts)
	pages := pageCount(total, opts.Limit)
	if err == nil && pageNo > pages {
		pageNo = pages
		opts.Offset = (pageNo - 1) * opts.Limit
		users, total, err = b.cfg.Auth.ListUsers(ctx, opts)
	}
	if err != nil {
		return b.loadFailed(ctx, "admin: list users", i18nui.AdminUsersLoadFailed, err, []render.HTML{header})
	}
	page := b.cfg.PathPrefix + "/rbac/users"
	if encoded := query.Encode(); encoded != "" {
		page += "?" + encoded
	}
	var known []string
	if b.cfg.Policy != nil {
		known = b.cfg.Policy.Roles()
	}
	cols := []ui.Column{
		{Key: "user", Header: i18nui.AdminColUser.Text(ctx)},
		{Key: "roles", Header: i18nui.AdminColRoles.Text(ctx)},
		{Key: "set", Header: i18nui.AdminColActions.Text(ctx), Align: "end"},
	}
	rows := make([]ui.Row, len(users))
	for i, u := range users {
		direct := u.GetRoles()
		labels := direct
		if b.cfg.EffectiveRoles != nil {
			labels = b.effectiveRoleLabels(ctx, u.GetID(), direct)
		}
		held := make([]render.HTML, 0, len(labels))
		for _, l := range labels {
			held = append(held, ui.Tag(ui.TagConfig{Ctx: ctx, Label: l}))
		}
		heldCell := ui.Muted(render.Text("—"))
		if len(held) > 0 {
			heldCell = ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS}, held...)
		}
		rows[i] = ui.Row{ID: u.GetID(), Cells: map[string]render.HTML{
			"user":  render.Text(u.GetEmail()),
			"roles": heldCell,
			"set": ui.Dropdown(ui.DropdownConfig{
				Ctx:   ctx,
				ID:    "admin-roles-edit-" + u.GetID(),
				Label: i18nui.AdminEditRoles.Text(ctx),
				Align: ui.DropdownEnd,
				Content: b.opForm(ctx, opSpec{
					path: b.cfg.PathPrefix + "/rbac/_assign", page: page,
					label: i18nui.AdminSaveRoles.Text(ctx), variant: ui.ButtonPrimary,
					fields:  map[string]string{"user_id": u.GetID()},
					body:    []render.HTML{rolesInput(ctx, u.GetID(), direct, known)},
					stacked: true,
				}),
			}),
		}}
	}
	query.Del(pageParam)
	pager := pagePager(ctx, b.cfg.PathPrefix+"/rbac/users", query, pageNo, pages)
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, header, resultNotice(ctx),
		ui.DataTable(ui.DataTableConfig{
			Columns:       cols,
			Rows:          rows,
			Caption:       i18nui.AdminUserRoles.Text(ctx),
			CaptionHidden: true,
			Responsive:    ui.ResponsiveScroll,
			Pagination:    pager,
			Ctx:           ctx,
			Empty:         ui.EmptyStateConfig{Title: i18nui.AdminNoUsers.Text(ctx), HeadingLevel: 2},
		}),
	)
}

const selectedRolesField = "selected_roles"

// rolesInput draws literal role checkboxes when roles are known,
// including undeclared roles the user holds, or a comma-separated text field.
func rolesInput(ctx context.Context, userID string, direct, known []string) render.HTML {
	label := i18nui.AdminColRoles.Text(ctx)
	if len(known) == 0 {
		return ui.TextField(ui.TextFieldConfig{Ctx: ctx, Name: "roles", Label: label, ID: "admin-roles-" + userID, Value: strings.Join(direct, ", ")})
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
	return ui.MultiSelect(ui.MultiSelectConfig{Name: selectedRolesField, Label: label, ID: "admin-roles-" + userID, Options: opts, Ctx: ctx})
}

func (b *Battery) effectiveRoleLabels(ctx context.Context, userID string, direct []string) (labels []string) {
	defer func() {
		if rec := recover(); rec != nil {
			b.logger().Error("admin: policy callback panicked", "callback", "EffectiveRoles", "panic", fmt.Sprintf("%T", rec))
			labels = direct
		}
	}()
	return roleOriginLabels(direct, b.cfg.EffectiveRoles(ctx, userID))
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

const maxUsersPage = 1 << 20

func listUsersOpts(q url.Values) (auth.ListUsersOptions, int) {
	page, limit := decodePage(q, usersPageSize, 500)
	page = min(page, maxUsersPage)
	return auth.ListUsersOptions{Limit: limit, Offset: (page - 1) * limit}, page
}

func (b *Battery) handleGrant(w http.ResponseWriter, r *http.Request) {
	b.grantOrRevoke(w, r, "grant", "granted", b.cfg.GrantStore.Grant)
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
	if !textsafe.Storable(role) {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input", "role")
		return
	}
	if !textsafe.Storable(string(perm)) {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input", "permission")
		return
	}
	if role == "" || perm == "" {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return
	}
	if b.applyGrants(w, r, page, []grantChange{{role: role, perm: perm, grant: op == "grant"}}) {
		b.done(w, r, page, ok)
	}
}

const maxGridCells = 10_000

func (b *Battery) handlePermissions(w http.ResponseWriter, r *http.Request) {
	page := b.cfg.PathPrefix + "/rbac/roles"
	vals, read := b.readOps(w, r, page)
	if !read {
		return
	}
	roles, rolesOK := distinctTrimmed(vals["role"])
	perms, permsOK := distinctTrimmed(vals["permission"])
	if !rolesOK || len(roles) == 0 {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input", "role")
		return
	}
	if !permsOK || len(perms) == 0 {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input", "permission")
		return
	}
	if len(roles) > maxGridCells/len(perms) {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return
	}
	want := map[[2]int]bool{}
	for _, v := range vals["grant"] {
		i, j, ok := gridCell(v, len(roles), len(perms))
		if !ok {
			b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
			return
		}
		want[[2]int{i, j}] = true
	}
	var changes []grantChange
	for i, role := range roles {
		held := b.cfg.Policy.PermissionsOf(role)
		if slices.Contains(held, access.Wildcard) {
			continue
		}
		for j, perm := range perms {
			p := access.Permission(perm)
			if want[[2]int{i, j}] != slices.Contains(held, p) {
				changes = append(changes, grantChange{role: role, perm: p, grant: want[[2]int{i, j}]})
			}
		}
	}
	if b.applyGrants(w, r, page, changes) {
		b.done(w, r, page, "permissions-saved")
	}
}

type grantChange struct {
	role  string
	perm  access.Permission
	grant bool
}

// applyGrants checks every change against the caller's own permissions
// (a Wildcard change needs the Decider's allow, as on assignment), and
// every grant against GrantStore.CheckGrant, before writing any, then
func (b *Battery) applyGrants(w http.ResponseWriter, r *http.Request, page string, changes []grantChange) bool {
	ctx := r.Context()
	actor := adminActorID(ctx)
	for _, c := range changes {
		if assignmentDeciderRefuses(ctx, c.perm) || !b.callerHoldsPermission(ctx, c.perm) {
			b.appendAudit(ctx, "access", c.op()+"-refused", c.role, actor, map[string]any{"permission": string(c.perm)})
			b.refuse(w, r, page, http.StatusForbidden, "grant-refused")
			return false
		}
	}
	if c, lost := b.ownPermissionLost(ctx, changes); lost {
		b.appendAudit(ctx, "access", c.op()+"-refused", c.role, actor, map[string]any{"permission": string(c.perm), "own_permission": true})
		b.refuse(w, r, page, http.StatusForbidden, "own-permission-refused")
		return false
	}
	for _, c := range changes {
		if c.grant && !b.grantAccepted(w, r, page, c, b.cfg.GrantStore.CheckGrant(ctx, c.role, c.perm)) {
			return false
		}
	}
	for _, c := range changes {
		call := b.cfg.GrantStore.Revoke
		if c.grant {
			call = b.cfg.GrantStore.Grant
		}
		if !b.grantAccepted(w, r, page, c, call(ctx, c.role, c.perm)) {
			return false
		}
		b.appendAudit(ctx, "access", c.op(), c.role, actor, map[string]any{"permission": string(c.perm)})
	}
	return true
}

func (b *Battery) grantAccepted(w http.ResponseWriter, r *http.Request, page string, c grantChange, err error) bool {
	if err == nil {
		return true
	}
	if _, unknown := errors.AsType[*access.UnknownCapabilityError](err); unknown {
		b.refuse(w, r, page, http.StatusBadRequest, "unknown-capability")
		return false
	}
	if errors.Is(err, access.ErrRevokedByWildcard) {
		b.refuse(w, r, page, http.StatusConflict, "revoked-by-wildcard")
		return false
	}
	b.logger().Error("admin: "+c.op(), "role", textsafe.ScrubControlBytes(c.role), "permission", textsafe.ScrubControlBytes(string(c.perm)), "error", textsafe.ScrubControlBytes(err.Error()))
	b.refuse(w, r, page, http.StatusInternalServerError, "failed")
	return false
}

func (c grantChange) op() string {
	if c.grant {
		return "grant"
	}
	return "revoke"
}

func distinctTrimmed(vals []string) ([]string, bool) {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || !textsafe.Storable(v) || slices.Contains(out, v) {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

func gridCell(v string, roles, perms int) (int, int, bool) {
	a, c, ok := strings.Cut(v, ":")
	if !ok {
		return 0, 0, false
	}
	i, err1 := strconv.Atoi(a)
	j, err2 := strconv.Atoi(c)
	if err1 != nil || err2 != nil || i < 0 || j < 0 || i >= roles || j >= perms {
		return 0, 0, false
	}
	return i, j, true
}

func (b *Battery) handleAssign(w http.ResponseWriter, r *http.Request) {
	page := b.cfg.PathPrefix + "/rbac/users"
	vals, read := b.readOps(w, r, page)
	if !read {
		return
	}
	userID := vals.Get("user_id")
	if strings.TrimSpace(userID) == "" || !textsafe.Storable(userID) {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input", "user_id")
		return
	}
	roles, badField := parseRoleAssignment(vals)
	if badField != "" {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input", badField)
		return
	}
	actor := adminActorID(r.Context())
	target, err := b.cfg.Auth.UserStore().FindByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
			return
		}
		b.logger().Error("admin: assign roles", "user", textsafe.ScrubControlBytes(userID), "error", textsafe.ScrubControlBytes(err.Error()))
		b.refuse(w, r, page, http.StatusInternalServerError, "failed")
		return
	}
	current := slices.Clone(target.GetRoles())
	var changed []string
	for _, role := range roles {
		if !slices.Contains(current, role) {
			changed = append(changed, role)
		}
	}
	for _, role := range current {
		if !slices.Contains(roles, role) && !slices.Contains(changed, role) {
			changed = append(changed, role)
		}
	}
	if b.cfg.Authorize == nil && userID == access.UserID(r.Context()) &&
		slices.Contains(current, b.adminRole()) && !slices.Contains(roles, b.adminRole()) {
		b.appendAudit(r.Context(), "access", "assign-roles-refused", userID, actor,
			map[string]any{"roles": roles, "refused_role": b.adminRole()})
		b.refuse(w, r, page, http.StatusForbidden, "own-admin-refused")
		return
	}
	if offending := b.nonAssignableRole(r.Context(), changed); offending != "" {
		b.appendAudit(r.Context(), "access", "assign-roles-refused", userID, actor,
			map[string]any{"roles": roles, "refused_role": offending})
		b.refuse(w, r, page, http.StatusForbidden, "assign-refused")
		return
	}
	swapped, err := b.cfg.Auth.SwapUserRoles(r.Context(), userID, current, roles)
	if err != nil {
		b.logger().Error("admin: assign roles", "user", textsafe.ScrubControlBytes(userID), "error", textsafe.ScrubControlBytes(err.Error()))
		b.refuse(w, r, page, http.StatusInternalServerError, "failed")
		return
	}
	if !swapped {
		b.refuse(w, r, page, http.StatusConflict, "roles-conflict")
		return
	}
	b.appendAudit(r.Context(), "access", "assign-roles", userID, actor, map[string]any{"roles": roles})
	b.done(w, r, page, "roles-saved")
}

func parseRoleAssignment(vals url.Values) ([]string, string) {
	var roles []string
	for _, role := range vals[selectedRolesField] {
		if !textsafe.Storable(role) {
			return nil, selectedRolesField
		}
		if role != "" && !slices.Contains(roles, role) {
			roles = append(roles, role)
		}
	}
	for _, v := range vals["roles"] {
		if !textsafe.Storable(v) {
			return nil, "roles"
		}
		for role := range strings.SplitSeq(v, ",") {
			if role = strings.TrimSpace(role); role != "" && !slices.Contains(roles, role) {
				roles = append(roles, role)
			}
		}
	}
	return roles, ""
}

// nonAssignableRole returns the first role whose change the caller's tier
// or the request's Decider refuses.
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
		byName := slices.Contains(held, want) || callerPerms[access.Wildcard]
		var perms []access.Permission
		if b.cfg.Policy != nil {
			perms = b.cfg.Policy.PermissionsOf(want)
		}
		if len(perms) == 0 && !byName {
			return want
		}
		for _, p := range perms {
			if assignmentDeciderRefuses(ctx, p) || (!byName && !callerPerms[p]) {
				return want
			}
		}
	}
	return ""
}

func assignmentDeciderRefuses(ctx context.Context, perm access.Permission) bool {
	if perm != access.Wildcard {
		return permissionDenied(ctx, perm)
	}
	d := access.GetDecider(ctx)
	return d != nil && d(ctx, access.GetRoles(ctx), perm, access.Ref{}) != access.DecisionAllow
}

func (b *Battery) callerHoldsPermission(ctx context.Context, perm access.Permission) bool {
	if permissionDenied(ctx, perm) || b.cfg.Policy == nil {
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

func permissionDenied(ctx context.Context, perm access.Permission) bool {
	d := access.GetDecider(ctx)
	return d != nil && d(ctx, access.GetRoles(ctx), perm, access.Ref{}) == access.DecisionDeny
}

func (b *Battery) ownPermissionLost(ctx context.Context, changes []grantChange) (grantChange, bool) {
	if b.cfg.Policy == nil {
		return grantChange{}, false
	}
	held := callerHeldRoles(ctx)
	after := map[access.Permission]bool{}
	for _, role := range held {
		perms := map[access.Permission]bool{}
		for _, p := range b.cfg.Policy.PermissionsOf(role) {
			perms[p] = true
		}
		for _, c := range changes {
			if c.role == role {
				perms[c.perm] = c.grant
			}
		}
		for p, ok := range perms {
			if ok {
				after[p] = true
			}
		}
	}
	if after[access.Wildcard] {
		return grantChange{}, false
	}
	for _, c := range changes {
		if !c.grant && slices.Contains(held, c.role) && !after[c.perm] {
			return c, true
		}
	}
	return grantChange{}, false
}

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
