package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// fakeModules is a processModuleController that records the levers
// pulled and answers what the test scripted; no child process runs.
type fakeModules struct {
	list []framework.ProcessModuleInfo
	err  error
	// calls are "op:name" or "revoke:name:grant".
	calls []string
}

func (f *fakeModules) List() []framework.ProcessModuleInfo { return f.list }

func (f *fakeModules) Enable(_ context.Context, name string) error {
	f.calls = append(f.calls, "enable:"+name)
	return f.err
}

func (f *fakeModules) Disable(_ context.Context, name string) error {
	f.calls = append(f.calls, "disable:"+name)
	return f.err
}

func (f *fakeModules) BumpGeneration(_ context.Context, name string) (uint64, error) {
	f.calls = append(f.calls, "bump:"+name)
	return 2, f.err
}

func (f *fakeModules) RevokeGrants(_ context.Context, name string, grants []access.Permission) (uint64, error) {
	for _, g := range grants {
		f.calls = append(f.calls, "revoke:"+name+":"+string(g))
	}
	return 3, f.err
}

func moduleInfo(name string, state framework.ProcessState, edit ...func(*framework.ProcessModuleInfo)) framework.ProcessModuleInfo {
	m := framework.ProcessModuleInfo{Name: name, State: state, TrustTier: framework.TrustTrusted,
		DesiredGeneration: 1, ObservedGeneration: 1, RouteCount: 2, ToolCount: 1}
	for _, e := range edit {
		e(&m)
	}
	return m
}

// modulesEnv wires the fake behind a policy where admin holds Wildcard
// and "viewer" may enter the admin without modules:manage.
func modulesEnv(t *testing.T, f *fakeModules) *env {
	return setup(t, nil, Config{ProcessModules: f, Authorize: func(ctx context.Context) bool {
		return len(callerHeldRoles(ctx)) > 0
	}}, func(_ *env, c *Config) {
		p := access.NewRolePolicy()
		_ = p.Grant("admin", access.Wildcard)
		_ = p.Grant("viewer", "posts:read")
		c.Policy = p
	})
}

var viewer = roleUser{id: "viewer-1", roles: []string{"viewer"}}

func TestModulesPageShowsEveryState(t *testing.T) {
	f := &fakeModules{list: []framework.ProcessModuleInfo{
		moduleInfo("billing", framework.StateReady),
		moduleInfo("search", framework.StateInstalledDisabled),
		moduleInfo("mailer", framework.StateCrashed, func(m *framework.ProcessModuleInfo) {
			m.CircuitOpen, m.LeaseFailing, m.DesiredGeneration, m.LastExit = true, true, 3, "exit status 2"
		}),
	}}
	x := modulesEnv(t, f)
	body := get(x.as(theAdmin), "/admin/modules").Body.String()
	for _, want := range []string{"billing", "Serving", "search", "Serves 404", "mailer", "Serves 503",
		"Circuit open", "Lease failing", "3 / 1", "exit status 2",
		`action="/admin/modules/_disable"`, `action="/admin/modules/_enable"`, `action="/admin/modules/_bump"`, `action="/admin/modules/_revoke"`} {
		if !strings.Contains(body, want) {
			t.Errorf("modules page lacks %q", want)
		}
	}
	empty := modulesEnv(t, &fakeModules{})
	if body := get(empty.as(theAdmin), "/admin/modules").Body.String(); !strings.Contains(body, "No process modules") {
		t.Error("an empty supervisor draws no empty state")
	}
}

func TestModulesLeversNeedManage(t *testing.T) {
	f := &fakeModules{list: []framework.ProcessModuleInfo{moduleInfo("billing", framework.StateReady)}}
	x := modulesEnv(t, f)
	body := get(x.as(viewer), "/admin/modules").Body.String()
	if !strings.Contains(body, "billing") || strings.Contains(body, "/admin/modules/_") {
		t.Fatal("a caller without modules:manage sees the levers, or not the table")
	}
	for _, op := range []string{"enable", "disable", "bump", "revoke"} {
		rr := rpc(x.as(viewer), "/admin/modules/_"+op, map[string]any{"module": "billing", "grant": "x:y"})
		if rr.Code != http.StatusForbidden {
			t.Errorf("SECURITY: viewer %s = %d, want 403", op, rr.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("SECURITY: refused levers reached the supervisor: %v", f.calls)
	}
	want := []string{"module_enable_refused", "module_disable_refused", "module_bump_refused", "module_revoke_refused"}
	if ops := x.auditOps("module"); !slices.Equal(ops, want) {
		t.Fatalf("audit ops = %v", ops)
	}
}

// With no policy nothing proves modules:manage held.
func TestModulesLeversRefusedWithoutPolicy(t *testing.T) {
	f := &fakeModules{list: []framework.ProcessModuleInfo{moduleInfo("billing", framework.StateReady)}}
	x := setup(t, nil, Config{ProcessModules: f}, nil)
	if got := resultOf(t, post(x.as(theAdmin), "/admin/modules/_enable", url.Values{"module": {"billing"}})); got != "module-refused" {
		t.Fatalf("result = %q", got)
	}
	if len(f.calls) != 0 {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestModulesLeversCallAndAudit(t *testing.T) {
	f := &fakeModules{}
	x := modulesEnv(t, f)
	for _, c := range []struct{ op, result string }{
		{"enable", "module-enabled"}, {"disable", "module-disabled"}, {"bump", "module-bumped"}, {"revoke", "module-revoked"},
	} {
		vals := url.Values{"module": {"billing"}, "grant": {"billing:refund"}}
		if got := resultOf(t, post(x.as(theAdmin), "/admin/modules/_"+c.op, vals)); got != c.result {
			t.Errorf("%s result = %q, want %s", c.op, got, c.result)
		}
	}
	if want := []string{"enable:billing", "disable:billing", "bump:billing", "revoke:billing:billing:refund"}; !slices.Equal(f.calls, want) {
		t.Fatalf("calls = %v", f.calls)
	}
	if want := []string{"module_enable", "module_disable", "module_bump", "module_revoke"}; !slices.Equal(x.auditOps("module"), want) {
		t.Fatalf("audit ops = %v", x.auditOps("module"))
	}
}

func TestModulesLeverRefusesBadInput(t *testing.T) {
	f := &fakeModules{}
	x := modulesEnv(t, f)
	if got := resultOf(t, post(x.as(theAdmin), "/admin/modules/_enable", url.Values{"module": {"  "}})); got != "bad-input" {
		t.Errorf("no module result = %q", got)
	}
	if got := resultOf(t, post(x.as(theAdmin), "/admin/modules/_revoke", url.Values{"module": {"billing"}})); got != "bad-input" {
		t.Errorf("no grant result = %q", got)
	}
	if len(f.calls) != 0 || len(x.auditOps("module")) != 0 {
		t.Fatalf("bad input reached the supervisor (%v) or the audit log", f.calls)
	}
}

// A supervisor error is logged, and the caller reads a fixed message,
// never the error text.
func TestModulesLeverErrorHidesItsText(t *testing.T) {
	f := &fakeModules{err: errors.New("boom: /var/run/secret.sock")}
	x := modulesEnv(t, f)
	rr := rpc(x.as(theAdmin), "/admin/modules/_enable", map[string]any{"module": "billing"})
	if rr.Code != http.StatusConflict || strings.Contains(rr.Body.String(), "secret") {
		t.Fatalf("failed lever = %d %s", rr.Code, rr.Body.String())
	}
	loc := post(x.as(theAdmin), "/admin/modules/_enable", url.Values{"module": {"billing"}}).Header().Get("Location")
	if loc != "/admin/modules?result=module-failed" {
		t.Fatalf("Location = %q", loc)
	}
	if body := get(x.as(theAdmin), loc).Body.String(); strings.Contains(body, "boom") || !strings.Contains(body, "fui-callout") {
		t.Fatal("the page after a failed lever shows the error text or no notice")
	}
	if len(x.auditOps("module")) != 0 {
		t.Fatal("a failed lever was audited as done")
	}
}

// ?result= reads only the fixed names: request text never prints.
func TestResultNoticeIgnoresUnknownNames(t *testing.T) {
	x := modulesEnv(t, &fakeModules{})
	body := get(x.as(theAdmin), "/admin/modules?result="+url.QueryEscape("<b>pwned</b>")).Body.String()
	if strings.Contains(body, "pwned") {
		t.Fatal("SECURITY: ?result= printed request text")
	}
}

func TestModulesUnwiredHasNoPage(t *testing.T) {
	x := setup(t, nil, Config{}, nil)
	if rr := get(x.as(theAdmin), "/admin/modules"); rr.Code != http.StatusNotFound {
		t.Errorf("unwired /admin/modules = %d, want 404", rr.Code)
	}
}
