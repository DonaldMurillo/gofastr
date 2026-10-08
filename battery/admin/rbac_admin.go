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

// renderRoles draws the Roles page: a grid with a row per permission
// and a column per role, a checkbox where the role holds it. A role
// holding Wildcard shows every box checked and locked. With a
// GrantStore the grid is one form whose Save grants and revokes the
// boxes that changed, and a form below adds a role; without one the
// boxes are read-only.
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

	cols := []ui.Column{{Key: "permission", Header: i18nui.T(ctx, i18nui.KeyAdminPermission)}}
	for i, role := range roles {
		cols = append(cols, ui.Column{Key: "role-" + strconv.Itoa(i), Header: role, Align: "center", Fit: true})
	}
	// Only the roles a save may change are posted; their index in the
	// posted list is what a checked box names.
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
			name = append(name, ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.T(ctx, i18nui.KeyAdminUndeclared), Variant: ui.StatusDanger}))
		}
		cells := map[string]render.HTML{"permission": ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, Align: ui.AlignCenter}, name...)}
		for i, role := range roles {
			cell := ui.ToggleConfig{
				Name:        "grant",
				ID:          "admin-grant-" + strconv.Itoa(i) + "-" + strconv.Itoa(j),
				LabelHidden: true,
				Label:       i18nui.TVars(ctx, i18nui.KeyAdminGrantCell, map[string]string{"permission": string(perm), "role": role}),
				Checked:     slices.Contains(p.PermissionsOf(role), perm),
				Disabled:    col[i] < 0,
			}
			if holdsWildcard(p, role) {
				cell.Checked = true
				cell.Label = i18nui.TVars(ctx, i18nui.KeyAdminHoldsEvery, map[string]string{"role": role})
			}
			if col[i] >= 0 {
				cell.Value = strconv.Itoa(col[i]) + ":" + strconv.Itoa(j)
			}
			cells["role-"+strconv.Itoa(i)] = ui.Checkbox(cell)
		}
		rows[j] = ui.Row{ID: string(perm), Cells: cells}
	}
	empty := ui.EmptyStateConfig{
		Title:        i18nui.T(ctx, i18nui.KeyAdminNoRoles),
		Description:  i18nui.T(ctx, i18nui.KeyAdminNoRolesDesc),
		HeadingLevel: 2,
	}
	if len(roles) > 0 {
		empty.Title = i18nui.T(ctx, i18nui.KeyAdminNoPermissions)
		empty.Description = i18nui.T(ctx, i18nui.KeyAdminNoPermissionsDesc)
	}
	grid := ui.DataTable(ui.DataTableConfig{
		Columns:       cols,
		Rows:          rows,
		Caption:       i18nui.T(ctx, i18nui.KeyAdminRoles),
		CaptionHidden: true,
		// On a phone each permission is a card of role boxes, so no
		// role hides past the scroll edge.
		Responsive: ui.ResponsiveCards,
		Ctx:        ctx,
		Empty:      empty,
	})
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.T(ctx, i18nui.KeyAdminRoles),
			Subtitle: i18nui.T(ctx, i18nui.KeyAdminRolesSub),
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
			label: i18nui.T(ctx, i18nui.KeyAdminSavePermissions), variant: ui.ButtonPrimary,
			body: append(shown, grid),
			wide: true,
		}))
	}
	if editable {
		parts = append(parts, ui.Section(ui.SectionConfig{Heading: i18nui.T(ctx, i18nui.KeyAdminAddRole)},
			b.opForm(ctx, opSpec{
				path: b.cfg.PathPrefix + "/rbac/_grant", page: page,
				label: i18nui.T(ctx, i18nui.KeyAdminGrant), variant: ui.ButtonSecondary,
				body: []render.HTML{
					ui.TextField(ui.TextFieldConfig{Name: "role", Label: i18nui.T(ctx, i18nui.KeyAdminNewRole), Required: true}),
					permissionInput(ctx, caps, ""),
				},
			})))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, parts...)
}

// permissionRows is the grid's rows: every declared capability and
// every permission a role holds, Wildcard aside, sorted.
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

// holdsWildcard reports whether role holds every permission.
func holdsWildcard(p *access.RolePolicy, role string) bool {
	return slices.Contains(p.PermissionsOf(role), access.Wildcard)
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
	opts, pageNo := listUsersOpts(r)
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.T(ctx, i18nui.KeyAdminUserRoles),
		Subtitle: i18nui.T(ctx, i18nui.KeyAdminUserRolesSub),
	})
	users, total, err := b.cfg.Auth.ListUsers(ctx, opts)
	pages := pageCount(total, opts.Limit)
	if err == nil && pageNo > pages {
		// A page past the end shows the last one.
		pageNo = pages
		opts.Offset = (pageNo - 1) * opts.Limit
		users, total, err = b.cfg.Auth.ListUsers(ctx, opts)
	}
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
			labels = b.effectiveRoleLabels(ctx, u.GetID(), direct)
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
	var pager *ui.PaginationConfig
	if pages > 1 {
		carry := url.Values{}
		if r != nil {
			carry = r.URL.Query()
			carry.Del(usersPageParam)
		}
		pager = &ui.PaginationConfig{
			Page:      pageNo,
			Pages:     pages,
			Path:      b.cfg.PathPrefix + "/rbac/users",
			Query:     carry,
			PageParam: usersPageParam,
			Ctx:       ctx,
		}
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, header, resultNotice(ctx),
		ui.DataTable(ui.DataTableConfig{
			Columns:       cols,
			Rows:          rows,
			Caption:       i18nui.T(ctx, i18nui.KeyAdminUserRoles),
			CaptionHidden: true,
			Responsive:    ui.ResponsiveScroll,
			Pagination:    pager,
			Ctx:           ctx,
			Empty:         ui.EmptyStateConfig{Title: i18nui.T(ctx, i18nui.KeyAdminNoUsers), HeadingLevel: 2},
		}),
	)
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

// effectiveRoleLabels labels a user's roles with their origins through
// EffectiveRoles. A resolver that panics leaves the direct roles as they
// are; the log names the callback and the panic's type.
func (b *Battery) effectiveRoleLabels(ctx context.Context, userID string, direct []string) (labels []string) {
	defer func() {
		if rec := recover(); rec != nil {
			b.logger().Error("admin: policy callback panicked", "callback", "EffectiveRoles", "panic", fmt.Sprintf("%T", rec))
			labels = direct
		}
	}()
	return roleOriginLabels(direct, b.cfg.EffectiveRoles(ctx, userID))
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

// usersPageParam is the User roles page's page number in its query.
const usersPageParam = "p"

// maxUsersPage bounds ?p= before it scales by the page size, so a huge
// value cannot overflow the offset; past the end shows the last page.
const maxUsersPage = 1 << 20

// listUsersOpts reads ?limit= (1–500, default 50) and ?p=, the 1-based
// page, into the store's window and the page number.
func listUsersOpts(r *http.Request) (auth.ListUsersOptions, int) {
	opts := auth.ListUsersOptions{Limit: usersPageSize}
	page := 1
	if r == nil {
		return opts, page
	}
	q := r.URL.Query()
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 500 {
		opts.Limit = n
	}
	if n, err := strconv.Atoi(q.Get(usersPageParam)); err == nil && n > 1 {
		page = min(n, maxUsersPage)
	}
	opts.Offset = (page - 1) * opts.Limit
	return opts, page
}

// ----- posts --------------------------------------------------------------------

// handleGrant grants a permission to a role through the GrantStore. A
// caller may grant only a permission its own roles hold; a refusal is
// audited too.
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
	if role == "" || perm == "" {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return
	}
	if b.applyGrants(w, r, page, []grantChange{{role: role, perm: perm, grant: op == "grant"}}) {
		b.done(w, r, page, ok)
	}
}

// maxGridCells bounds the roles × permissions a grid save may name.
const maxGridCells = 10_000

// handlePermissions saves the Roles grid. role and permission list the
// columns and rows the form showed; each grant value "i:j" is a checked
// box, role i holding permission j. A shown box that differs from the
// policy is granted or revoked; anything the form did not show, and any
// role holding Wildcard, is left alone.
func (b *Battery) handlePermissions(w http.ResponseWriter, r *http.Request) {
	page := b.cfg.PathPrefix + "/rbac/roles"
	vals, read := b.readOps(w, r, page)
	if !read {
		return
	}
	roles, rolesOK := distinctTrimmed(vals["role"])
	perms, permsOK := distinctTrimmed(vals["permission"])
	if !rolesOK || !permsOK || len(roles) == 0 || len(perms) == 0 || len(roles) > maxGridCells/len(perms) {
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

// grantChange is one box a save flips: grant (or revoke) perm on role.
type grantChange struct {
	role  string
	perm  access.Permission
	grant bool
}

// applyGrants checks every change against the caller's own permissions
// before writing any, then grants and revokes through the GrantStore,
// auditing each. A refusal is audited with the permission that stopped
// it and answered; it reports whether the caller should answer success.
func (b *Battery) applyGrants(w http.ResponseWriter, r *http.Request, page string, changes []grantChange) bool {
	ctx := r.Context()
	actor := adminActorID(ctx)
	for _, c := range changes {
		if !b.callerHoldsPermission(ctx, c.perm) {
			b.appendAudit(ctx, "access", c.op()+"-refused", c.role, actor, map[string]any{"permission": string(c.perm)})
			b.refuse(w, r, page, http.StatusForbidden, "grant-refused")
			return false
		}
	}
	for _, c := range changes {
		call := b.cfg.GrantStore.Revoke
		if c.grant {
			call = b.cfg.GrantStore.Grant
		}
		if err := call(ctx, c.role, c.perm); err != nil {
			if _, unknown := errors.AsType[*access.UnknownCapabilityError](err); unknown {
				b.refuse(w, r, page, http.StatusBadRequest, "unknown-capability")
				return false
			}
			b.logger().Error("admin: "+c.op(), "role", textsafe.ScrubControlBytes(c.role), "permission", textsafe.ScrubControlBytes(string(c.perm)), "error", textsafe.ScrubControlBytes(err.Error()))
			b.refuse(w, r, page, http.StatusInternalServerError, "failed")
			return false
		}
		b.appendAudit(ctx, "access", c.op(), c.role, actor, map[string]any{"permission": string(c.perm)})
	}
	return true
}

func (c grantChange) op() string {
	if c.grant {
		return "grant"
	}
	return "revoke"
}

// distinctTrimmed trims each value and refuses an empty or repeated one:
// a grid index must name one column or row.
func distinctTrimmed(vals []string) ([]string, bool) {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || slices.Contains(out, v) {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

// gridCell parses a grant value "i:j" against the grid's size.
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
