package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func TestSweepAppUnknownVerbIsNamedUnrecognised(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile(`	r.Handle("FETCH", "/users", nil)`),
	})
	d := assertHas(t, ds, contracts.RuleNonUppercaseVerb)
	if !strings.Contains(d.Message, "unrecognised HTTP method") || d.Fix != nil {
		t.Fatalf("an unknown verb is not a case mistake: %q fix=%v", d.Message, d.Fix)
	}
}

func TestSweepAppColonInsideSegmentIsNotAParam(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile(`	r.Handle("GET", "/files/a:b", nil)`),
	})
	assertNot(t, ds, contracts.RuleColonPathParam, "a colon inside a segment is literal text, not :param")
}

func TestSweepAppRootRouteNeedsAnExactMention(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":      routerFile("\tr.Handle(\"GET\", \"/\", nil)\n\tr.Handle(\"GET\", \"/api/x\", nil)"),
		"main_test.go": "package main\n\nvar paths = []string{\"/api/x\"}\n",
	})
	found := countRule(t, ds, contracts.RuleUntestedRoute)
	if len(found) != 1 || !strings.Contains(found[0].Message, "mentions /") {
		t.Fatalf("only the root route is untested, got %v", found)
	}
}

func TestSweepAppAmbiguousLiteralGetsNoFix(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile(`	r.Handle("get", "/a", nil); r.Handle("get", "/b", nil)`),
	})
	for _, d := range countRule(t, ds, contracts.RuleNonUppercaseVerb) {
		if d.Fix != nil {
			t.Fatalf("two \"get\" literals on one line must not get a guessed edit: %+v", d.Fix)
		}
	}
}

func TestSweepAppThirdDeclarationCannotResurrectAConflict(t *testing.T) {
	ds := fixture(t, map[string]string{
		"a.go":   "//go:build a\n\npackage main\n\nconst prefix = \"/api/\"\n",
		"b.go":   "//go:build b\n\npackage main\n\nconst prefix = \"/api\"\n",
		"c.go":   "//go:build c\n\npackage main\n\nconst prefix = \"/api/\"\n",
		"use.go": "package main\n\nimport \"strings\"\n\nfunc f(path string) bool { return strings.HasPrefix(path, prefix) }\n",
	})
	assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
}

func TestSweepAppDefaultedSeparatorParamStaysDynamic(t *testing.T) {
	ds := fixture(t, map[string]string{
		"x.go": "package main\n\nimport (\n\t\"path/filepath\"\n\t\"strings\"\n)\n\nfunc f(path, root, sep string) bool {\n\tif sep == \"\" {\n\t\tsep = string(filepath.Separator)\n\t}\n\treturn strings.HasPrefix(path, root+sep)\n}\n",
	})
	assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
}

func TestSweepAppPackageVarPrefixStaysDynamic(t *testing.T) {
	ds := fixture(t, map[string]string{
		"x.go": "package main\n\nimport \"strings\"\n\nvar prefix = \"/api/\"\n\nfunc f(path string) bool { return strings.HasPrefix(path, prefix) }\n",
	})
	assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
}

func TestSweepAppRelIsMatchedExactly(t *testing.T) {
	ds := fixture(t, map[string]string{
		"x.go": "package main\n\nimport \"strings\"\n\nfunc f(release string) bool { return strings.HasPrefix(release, \"v1\") }\n",
	})
	assertNot(t, ds, contracts.RulePrefixSegmentBoundary, "release is not rel")
}

func TestSweepAppCalleeDoesNotMakeAHaystackStrong(t *testing.T) {
	ds := fixture(t, map[string]string{
		"x.go": "package main\n\nimport (\n\t\"path/filepath\"\n\t\"strings\"\n)\n\nfunc f(name string) bool { return strings.HasPrefix(filepath.ToSlash(name), \"screen_\") }\n",
	})
	assertNot(t, ds, contracts.RulePrefixSegmentBoundary, "filepath names the transform, not the value")
}

func TestSweepAppParamShadowsPackageConstant(t *testing.T) {
	ds := fixture(t, map[string]string{
		"x.go": "package main\n\nimport \"strings\"\n\nconst prefix = \"/api/\"\n\nfunc f(path, prefix string) bool { return strings.HasPrefix(path, prefix) }\n",
	})
	assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
}

const sweepAuthNew = "package main\n\nimport \"github.com/DonaldMurillo/gofastr/battery/auth\"\n\nfunc wire() {\n\tmgr := auth.New(auth.AuthConfig{})\n\t_ = mgr\n}\n"

func TestSweepAppAcronymStaysOneToken(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + "\nfunc wire(app *framework.App) {\n\tapp.Entity(\"people\", entity.EntityConfig{\n\t\tFields: []schema.Field{{Name: \"SSNValue\", Type: schema.String}},\n\t})\n}\n",
	})
	assertHas(t, ds, contracts.RuleUnscopedPII)
}

func TestSweepAppPIIMatchesWholeTokens(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + "\nfunc wire(app *framework.App) {\n\tapp.Entity(\"stats\", entity.EntityConfig{\n\t\tFields: []schema.Field{{Name: \"cardinality\", Type: schema.String}},\n\t})\n}\n",
	})
	assertNot(t, ds, contracts.RuleUnscopedPII, "cardinality is not card")
}

func TestSweepAppSelectorIsNotADotImportedMount(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":  sweepAuthNew,
		"opts.go":  "package main\n\nimport . \"github.com/DonaldMurillo/gofastr/battery/auth\"\n\nvar _ = AuthConfig{}\n\nfunc f(o *opts) { o.RequireAuth = true }\n",
		"types.go": "package main\n\ntype opts struct{ RequireAuth bool }\n",
	})
	assertHas(t, ds, contracts.RuleAuthNotWired)
}

func TestSweepAppAuthNewMustBeCalledByName(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/battery/auth\"\n\nvar ctor = auth.New\n\nfunc f() { _ = auth.NewMemoryStore() }\n",
	})
	assertNot(t, ds, contracts.RuleAuthNotWired, "a reference to New and another New* configure no manager")
}

func TestSweepAppOneAuthSitePerPackage(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/battery/auth\"\n\nfunc wire() {\n\t_ = auth.New(auth.AuthConfig{})\n\t_ = auth.New(auth.AuthConfig{})\n}\n",
	})
	if found := countRule(t, ds, contracts.RuleAuthNotWired); len(found) != 1 {
		t.Fatalf("one package is one wiring decision, got %v", found)
	}
}

func TestSweepAppMountedReaderCountsAsAuth(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": "package main\n\nimport (\n\t\"github.com/DonaldMurillo/gofastr/battery/auth\"\n\t\"github.com/DonaldMurillo/gofastr/core/schema\"\n\t\"github.com/DonaldMurillo/gofastr/framework\"\n\t\"github.com/DonaldMurillo/gofastr/framework/entity\"\n)\n\nfunc wire(app *framework.App) {\n\tapp.Use(auth.RequireAuth)\n\tapp.Entity(\"posts\", entity.EntityConfig{\n\t\tFields: []schema.Field{{Name: \"title\", Type: schema.String}},\n\t})\n}\n",
	})
	assertNot(t, ds, contracts.RuleCrudWithoutAuth, "a mounted reader is auth wired")
}

func TestSweepAppOnlyALiteralFalseDisablesCRUD(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + "\nfunc wire(app *framework.App) {\n\tapp.Entity(\"posts\", entity.EntityConfig{\n\t\tExposure: entity.ExposureConfig{CRUD: entity.Ptr(true), MCP: true},\n\t})\n}\n",
	})
	assertNot(t, ds, contracts.RuleMCPWithoutCRUD, "CRUD set to true keeps the routes")
}

func TestSweepAppGrantNeedsTheAccessImport(t *testing.T) {
	ds := hookFixture(t, "package main\n\ntype ledger struct{}\n\nfunc (l *ledger) Grant(role string, perms ...string) {}\n\nfunc wire(l *ledger) { l.Grant(\"admin\", \"posts:read\") }\n",
		`{"version":1,"routes":{},"entities":{},"hooks":{},"roles":[]}`)
	assertNot(t, ds, contracts.RuleRoleNotExercised, "no access import, no RolePolicy")
}

func TestSweepAppGrantNeedsAPermissionArgument(t *testing.T) {
	ds := hookFixture(t, "package main\n\nimport \"github.com/DonaldMurillo/gofastr/framework/access\"\n\nvar _ access.RolePolicy\n\nfunc wire(l *ledger) { l.Grant(\"acct-1\", \"five hundred\") }\n",
		`{"version":1,"routes":{},"entities":{},"hooks":{},"roles":[]}`)
	assertNot(t, ds, contracts.RuleRoleNotExercised, "no argument looks like resource:verb")
}

func TestSweepAppVariableEntityConfigRecordsNothing(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + "\nfunc wire(app *framework.App, cfg entity.EntityConfig) {\n\tapp.Entity(\"posts\", cfg)\n}\n",
	})
	assertNot(t, ds, contracts.RuleCrudWithoutAuth, "an unreadable config makes no claim")
}

func TestSweepAppVariableAccessCountsAsScoped(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + "\nvar acl entity.AccessControl\n\nfunc wire(app *framework.App) {\n\tapp.Entity(\"people\", entity.EntityConfig{\n\t\tFields:   []schema.Field{{Name: \"email\", Type: schema.String}},\n\t\tExposure: entity.ExposureConfig{Access: acl},\n\t})\n}\n",
	})
	assertNot(t, ds, contracts.RuleUnscopedPII, "an Access value the pass cannot read is assumed to gate")
}

func TestSweepAppRelationColumnIsNotPII(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + "\nfunc wire(app *framework.App) {\n\tapp.Entity(\"orders\", entity.EntityConfig{\n\t\tFields: []schema.Field{{Name: \"email\", Type: schema.Relation}},\n\t})\n}\n",
	})
	assertNot(t, ds, contracts.RuleUnscopedPII, "a relation column holds a key, not the PII")
}

func TestSweepAppFieldKeyIsNotADotImportedMount(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":  sweepAuthNew,
		"opts.go":  "package main\n\nimport . \"github.com/DonaldMurillo/gofastr/battery/auth\"\n\nvar _ = AuthConfig{}\n\nvar _ = opts{RequireAuth: true}\n",
		"types.go": "package main\n\ntype opts struct{ RequireAuth bool }\n",
	})
	assertHas(t, ds, contracts.RuleAuthNotWired)
}

func TestSweepAppUnplacedRegisterWithoutPolicyImportKeepsTheRegistry(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":  permApp(`p.Register("plans:write")`, `_ = framework.AccessControl{Create: "plans:wirte"}`),
		"other.go": "package main\n\nfunc other() { registry().Register(\"plans:admin\") }\n",
	})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

func TestSweepAppTransitionPermissionMustLookLikeOne(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")`,
		`_ = entity.Transition{Permission: "approve"}`)})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "a Transition named approve is not resource:verb")
}

func TestSweepAppImportedPackageRegisterIsNotAPolicy(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": "package main\n\nimport (\n\t\"example.com/app/metrics\"\n\n\t\"github.com/DonaldMurillo/gofastr/framework\"\n\t\"github.com/DonaldMurillo/gofastr/framework/access\"\n)\n\nvar _ = framework.AccessControl{}\n\nfunc wire(p *access.RolePolicy) {\n\tp.Register(\"plans:write\")\n\tmetrics.Register(\"plans:read\")\n\t_ = framework.AccessControl{Create: \"plans:wirte\"}\n}\n"})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

func TestSweepAppSharedLibraryIsJudgedPerBinary(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":    "package main\n\nimport (\n\t_ \"example.com/app/lib\"\n\n\t\"github.com/DonaldMurillo/gofastr/framework/access\"\n)\n\nfunc wire(p *access.RolePolicy) { p.Register(\"plans:write\") }\n",
		"lib/lib.go": "package lib\n\nimport (\n\t\"github.com/DonaldMurillo/gofastr/framework\"\n\t\"github.com/DonaldMurillo/gofastr/framework/access\"\n)\n\nfunc wire(p *access.RolePolicy) {\n\tp.Register(\"orders:read\")\n\t_ = framework.AccessControl{Create: \"plans:write\"}\n}\n",
	})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "the binary that links lib registers plans:write")
}

func TestSweepAppRouteFilesNeedARouterImport(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": "package main\n\ntype Router struct{}\n\nfunc (Router) Handle(string, string, any) {}\n\nfunc wire(r *Router) { r.Handle(\"GET\", \"/users/:id\", nil) }\n",
	})
	assertNot(t, ds, contracts.RuleColonPathParam, "a file that cannot reach the router registers no routes")
}

func TestSweepAppConstructedRouterIsSeen(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\nfunc main() {\n\tr := router.New()\n\tr.Handle(\"GET\", \"/users/:id\", nil)\n}\n",
	})
	assertHas(t, ds, contracts.RuleColonPathParam)
}

func TestSweepAppGroupPrefixSqueezesDoubleSlashes(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile("\tg := r.Group(\"/api/\")\n\tg.Handle(\"GET\", \"/x\", nil)\n\tr.Handle(\"GET\", \"/api/x\", nil)"),
	})
	assertHas(t, ds, contracts.RuleDuplicateRoute)
}

const sweepEmptyManifest = `{"version":1,"routes":{},"entities":{},"hooks":{},"roles":[],"permissions":[]}`

func TestSweepAppOnlyPermissionShapedArgumentsAreDeclared(t *testing.T) {
	ds := hookFixture(t, "package main\n\nimport \"github.com/DonaldMurillo/gofastr/framework/access\"\n\nfunc f(p *access.RolePolicy) {\n\tp.Grant(\"editor\", \"posts:write\")\n\tp.Can(nil, \"note: hello\")\n\tp.Can(nil, \"posts:read\")\n}\n", sweepEmptyManifest)
	found := countRule(t, ds, contracts.RulePermissionNotExercised)
	if len(found) != 1 || !strings.Contains(found[0].Message, "posts:read") {
		t.Fatalf("only resource:verb strings are permissions, got %v", found)
	}
}

func TestSweepAppEntityAccessPermissionsAreDeclared(t *testing.T) {
	ds := hookFixture(t, entityImports+"\nfunc wire(app *framework.App) {\n\tapp.Entity(\"posts\", entity.EntityConfig{\n\t\tExposure: entity.ExposureConfig{Access: entity.AccessControl{Read: \"posts:read\"}},\n\t})\n}\n", sweepEmptyManifest)
	found := countRule(t, ds, contracts.RulePermissionNotExercised)
	if len(found) != 1 || !strings.Contains(found[0].Message, "posts:read") {
		t.Fatalf("an Access block's permission needs a request to prove it, got %v", found)
	}
}

func TestSweepAppEveryStringArgumentOfASkipIsRead(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":      "package main\n",
		"main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tt.Skipf(\"%s\", \"flaky on CI\")\n}\n",
	})
	assertHas(t, ds, contracts.RuleDisabledTest)
}

func TestSweepAppShortGuardIsALaneBoundary(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":      "package main\n",
		"main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tif testing.Short() {\n\t\tt.Skip(\"flaky\")\n\t}\n}\n",
	})
	assertNot(t, ds, contracts.RuleDisabledTest, "a testing.Short guard marks a lane")
}
