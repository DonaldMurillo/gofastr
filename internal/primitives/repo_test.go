package primitives

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var sentenceBreak = regexp.MustCompile(`[.!?]\s+\S`)

//gofastr:test-reads primitives/ *.go *.md
func TestRepoRegistryIsClean(t *testing.T) {
	c, findings, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	findings = append(findings, c.Lint()...)
	for _, f := range findings {
		t.Error(f)
	}
	if len(c.Primitives) == 0 {
		t.Fatal("the repo registry holds no primitive: the check would pass on an empty tree")
	}
	stale, err := c.Stale()
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range slices.Sorted(maps.Keys(stale)) {
		t.Errorf("%s is %s: run `go run ./cmd/primitives gen`", rel, stale[rel])
	}
	for _, p := range c.Primitives {
		if sentenceBreak.MatchString(p.Summary) {
			t.Errorf("%s: summary must be one sentence, got %q", p.File, p.Summary)
		}
	}
}

// agentsMDMaxLines is the guide/stays-short limit.
const agentsMDMaxLines = 60

//gofastr:test-reads /AGENTS.md
func TestAgentsMDStaysShort(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(string(guide), "\n")
	if len(guide) > 0 && guide[len(guide)-1] != '\n' {
		lines++
	}
	if lines > agentsMDMaxLines {
		t.Errorf("AGENTS.md is %d lines, over the %d-line limit (guide/stays-short): move the new rule into the primitive it governs, or into docs/contributing.md when no primitive can own it", lines, agentsMDMaxLines)
	}
}

//gofastr:test-reads /CLAUDE.md
func TestClaudeMDOnlyImportsAgentsMD(t *testing.T) {
	got, err := os.ReadFile(filepath.Join("..", "..", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "@AGENTS.md" {
		t.Errorf("CLAUDE.md holds %q, want only the line @AGENTS.md (guide/claude-md-only-imports): put the text in AGENTS.md, which every agent reads", got)
	}
}

// ruleRef is the primitive/invariant pair that ends a numbered rule,
// whatever separates the rule from it. Paths earlier in the rule, such
// as `framework/ui`, are not references.
var ruleRef = regexp.MustCompile("(?m)^\\d+\\. .*`([a-z0-9.-]+)/([a-z0-9-]+)`\\.?$")

func ruleRefs(guide string) [][2]string {
	var refs [][2]string
	for _, m := range ruleRef.FindAllStringSubmatch(guide, -1) {
		refs = append(refs, [2]string{m[1], m[2]})
	}
	return refs
}

func TestRuleRefsIgnoreTheSeparator(t *testing.T) {
	got := ruleRefs("1. A rule: `a/b-c`.\n2. Another rule \u2014 `ui.x/not-a-rule`.\n3. Survey `framework/ui` first.\n- not numbered `c/d`\n")
	want := [][2]string{{"a", "b-c"}, {"ui.x", "not-a-rule"}}
	if !slices.Equal(got, want) {
		t.Errorf("ruleRefs = %v, want %v", got, want)
	}
}

//gofastr:test-reads /AGENTS.md
func TestAgentsMDNamesRealRules(t *testing.T) {
	root := filepath.Join("..", "..")
	guide, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	named := 0
	for _, ref := range ruleRefs(string(guide)) {
		named++
		p := c.Primitive(ref[0])
		if p == nil {
			t.Errorf("AGENTS.md names %s/%s, and there is no primitive %s", ref[0], ref[1], ref[0])
			continue
		}
		if c.Invariant(p, ref[1]) == nil {
			t.Errorf("AGENTS.md names %s/%s, which is not an invariant of %s", ref[0], ref[1], ref[0])
		}
	}
	if named == 0 {
		t.Fatal("AGENTS.md names no registry invariant: the hard rules lost their pointers")
	}
}

// ownedPackages are the package directories PLAN.md step 4 has covered:
// every non-test Go file directly in one has an owner (app/every-file-has-an-owner).
var ownedPackages = []string{
	"framework", "framework/crud",
	"framework/headless",
	"framework/uihost", "framework/uihost/uinoderender", "framework/uihost/internal/sessiontoken",
	"framework/ui", "framework/ui/theme", "framework/ui/icons", "framework/ui/icons/internal/generate", "framework/ui/internal/treegate",
	"framework/contracts", "framework/contracts/analyzers", "framework/contracts/internal/codetext", "framework/contracts/internal/docgate",
	"framework/entityui", "framework/migrate", "framework/embed", "framework/entity", "framework/access",
	"framework/image", "framework/image/internal/vp8l", "framework/imagefield",
	"framework/static",
	"framework/outbox", "framework/event", "framework/fanout", "framework/cron",
	"framework/filter", "framework/pagination", "framework/openapi", "framework/sdk", "framework/dsl",
	"framework/file", "framework/pluginhost", "framework/isolation", "framework/localrun",
	"framework/localapp", "framework/localentity", "framework/dev",
	"framework/gallery", "framework/i18nui", "framework/sdkdocs", "framework/sdkdocs/internal/docpage",
	"framework/docs", "framework/docs/mcptools",
	"framework/testkit", "framework/testkit/axetest", "framework/testkit/cssweight", "framework/testkit/nojstest",
	"framework/semcov", "framework/axecov", "framework/agentsinv", "framework/factory",
	"framework/processmoduletest", "framework/uie2e", "framework/internal/pmtestkit",
	"framework/ratelimit", "framework/hook", "framework/db", "framework/db/sqlbusy", "framework/lifecycle",
	"framework/routegroup", "framework/slowquery", "framework/tenant", "framework/softdelete", "framework/owner",
	"internal/casing", "framework/internal/exif", "framework/internal/jpegcost",
	"framework/internal/jpegtest", "framework/internal/testdb",
	"framework/experimental/apiversions", "framework/experimental/webmcp",
	"core/mcp",
	"core/schema",
	"core/query",
	"core/render",
	"core/middleware",
	"core/router",
	"core/backoff",
	"core/dotenv",
	"core/fuzzy",
	"core/jcs",
	"core/textsafe",
	"core/yaml",
	"core/stream",
	"internal/sseframe",
	"core/moduleproto",
	"core/acp",
	"core/a2a",
	"core/fanout",
	"core/openapi",
	"core/datexport",
	"core/markdown",
	"core/migrate",
	"core/netguard",
	"core/upload",
	"core/static",
	"core/handler",
	"core/webbotauth",
	"core/unmounted",
	"core/tracing",
	"core/settings",
	"core/featureflag",
	"core/config",
	"core/i18n",
	"core/i18n/internal/cldrgen",
	"core-ui/app",
	"core-ui/app/decide",
	"core-ui/component",
	"core-ui/check",
	"core-ui/compute",
	"core-ui/di",
	"core-ui/html",
	"core-ui/interactive",
	"core-ui/island",
	"core-ui/localdb",
	"core-ui/node",
	"core-ui/noderender",
	"framework/experimental/harness",
	"framework/experimental/harness/client/tui",
	"framework/experimental/harness/client/web",
	"framework/experimental/harness/context",
	"framework/experimental/harness/control",
	"framework/experimental/harness/control/auth",
	"framework/experimental/harness/control/inproc",
	"framework/experimental/harness/control/mcpserver",
	"framework/experimental/harness/control/multiplex",
	"framework/experimental/harness/control/resources",
	"framework/experimental/harness/control/rest",
	"framework/experimental/harness/control/ws",
	"framework/experimental/harness/engine",
	"framework/experimental/harness/hook",
	"framework/experimental/harness/ids",
	"framework/experimental/harness/internal/clock",
	"framework/experimental/harness/internal/jsonredact",
	"framework/experimental/harness/internal/ulid",
	"framework/experimental/harness/logging",
	"framework/experimental/harness/mcpclient",
	"framework/experimental/harness/memory",
	"framework/experimental/harness/profile",
	"framework/experimental/harness/provider",
	"framework/experimental/harness/provider/credstore",
	"framework/experimental/harness/provider/helper",
	"framework/experimental/harness/provider/internal/openai",
	"framework/experimental/harness/provider/openrouter",
	"framework/experimental/harness/provider/providertest",
	"framework/experimental/harness/provider/zai",
	"framework/experimental/harness/secrets",
	"framework/experimental/harness/session",
	"framework/experimental/harness/session/sqlite",
	"framework/experimental/harness/skill",
	"framework/experimental/harness/skill/skillmd",
	"framework/experimental/harness/slash",
	"framework/experimental/harness/tool",
	"framework/experimental/harness/tool/builtins",
	"framework/experimental/harness/tool/permission",
	"framework/experimental/harness/tracing",
	"core-ui/urlsafe",
	"core-ui/widget/theme",
	"core-ui/widget/preset",
	"core-ui/seo",
	"core-ui/urlprefix",
	"core-ui/runtime/internal/treegate",
	"core-ui/runtime/minify",
	"core-ui/registry",
	"core-ui/store",
	"core-ui/uinodev1",
	"core-ui/runtime",
	"core-ui/widget",
	"core-ui/ownstyle",
	"core-ui/style",
	"battery/notify",
	"battery/storage",
	"battery/print",
	"battery/print/chromepdf",
	"battery/relay",
	"battery/setup",
	"battery/search",
	"battery/email",
	"battery/cache",
	"battery/sitesearch",
	"battery/telemetry",
	"battery/telemetry/telemetryui",
	"battery/telemetry/telemetryadmin",
	"battery/rtc",
	"battery/log",
	"battery/service",
	"battery/queue",
	"battery/admin",
	"battery/semantic",
	"battery/webhook",
	"battery/desktop",
	"battery/desktop/appstate",
	"battery/desktop/desktoptest",
	"battery/desktop/internal/fakecgo",
	"battery/desktop/internal/ffi",
	"battery/desktop/internal/gtk",
	"battery/desktop/internal/objc",
	"battery/desktop/internal/placement",
	"battery/desktop/internal/update",
	"battery/desktop/internal/win32",
	"battery/desktop/internal/windowtest",
	"battery/desktop/linux",
	"battery/desktop/macos",
	"battery/desktop/native",
	"battery/desktop/ui",
	"battery/desktop/windows",
	"battery/auth",
	"battery/auth/authui",
	"cmd/affected",
	"cmd/bench-resources",
	"cmd/check-csp",
	"cmd/check-embed",
	"cmd/check-embed/embedcheck",
	"cmd/kiln",
	"cmd/mutate",
	"cmd/mutate/guardmut",
	"cmd/primitives",
	"cmd/repolint",
	"cmd/vettool",
	// cmd/gofastr, scaffolding and project tools.
	"cmd/gofastr/internal/agentscmd",
	"cmd/gofastr/internal/auditcmd",
	"cmd/gofastr/internal/docscmd",
	"cmd/gofastr/internal/generatecmd",
	"cmd/gofastr/internal/initcmd",
	"cmd/gofastr/internal/migratecmd",
	"cmd/gofastr/internal/newcmd",
	"cmd/gofastr/internal/semanticcmd",
	"cmd/gofastr/internal/themecmd",
	"cmd/gofastr/internal/unmountcmd",
	"cmd/gofastr/internal/upgradecmd",
	"cmd/gofastr/internal/validatecmd",
	// cmd/gofastr, the CLI shell and the site packages.
	"cmd/gofastr",
	"cmd/gofastr/internal/cli",
	"cmd/gofastr/internal/clitest",
	"cmd/gofastr/packages",
	"cmd/gofastr/packages/docpage",
	"cmd/gofastr/packages/sitefooter",
	"cmd/gofastr/packages/siteheader",
	// cmd/gofastr, the dev loop.
	"cmd/gofastr/internal/loopback",
	"cmd/gofastr/internal/testcmd",
	"cmd/gofastr/internal/telemetrycmd",
	"cmd/gofastr/internal/projectfs",
	"cmd/gofastr/internal/harnesscmd",
	"cmd/gofastr/internal/devcmd",
}

//gofastr:test-reads primitives/ *.go
func TestCoveredPackagesHaveNoUnownedFile(t *testing.T) {
	c, _, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, dir := range ownedPackages {
		files, err := filepath.Glob(filepath.Join("..", "..", filepath.FromSlash(dir), "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		checked := 0
		for _, f := range files {
			name := filepath.Base(f)
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			checked++
			if rel := dir + "/" + name; len(c.Chain(rel)) == 0 {
				t.Errorf("%s has no owner: add it to the code list of the primitive whose job it does, or add a primitive", rel)
			}
		}
		if checked == 0 {
			t.Errorf("%s holds no Go file: the check would pass on an empty directory", dir)
		}
	}
}
