//check-csp:ignore-file
// Every rule ships a deliberately-wrong example, and the one for
// GOFASTR1804 is an inline style attribute. Same exemption check-csp
// applies to its own source, and the same reason gofastr.contracts.yml
// exempts this package from the rendering and SQL rules: a catalog of
// counter-examples trips the checks it documents.

package contracts

// The rule catalog. Every contract GoFastr enforces is declared here, as
// data, in one file, so "what does this framework actually demand of my
// app" is answerable by reading, without running anything and without
// grepping twelve analyzers.
//
// # Numbering
//
// Each capability owns a hundred-wide block. IDs are permanent: a rule
// that is deleted leaves its number retired rather than recycled, because
// a `//gofastr:allow(GOFASTR1403)` written two years ago must never
// silently start suppressing something else.
//
//	0000  meta            1500  performance
//	1000  routing         1600  data
//	1100  testing         1700  entities
//	1200  accessibility   1800  rendering
//	1300  architecture    1900  permissions
//	1400  security        2000  ai
//
// # Writing a rule
//
// Why and Fix are not optional and not decoration. Why must name the
// consequence and who suffers it; Fix must name the API. If you cannot
// write a Fix, the finding is an opinion and does not belong in the
// catalog. Examples are the difference between an agent fixing the code
// and an agent suppressing the rule.

// capabilityBlocks assigns each capability its hundred-wide ID range.
// [validateRule] enforces membership, so a rule filed under the wrong
// capability fails at init rather than confusing a reader later.
var capabilityBlocks = map[Capability]int{
	CapMeta:          0,
	CapRouting:       1000,
	CapTesting:       1100,
	CapAccessibility: 1200,
	CapArchitecture:  1300,
	CapSecurity:      1400,
	CapPerformance:   1500,
	CapData:          1600,
	CapEntities:      1700,
	CapRendering:     1800,
	CapPermissions:   1900,
	CapAI:            2000,
}

func capabilityBlock(c Capability) (int, bool) {
	n, ok := capabilityBlocks[c]
	return n, ok
}

// Meta rules: the contract system reporting on itself.
const (
	RuleSuppressionNoReason    = "GOFASTR0001"
	RuleSuppressionStale       = "GOFASTR0002"
	RuleSuppressionUnknownRule = "GOFASTR0003"
	RuleSuppressionMalformed   = "GOFASTR0004"
)

// Routing rules.
const (
	RuleDuplicateRoute   = "GOFASTR1001"
	RuleColonPathParam   = "GOFASTR1002"
	RuleUntestedRoute    = "GOFASTR1003"
	RuleStateAsRoute     = "GOFASTR1004"
	RuleNonUppercaseVerb = "GOFASTR1005"
)

// Testing rules.
const (
	RuleRouteNotExercised      = "GOFASTR1101"
	RulePermissionNotExercised = "GOFASTR1102"
	RuleEntityNotExercised     = "GOFASTR1103"
	RuleCoverageBelowMinimum   = "GOFASTR1104"
	RuleDisabledTest           = "GOFASTR1105"
	RuleNoCoverageManifest     = "GOFASTR1106"
	RuleHookNotFired           = "GOFASTR1107"
	RuleEventNotEmitted        = "GOFASTR1108"
	RuleRoleNotExercised       = "GOFASTR1109"
	RuleCoverageManifestBroken = "GOFASTR1110"
)

// Accessibility rules.
const (
	RuleMissingAlt            = "GOFASTR1201"
	RuleMissingAccessibleName = "GOFASTR1202"
	RuleUnnamedLandmark       = "GOFASTR1203"
	RuleIncompleteFormControl = "GOFASTR1204"
	RuleImplicitHeadingLevel  = "GOFASTR1205"
	RuleMissingElementMeta    = "GOFASTR1206"
)

// Architecture rules.
const (
	RuleLayerViolation  = "GOFASTR1301"
	RuleForbiddenImport = "GOFASTR1302"
)

// Security rules.
const (
	RuleSQLStringConcat = "GOFASTR1401"
	RuleFormWithoutCSRF = "GOFASTR1402"
	RuleHTMLConcat      = "GOFASTR1403"
	RuleInsecureCookie  = "GOFASTR1404"
	RuleHardcodedSecret = "GOFASTR1405"
)

// Performance rules.
const (
	RuleRegexpCompilePerCall = "GOFASTR1501"
	RuleQueryInLoop          = "GOFASTR1502"
	RuleReflectionPerRequest = "GOFASTR1503"
)

// Data rules.
const RuleIgnoredExec = "GOFASTR1601"

// Entity rules.
const (
	RuleMCPWithoutCRUD  = "GOFASTR1701"
	RulePublicEntity    = "GOFASTR1702"
	RuleCrudWithoutAuth = "GOFASTR1703"
)

// Rendering rules.
const (
	RuleBespokeCSS          = "GOFASTR1801"
	RuleHardNavigation      = "GOFASTR1802"
	RuleBespokeEventSource  = "GOFASTR1803"
	RuleInlineStyle         = "GOFASTR1804"
	RuleInlineScript        = "GOFASTR1805"
	RuleUnknownThemeToken   = "GOFASTR1806" // not-a-secret: a rule id, flagged only because the name ends in "Token"
	RuleHardcodedTokenValue = "GOFASTR1807"
)

// Permission rules.
const (
	RuleUnscopedPII       = "GOFASTR1901"
	RuleUnguardedMutation = "GOFASTR1902"
	RuleAuthNotWired      = "GOFASTR1903"
)

// AI-guidance rules.
const (
	RuleHandrolledCRUD    = "GOFASTR2001"
	RuleHandrolledBattery = "GOFASTR2002"
	RuleRawSQLOverRepo    = "GOFASTR2003"
)

func init() {
	RegisterRules(metaRules()...)
	RegisterRules(routingRules()...)
	RegisterRules(testingRules()...)
	RegisterRules(accessibilityRules()...)
	RegisterRules(architectureRules()...)
	RegisterRules(securityRules()...)
	RegisterRules(performanceRules()...)
	RegisterRules(dataRules()...)
	RegisterRules(entityRules()...)
	RegisterRules(renderingRules()...)
	RegisterRules(permissionRules()...)
	RegisterRules(aiRules()...)
}

func metaRules() []Rule {
	return []Rule{{
		ID: RuleSuppressionNoReason, Slug: "meta/suppression-without-reason",
		Title: "Suppression without a reason", Capability: CapMeta, Severity: SeverityError,
		Summary: "A `//gofastr:allow(...)` directive carries no explanation.",
		Why: "The whole value of an escape hatch is the sentence justifying it. " +
			"A bare suppression is indistinguishable from a mistake six months later, " +
			"so nobody dares delete it and it becomes permanent.",
		Fix: "Write the reason after the directive: `//gofastr:allow(GOFASTR1003) covered by the chromedp suite in examples/site`.",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  `//gofastr:allow(GOFASTR1403)`,
			Good: `//gofastr:allow(GOFASTR1403) the concatenated half is a compile-time constant`,
		}},
	}, {
		ID: RuleSuppressionStale, Slug: "meta/stale-suppression",
		Title: "Suppression matches nothing", Capability: CapMeta, Severity: SeverityWarn,
		Summary: "A suppression directive silences a finding that no longer occurs here.",
		Why: "Dead suppressions accumulate until nobody can tell which ones still matter. " +
			"Worse, one left on a moved line silently covers whatever code slid into its place.",
		Fix: "Delete the directive. The finding it silenced is gone.",
		Doc: "contracts", Autofix: true,
		Examples: []Example{{
			Bad:  "//gofastr:allow(GOFASTR1401) old concatenated query\nrows, err := db.Query(\"SELECT name FROM users WHERE id = ?\", id)",
			Good: "rows, err := db.Query(\"SELECT name FROM users WHERE id = ?\", id)",
		}},
	}, {
		ID: RuleSuppressionUnknownRule, Slug: "meta/unknown-suppressed-rule",
		Title: "Suppression names an unknown rule", Capability: CapMeta, Severity: SeverityError,
		Summary: "A `//gofastr:allow(...)` directive names a rule that is not in the catalog.",
		Why: "A typo'd rule ID suppresses nothing. The finding the author meant to silence " +
			"is still firing, or will start firing the moment the surrounding code changes.",
		Fix: "Correct the ID. `gofastr verify --list` prints the catalog; `gofastr verify --explain <id>` prints one rule.",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  "//gofastr:allow(GOFASTR9999) not an id in the catalog",
			Good: "//gofastr:allow(GOFASTR1401) reviewed: the id is parsed from r.PathValue as an int",
		}},
	}, {
		ID: RuleSuppressionMalformed, Slug: "meta/malformed-suppression",
		Title: "Malformed suppression", Capability: CapMeta, Severity: SeverityError,
		Summary: "A suppression directive names no rule, or tries to suppress everything.",
		Why: "`//gofastr:allow(all)` is a hole, not a decision: it silently absorbs every rule " +
			"added to the catalog afterwards, including ones written to catch the bug you are about to ship.",
		Fix: "Name each rule explicitly: `//gofastr:allow(GOFASTR1401,GOFASTR1403) <reason>`.",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  "//gofastr:allow(all) too noisy",
			Good: "//gofastr:allow(GOFASTR1401,GOFASTR1403) reviewed: static SQL, no user input",
		}},
	}}
}

func routingRules() []Rule {
	return []Rule{{
		ID: RuleDuplicateRoute, Slug: "routing/duplicate-route",
		Title: "Duplicate route registration", Capability: CapRouting, Severity: SeverityError,
		Summary: "The same method and path are registered more than once.",
		Why: "The underlying ServeMux panics on a duplicate at registration time, so this crashes " +
			"the app at boot rather than at build. Catching it here turns a failed deploy into a " +
			"failed build, which is the difference between a rollback and an edit.",
		Fix: "Delete one registration, or give them distinct paths. If both are wanted on one path, branch inside a single handler.",
		Doc: "project-structure",
		Examples: []Example{{
			Caption: "two handlers claiming POST /orders",
			Bad:     "r.Handle(\"POST\", \"/orders\", createOrder)\nr.Handle(\"POST\", \"/orders\", createOrderV2)",
			Good:    "r.Handle(\"POST\", \"/orders\", createOrder)\nr.Handle(\"POST\", \"/v2/orders\", createOrderV2)",
		}},
	}, {
		ID: RuleColonPathParam, Slug: "routing/colon-path-parameter",
		Title: "Express-style `:param` in a route pattern", Capability: CapRouting, Severity: SeverityError,
		Summary: "A route pattern uses `:name` for a path parameter instead of `{name}`.",
		Why: "GoFastr routes through net/http's ServeMux, which uses `{name}`. A `:name` segment is " +
			"matched **literally**, so `/users/:id` responds to a request for the path `/users/:id` " +
			"and 404s on `/users/42`. Nothing warns you: the route registers fine, the handler " +
			"compiles fine, and every real request misses it. This is the single most common habit " +
			"carried over from Express, Gin, and Chi.",
		Fix: "Use brace syntax, `/users/{id}`, and read the value with `r.PathValue(\"id\")`. For a trailing catch-all, `{path...}`.",
		// Not autofixable on purpose: rewriting the pattern alone turns a
		// loud 404 into a route whose handler still reads the parameter
		// the old way, which fails quietly. The handler has to change in
		// the same edit, and only a human knows what it should read.
		Doc: "project-structure",
		Examples: []Example{{
			Bad:  `r.Handle("GET", "/users/:id", showUser)`,
			Good: "r.Handle(\"GET\", \"/users/{id}\", showUser)\n// inside the handler: id := r.PathValue(\"id\")",
		}},
	}, {
		ID: RuleUntestedRoute, Slug: "routing/untested-route",
		Title: "Route has no test", Capability: CapRouting, Severity: SeverityWarn,
		Summary: "No test file in the module references this route's path.",
		Why: "An untested route is the one that breaks silently on a refactor. This is the static " +
			"half of the check: cheap, and it runs without a test run. The runtime half " +
			"(GOFASTR1101) proves the route was actually reached.",
		Fix: "Add a test that exercises the path. `framework.TestHarness(t, app)` gives you an in-process client; see `gofastr docs testkit`.",
		Doc: "testkit",
		Examples: []Example{{
			Bad:  "r.Handle(\"GET\", \"/reports/monthly\", monthlyReport) // no test file names this path",
			Good: "r.Handle(\"GET\", \"/reports/monthly\", monthlyReport) // reports_test.go: ta.Get(\"/reports/monthly\")",
		}},
	}, {
		ID: RuleStateAsRoute, Slug: "routing/in-page-state-as-route",
		Title: "In-page state modelled as a route", Capability: CapRouting, Severity: SeverityInfo,
		Summary: "A route path encodes sort, page, filter, or tab state.",
		Why: "Sorting a table is not navigation. Routing it costs a full page render, loses scroll " +
			"position and focus, and pushes a history entry the user did not ask for. " +
			"GoFastr models discrete in-page state as islands: the click fires an RPC and the runtime " +
			"swaps one fragment. This is advisory, and only checked in modules that render UI: the " +
			"evidence is a path segment's name, and URL-addressable pages are sometimes the point: a " +
			"blog's page 2 wants a URL, and a headless API using `/orders/page/{n}` is just REST. It " +
			"also only speaks to discrete list state. Continuous, client-owned state, like a map viewport's " +
			"lat/lng/zoom, an animated chart's range, or a scrubber position, is neither a route nor an " +
			"island: an RPC per pan would be absurd. That state lives in the client signal store, and " +
			"reflecting it into the URL so the view is shareable is a feature, not a finding.",
		Fix: "For sort/page/filter/tab, move the state into an island, a `core-ui/widget` Builder, and let the server return the new island HTML. For continuous state (maps, charts), keep it in the client signal store. See `gofastr docs interactive-patterns`.",
		Doc: "interactive-patterns",
		Examples: []Example{{
			Caption: "pagination as navigation vs as an island",
			Bad:     `r.Handle("GET", "/orders/page/{n}", ordersPage)`,
			Good:    "r.Handle(\"GET\", \"/orders\", ordersPage) // the table island handles ?page= internally",
		}},
	}, {
		ID: RuleNonUppercaseVerb, Slug: "routing/non-uppercase-method",
		Title: "Route method is not uppercase", Capability: CapRouting, Severity: SeverityError,
		Summary: "A route is registered with a lowercase or mixed-case HTTP method.",
		Why: "ServeMux compares methods literally. `\"get /x\"` registers without complaint and then " +
			"answers 405 to every real GET: a dead route that looks registered, with no boot-time " +
			"error and no log line to find it by.",
		Fix: `Uppercase the method: "GET", "POST", "PUT", "PATCH", "DELETE".`,
		Doc: "project-structure", Autofix: true,
		Examples: []Example{{
			Bad:  `r.Handle("post", "/orders", createOrder)`,
			Good: `r.Handle("POST", "/orders", createOrder)`,
		}},
	}}
}

func testingRules() []Rule {
	return []Rule{{
		ID: RuleRouteNotExercised, Slug: "testing/route-not-exercised",
		Title: "Route never reached by a test", Capability: CapTesting, Severity: SeverityWarn,
		Summary: "The semantic-coverage manifest records no test request that resolved to this route.",
		Why: "Line coverage says a function ran. It does not say a request ever reached it through " +
			"the real router, the real middleware chain, and the real auth check, which is where " +
			"routes actually break. A route with 100% line coverage and zero requests is untested.",
		Fix: "Exercise the route in a test. `framework.TestHarness` records every request it makes into `.gofastr/semantic-coverage.json`; run `go test ./...` once to populate it. For a test that drives the built binary over HTTP instead, set GOFASTR_SEMANTIC_COVERAGE=1 on the server process.",
		Doc: "testkit",
		Examples: []Example{{
			Bad:  "r.Handle(\"GET\", \"/reports/monthly\", monthlyReport) // manifest records no request here",
			Good: "ta := framework.TestHarness(t, app)\nta.Get(\"/reports/monthly\")",
		}},
	}, {
		ID: RulePermissionNotExercised, Slug: "testing/permission-not-exercised",
		Title: "Permission never checked by a test", Capability: CapTesting, Severity: SeverityWarn,
		Summary: "A declared permission was never evaluated during the recorded test run.",
		Why: "An unexercised permission is an unproven boundary. The common failure is not that " +
			"the check rejects the wrong person. It is that the check is never reached at all, " +
			"which no line-coverage number can distinguish from passing.",
		Fix: "Add a test that calls the guarded surface as a principal who lacks the permission, and assert the rejection.",
		Doc: "access-control",
		Examples: []Example{{
			Bad:  "policy.Grant(\"support\", \"orders:refund\") // no test ever acts as support",
			Good: "ta.AsUser(supportUser).Post(\"/orders/1/refund\", nil)",
		}},
	}, {
		ID: RuleEntityNotExercised, Slug: "testing/entity-crud-not-exercised",
		Title: "Entity operation never exercised", Capability: CapTesting, Severity: SeverityWarn,
		Summary: "An auto-generated CRUD operation was never called during the recorded test run.",
		Why: "Auto-CRUD is generated, so it is easy to assume it works. The parts that break are " +
			"the app-specific ones bolted to it: hooks, validators, owner scoping, includes. None " +
			"of those run until the endpoint does.",
		Fix: "Call the operation through the HTTP surface in a test: a `framework.TestHarness` request records it automatically.",
		Doc: "testkit",
		Examples: []Example{{
			Bad:  "app.Entity(\"invoices\", entity.EntityConfig{}) // no test calls DELETE /invoices/{id}",
			Good: "ta.Delete(\"/invoices/\" + created.ID)",
		}},
	}, {
		ID: RuleCoverageBelowMinimum, Slug: "testing/coverage-below-minimum",
		Title: "Line coverage below the configured floor", Capability: CapTesting, Severity: SeverityError,
		Summary: "The coverage profile reports less than the configured minimum.",
		Why: "A floor that drifts downward one merge at a time is not a floor. This check exists to " +
			"make the drift a build failure at the moment it happens, rather than a number someone " +
			"notices a quarter later.",
		Fix: "Add tests, or lower `contracts.coverage.minimum` deliberately: the change will be visible in review.",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  "contracts:\n  coverage:\n    minimum: 90   # profile reports 71%",
			Good: "contracts:\n  coverage:\n    minimum: 70   # lowered deliberately, visible in review",
		}},
	}, {
		ID: RuleDisabledTest, Slug: "testing/disabled-test",
		Title: "Test disabled without a stated boundary", Capability: CapTesting, Severity: SeverityError,
		Summary: "A `t.Skip` hides missing coverage rather than marking a lane boundary.",
		Why: "`t.Skip(\"TODO\")` reports as a pass. The suite stays green while the behaviour it " +
			"claims to cover is unverified: the most expensive kind of green.",
		Fix: "Fix the test, delete it, or state the boundary: an explicit `testing.Short()` guard, an environment-capability skip, or `//gofastr:allow(GOFASTR1105) <why>`.",
		Doc: "testkit",
		Examples: []Example{{
			Bad:  `t.Skip("not yet implemented")`,
			Good: "if testing.Short() {\n    t.Skip(\"chromedp suite: -short\")\n}",
		}},
	}, {
		ID: RuleHookNotFired, Slug: "testing/hook-not-fired",
		Title: "Lifecycle hook never ran", Capability: CapTesting, Severity: SeverityWarn,
		Summary: "A registered entity lifecycle hook never fired during the recorded test run.",
		Why: "This is the quietest way a feature stops working. The hook is still registered, " +
			"the handler it decorates still has coverage, and the suite is still green, but the " +
			"behaviour the hook adds (the audit row, the derived column, the cache bust) has not " +
			"happened once. Nothing in a line-coverage report can tell you that.",
		Fix: "Exercise the operation the hook is attached to through the HTTP surface: a `framework.TestHarness` request to the entity's create/update/delete endpoint fires it and records it.",
		Doc: "hooks-and-transactions",
		Examples: []Example{{
			Caption: "registered but never exercised",
			Bad: "framework.OnBeforeCreate[Post](app, `posts`, stampSlug)\n" +
				"// nothing in the suite ever POSTs /posts, so stampSlug has never run",
			Good: "framework.OnBeforeCreate[Post](app, `posts`, stampSlug)\n" +
				"// ta.Post(`/posts`, Post{Title: `x`}).AssertStatus(t, 201)",
		}},
	}, {
		ID: RuleEventNotEmitted, Slug: "testing/event-subscriber-not-exercised",
		Title: "Event subscriber never ran", Capability: CapTesting, Severity: SeverityWarn,
		Summary: "A handler is subscribed to an event type that was never published during the recorded test run.",
		Why: "A subscriber is invisible until its event fires. Nothing calls it directly, so it has " +
			"no callers to follow and no failing test to notice: the notification is simply never " +
			"sent, the projection never updated, and the suite stays green. Renaming the event type " +
			"on the emitting side breaks it silently and identically.",
		Fix: "Emit the event in a test, usually by exercising the operation that publishes it, so the subscriber runs at least once. If it is only ever emitted by another service, say so with a suppression.",
		Doc: "events",
		Examples: []Example{{
			Caption: "subscribed to a type nothing publishes",
			Bad: "bus.On(`order.place`, sendReceipt)" +
				" // the emitter publishes `order.placed`; sendReceipt has never run",
			Good: "bus.On(`order.placed`, sendReceipt)" +
				" // a checkout test emits order.placed, so the subscriber runs",
		}},
	}, {
		ID: RuleRoleNotExercised, Slug: "testing/role-not-exercised",
		Title: "Role never authenticated as", Capability: CapTesting, Severity: SeverityWarn,
		Summary: "A role is granted permissions but no recorded test ever ran a request holding it.",
		Why: "Granting a role permissions is a claim about who can do what, and the claim is " +
			"unverified until a request arrives carrying that role. The failure mode is not a " +
			"role that grants too little, which shows up as a broken feature. It is a role that " +
			"grants too much, which shows up as nothing at all until someone uses it.",
		Fix: "Add a test that authenticates as the role and exercises what it should and should not reach. `TestApp.AsUser` sets the caller; the role resolver in access.Middleware maps it.",
		Doc: "access-control",
		Examples: []Example{{
			Caption: "granted but never authenticated as",
			Bad: "policy.Grant(`support`, `orders:read`, `orders:refund`)" +
				" // no test ever issues a request as support",
			Good: "policy.Grant(`support`, `orders:read`, `orders:refund`)" +
				" // ta.AsUser(supportUser).Post(`/orders/1/refund`, nil)",
		}},
	}, {
		ID: RuleNoCoverageManifest, Slug: "testing/no-coverage-manifest",
		Title: "No semantic-coverage manifest", Capability: CapTesting, Severity: SeverityInfo,
		Summary: "`.gofastr/semantic-coverage.json` does not exist, so the semantic-coverage checks could not run.",
		Why: "Absence is reported rather than enforced: a fresh clone has never run its tests, and " +
			"walling off first verify behind a full test run would teach the wrong lesson. Drift " +
			"(a manifest that exists but misses a route) is the failure worth catching: that is GOFASTR1101.",
		Fix: "Run `go test ./...` once. Every `framework.TestHarness` request writes to the manifest from then on.",
		Doc: "testkit",
		Examples: []Example{{
			Bad:  "# .gofastr/semantic-coverage.json absent: the testing rules check nothing",
			Good: "go test ./...   # every TestHarness request records into the manifest",
		}},
	}}
}

func accessibilityRules() []Rule {
	return []Rule{{
		ID: RuleMissingAlt, Slug: "accessibility/missing-alt",
		Title: "Image without alt text", Capability: CapAccessibility, Severity: SeverityError,
		Summary: "An `html.Image` omits the Alt field.",
		Why: "A screen reader announces an image with no alt by reading its filename, or skips it " +
			"silently. Either way the user gets nothing. Omission is also ambiguous: the linter " +
			"cannot tell a forgotten alt from a decorative image.",
		Fix: `Set Alt. Informative image → describe what it shows ("Team photo at launch"). Decorative → explicit empty Alt: "" so it is skipped deliberately.`,
		Doc: "accessibility", Autofix: false,
		Examples: []Example{{
			Bad:  `html.Image(html.ImageConfig{Src: "/logo.png"})`,
			Good: `html.Image(html.ImageConfig{Src: "/logo.png", Alt: "GoFastr"})`,
		}},
	}, {ID: RuleCoverageManifestBroken, Slug: "testing/coverage-manifest-unreadable",
		Title: "Semantic-coverage manifest is unreadable", Capability: CapTesting, Severity: SeverityError,
		Summary: "`.gofastr/semantic-coverage.json` exists but cannot be parsed.",
		Why: "Absence and corruption are different failures. A missing manifest means the tests " +
			"have not run yet, which is normal on a fresh clone. A manifest that exists and cannot " +
			"be read means the record of what the tests covered is untrustworthy, and every " +
			"semantic-coverage check silently did not run. Reporting that at the same low severity " +
			"as absence relaxes enforcement at exactly the moment the evidence is broken.",
		Fix: "Delete `.gofastr/semantic-coverage.json` and re-run `go test ./...` to rebuild it. " +
			"If it keeps corrupting, a test harness is probably killing the process mid-flush.",
		Doc: "contracts", Autofix: false,
		Examples: []Example{{
			Bad:  "# .gofastr/semantic-coverage.json truncated mid-write\n{\"version\":1,\"routes\":{\"GET /a\"",
			Good: "rm .gofastr/semantic-coverage.json && go test ./...",
		}},
	}, {
		ID: RuleMissingAccessibleName, Slug: "accessibility/missing-accessible-name",
		Title: "Control without an accessible name", Capability: CapAccessibility, Severity: SeverityError,
		Summary: "A button or link has no text a screen reader can announce.",
		Why: `An icon-only button announces as "button". A link labelled "click here" is meaningless ` +
			"in the link list screen-reader users navigate by, which lists links out of context.",
		Fix: `Set Label (buttons) or Text (links) to the action or destination: "Close dialog", "View pricing".`,
		Doc: "accessibility",
		Examples: []Example{{
			Bad:  `html.Button(html.ButtonConfig{Icon: "x"})`,
			Good: `html.Button(html.ButtonConfig{Icon: "x", Label: "Close dialog"})`,
		}},
	}, {
		ID: RuleUnnamedLandmark, Slug: "accessibility/unnamed-landmark",
		Title: "Landmark without a name", Capability: CapAccessibility, Severity: SeverityError,
		Summary: "A nav, section, aside, or group landmark has no accessible name or role.",
		Why: "Screen-reader users navigate by landmark. Three `<nav>` elements that all announce as " +
			`"navigation" are worse than one, because now the user has to enter each to find out which is which.`,
		Fix: `Set Label ("Main", "Footer") or LabelledBy pointing at a heading id. If it is not a landmark, use a plain Div.`,
		Doc: "accessibility",
		Examples: []Example{{
			Bad:  "html.Nav(html.NavConfig{}, links...)",
			Good: "html.Nav(html.NavConfig{Label: \"Primary\"}, links...)",
		}},
	}, {
		ID: RuleIncompleteFormControl, Slug: "accessibility/incomplete-form-control",
		Title: "Form control missing required semantics", Capability: CapAccessibility, Severity: SeverityError,
		Summary: "An input, select, textarea, label, form, or fieldset omits a field assistive tech needs.",
		Why: "A control with no Name does not submit. A label with no For is not attached to anything: " +
			"clicking it does nothing and a screen reader announces the field as unlabelled. " +
			"Placeholder text is not a label: it disappears the moment the user types.",
		Fix: "Set Type and Name on inputs, For on labels (matching the input id), Method on forms, Legend on fieldsets.",
		Doc: "accessibility",
		Examples: []Example{{
			Bad:  "html.Input(html.InputConfig{})",
			Good: "html.Input(html.InputConfig{Type: \"email\", Name: \"email\"})",
		}},
	}, {
		ID: RuleImplicitHeadingLevel, Slug: "accessibility/implicit-heading-level",
		Title: "Heading without an explicit level", Capability: CapAccessibility, Severity: SeverityError,
		Summary: "An `html.Heading` omits Level.",
		Why: "Heading levels form the page outline screen-reader users navigate by. Picking a level " +
			"to get the font size you want breaks that outline: one h1, no skipped levels. Font size is a styling concern.",
		Fix: "Set Level explicitly to the level the outline needs, and size it with the design system's tokens.",
		Doc: "accessibility",
		Examples: []Example{{
			Bad:  "html.Heading(html.HeadingConfig{}, html.Text(\"Orders\"))",
			Good: "html.Heading(html.HeadingConfig{Level: 2}, html.Text(\"Orders\"))",
		}},
	}, {
		ID: RuleMissingElementMeta, Slug: "accessibility/missing-element-metadata",
		Title: "Element missing required metadata", Capability: CapAccessibility, Severity: SeverityError,
		Summary: "A core-ui/html element omits a field the accessibility contract requires.",
		Why: "Each of these fields exists because assistive technology has no way to infer it: an " +
			"abbreviation's expansion, a time element's machine-readable value, a media source's type.",
		Fix: "Fill in the field named in the message. `gofastr docs accessibility` lists the requirement per element.",
		Doc: "accessibility",
		Examples: []Example{{
			Bad:  "html.Time(html.TimeConfig{}, html.Text(\"yesterday\"))",
			Good: "html.Time(html.TimeConfig{Datetime: \"2026-08-03\"}, html.Text(\"yesterday\"))",
		}},
	}}
}

func architectureRules() []Rule {
	return []Rule{{
		ID: RuleLayerViolation, Slug: "architecture/layer-violation",
		Title: "Import points up the layer stack", Capability: CapArchitecture, Severity: SeverityError,
		Summary: "A package imports one from a layer above it.",
		Why: "Layering is what keeps a codebase reorganizable. One upward import turns two " +
			"independently testable halves into one unit, and the next one makes it a cycle. " +
			"The cost is never visible at the moment the import is added, only months later, when nothing can move.",
		Fix: "Invert the dependency: define the interface the lower layer needs *in* the lower layer, and have the upper layer implement it.",
		Doc: "project-structure",
		Examples: []Example{{
			Caption: "core reaching up into the framework",
			Bad:     `package core/render // import "…/framework"`,
			Good:    "package core/render // define an interface here; framework implements it",
		}},
	}, {
		ID: RuleForbiddenImport, Slug: "architecture/forbidden-import",
		Title: "Forbidden import edge", Capability: CapArchitecture, Severity: SeverityError,
		Summary: "An import matches a `contracts.architecture.forbid` entry.",
		Why: "Some edges are banned for reasons no layer ordering expresses: a package that must " +
			"stay dependency-free so it can be vendored, or a decoder set that must not be linked " +
			"into every binary. The ban is only real if something checks it.",
		Fix: "Remove the import, or route it through the seam the forbid rule's reason names.",
		Doc: "project-structure",
		Examples: []Example{{
			Bad:  "import \"example.com/app/internal/store\" // forbid: ui -> store",
			Good: "import \"example.com/app/internal/service\" // the seam the forbid entry names",
		}},
	}}
}

func securityRules() []Rule {
	return []Rule{{
		ID: RuleSQLStringConcat, Slug: "security/sql-string-concat",
		Title: "User input concatenated into SQL", Capability: CapSecurity, Severity: SeverityError,
		Summary: "A SQL statement is built by string concatenation or Sprintf around request-derived data.",
		Why: "This is SQL injection. The variable reaches the database as syntax rather than as a " +
			"value, so a quote in it rewrites the statement. Every ORM in the process is bypassed at that line.",
		Fix: "Use placeholders, `$1`/`?`, and pass the value as an argument. For a dynamic identifier (table or column name), validate it against a fixed allow-list first.",
		Doc: "security",
		Examples: []Example{{
			Bad:  `db.Query("SELECT * FROM users WHERE email = '" + req.Email + "'")`,
			Good: `db.Query("SELECT * FROM users WHERE email = $1", req.Email)`,
		}},
	}, {
		ID: RuleFormWithoutCSRF, Slug: "security/form-without-csrf",
		Title: "POST form without a CSRF token", Capability: CapSecurity, Severity: SeverityError,
		Summary: "A `<form method=\"POST\">` is rendered without a CSRF input.",
		Why: "Any site the user visits can POST to yours with their cookies attached. Without a token " +
			"the request is indistinguishable from one the user meant to make, which is how a page " +
			"on another origin deletes their account.",
		Fix: "Render `CSRFInputFromCtx(ctx)` inside the form, or use `framework/ui` Form, which does it for you.",
		Doc: "security",
		Examples: []Example{{
			Bad:  `<form method="POST" action="/delete">`,
			Good: `<form method="POST" action="/delete">` + "\n  " + `{{ CSRFInputFromCtx(ctx) }}`,
		}},
	}, {
		ID: RuleHTMLConcat, Slug: "security/html-concat",
		Title: "Untrusted value concatenated into HTML", Capability: CapSecurity, Severity: SeverityError,
		Summary: "`render.HTML` is called on a concatenated string.",
		Why: "`render.HTML` means \"this is already safe markup: do not escape it\". Concatenating a " +
			"variable into it hands the user's string straight to the browser as markup. That is stored XSS.",
		Fix: "Use `render.Text` for untrusted values (it escapes), or compose `core-ui/html` elements. If the concatenated half is genuinely a constant, annotate `//gofastr:allow(GOFASTR1403) <why>`.",
		Doc: "security",
		Examples: []Example{{
			Bad:  `render.HTML("<h1>" + user.Name + "</h1>")`,
			Good: `html.Heading(html.HeadingConfig{Level: 1, Text: user.Name})`,
		}},
	}, {
		ID: RuleInsecureCookie, Slug: "security/insecure-cookie",
		Title: "Cookie without security attributes", Capability: CapSecurity, Severity: SeverityError,
		Summary: "An `http.Cookie` is constructed without HttpOnly, Secure, or SameSite.",
		Why: "A cookie without HttpOnly is readable by any script that gets injected. Without Secure " +
			"it travels over plain HTTP on the first request after a downgrade. Without SameSite it " +
			"rides along on cross-site requests, the CSRF token's whole reason for existing.",
		Fix: "Set HttpOnly: true, Secure: true, and SameSite: http.SameSiteLaxMode (or Strict). Session cookies minted by `battery/auth` already do this.",
		Doc: "security", Autofix: true,
		Examples: []Example{{
			Bad:  `http.SetCookie(w, &http.Cookie{Name: "sid", Value: token})`,
			Good: "http.SetCookie(w, &http.Cookie{\n    Name: \"sid\", Value: token,\n    HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,\n})",
		}},
	}, {
		ID: RuleHardcodedSecret, Slug: "security/hardcoded-secret",
		Title: "Secret literal in source", Capability: CapSecurity, Severity: SeverityError,
		Summary: "A key, token, password, or secret is assigned from a string literal.",
		Why: "A committed secret is a leaked secret: it is in every clone, every fork, and the reflog " +
			"after you delete it. Rotating is the only remedy, and rotation is expensive precisely " +
			"when you discover this.",
		Fix: "Read it from the environment (`os.Getenv`) or `WithSecret`/`GOFASTR_SECRET`. Test fixtures and public constants are fine: annotate them `// not-a-secret: <why>`.",
		Doc: "security",
		Examples: []Example{{
			Bad:  `apiKey := "sk-live-9f3c2a1b8e7d6c5f4a3b2c1d"`,
			Good: `apiKey := os.Getenv("STRIPE_API_KEY")`,
		}},
	}}
}

func performanceRules() []Rule {
	return []Rule{{
		ID: RuleRegexpCompilePerCall, Slug: "performance/regexp-compile-per-call",
		Title: "Regexp compiled inside a function", Capability: CapPerformance, Severity: SeverityWarn,
		Summary: "`regexp.MustCompile` or `regexp.Compile` runs on every call rather than once at init.",
		Why: "Compiling a pattern is orders of magnitude more expensive than matching with it. In a " +
			"request handler this turns a microsecond of work into a millisecond, on every request, forever.",
		Fix: "Hoist it to a package-level `var re = regexp.MustCompile(...)`.",
		Doc: "benchmarks",
		Examples: []Example{{
			Bad:  "func slugify(s string) string {\n    re := regexp.MustCompile(`[^a-z0-9]+`)\n    return re.ReplaceAllString(s, \"-\")\n}",
			Good: "var reSlug = regexp.MustCompile(`[^a-z0-9]+`)\n\nfunc slugify(s string) string {\n    return reSlug.ReplaceAllString(s, \"-\")\n}",
		}},
	}, {
		ID: RuleQueryInLoop, Slug: "performance/query-in-loop",
		Title: "Database query inside a loop", Capability: CapPerformance, Severity: SeverityWarn,
		Summary: "A Query/Exec/Get call sits inside a range or for loop.",
		Why: "This is the N+1: one query to get the list, then one per row. It is invisible with " +
			"ten rows in development and takes the database down with ten thousand in production.",
		Fix: "Fetch in one query: `?include=` / eager loading for relations, or an `IN (…)` batch. See `gofastr docs includes`.",
		Doc: "includes",
		Examples: []Example{{
			Bad:  "for _, o := range orders {\n    o.Customer, _ = repo.GetCustomer(ctx, o.CustomerID)\n}",
			Good: `orders, _ := repo.List(ctx, query.Include("customer"))`,
		}},
	}, {
		ID: RuleReflectionPerRequest, Slug: "performance/reflection-per-request",
		Title: "Reflection in a request handler", Capability: CapPerformance, Severity: SeverityInfo,
		Summary: "A handler calls into `reflect` on the request path.",
		Why: "Reflection defeats inlining and escape analysis, and its cost lands on every request " +
			"rather than once at startup. The framework reflects at registration time for exactly this reason.",
		Fix: "Move the reflection to init/registration and cache the result, or replace it with a generic function.",
		Doc: "benchmarks",
		Examples: []Example{{
			Bad:  "func handler(w http.ResponseWriter, req *http.Request) {\n	t := reflect.TypeOf(payload)\n	_ = t\n}",
			Good: "var payloadType = reflect.TypeOf(payload) // resolved once at init",
		}},
	}}
}

func dataRules() []Rule {
	return []Rule{{
		ID: RuleIgnoredExec, Slug: "data/ignored-exec",
		Title: "Write result discarded", Capability: CapData, Severity: SeverityError,
		Summary: "The result and error of a `db.Exec` are both assigned to `_`.",
		Why: "The write may not have happened. A constraint violation, a closed connection, a " +
			"read-only replica: all of them return an error here and none of them are visible. " +
			"The request returns 200 and the data is gone.",
		Fix: "Handle the error, or state that you mean it: `// best-effort: <why>` on the line or just above it.",
		Doc: "hooks-and-transactions",
		Examples: []Example{{
			Bad:  `_, _ = db.Exec("DELETE FROM sessions WHERE id = $1", id)`,
			Good: "if _, err := db.Exec(\"DELETE FROM sessions WHERE id = $1\", id); err != nil {\n    return fmt.Errorf(\"revoke session: %w\", err)\n}",
		}},
	}}
}

func entityRules() []Rule {
	return []Rule{{
		ID: RuleMCPWithoutCRUD, Slug: "entities/mcp-without-crud",
		Title: "Entity exposes MCP tools without CRUD routes", Capability: CapEntities, Severity: SeverityError,
		Summary: "An entity sets MCP without CRUD.",
		Why: "MCP entity tools dispatch in-process against the app's own router. With CRUD off the " +
			"routes do not exist, so every tool call 404s: the app boots, the tools list, and nothing works.",
		Fix: "Enable CRUD alongside MCP, or drop MCP for this entity.",
		Doc: "entity-declarations",
		Examples: []Example{{
			Bad:  "app.Entity(\"invoices\", entity.EntityConfig{Exposure: &entity.ExposureConfig{CRUD: boolPtr(false), MCP: true}})",
			Good: "app.Entity(\"invoices\", entity.EntityConfig{Exposure: &entity.ExposureConfig{MCP: true}}) // CRUD nil = on",
		}},
	}, {
		ID: RulePublicEntity, Slug: "entities/public-entity",
		Title: "Entity exposed anonymously", Capability: CapEntities, Severity: SeverityWarn,
		Summary: "An entity sets Public, opting out of the session requirement on every operation.",
		Why: "Auto-CRUD is secure by default: every operation requires a session. Public removes " +
			"that for reads *and writes*, so anyone on the internet can create and delete rows unless " +
			"another gate stops them. That is sometimes exactly right, and it should always be deliberate.",
		Fix: "Confirm the entity is genuinely public data. If only reads should be, keep Public off and grant read access through `access:` permissions instead.",
		Doc: "access-control",
		Examples: []Example{{
			Bad:  "app.Entity(\"invoices\", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}})",
			Good: "app.Entity(\"invoices\", entity.EntityConfig{Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: \"invoices:read\"}}})",
		}},
	}, {
		ID: RuleCrudWithoutAuth, Slug: "entities/crud-without-auth",
		Title: "CRUD entity exposed with no auth wired", Capability: CapEntities, Severity: SeverityWarn,
		Summary: "An entity mounts auto-CRUD routes, but the app wires no auth, so every operation 401s for every caller.",
		Why: "Auto-CRUD is secure by default: each operation requires a session. With no auth battery " +
			"(no auth.New and no SessionMiddleware / RequireAuth / BFF), no request ever carries a user, " +
			"so the entire CRUD surface is unreachable: the app boots and advertises endpoints that " +
			"always return 401. That is the worst first-contact signal: a curl to the documented URL " +
			"fails, which reads as broken. It is almost always an oversight, not a decision. Wiring " +
			"auth makes signed-in callers reach the API. This complements GOFASTR1903 (auth configured " +
			"but never mounted): 1903 fires when an auth.New exists with no reader; this fires when no " +
			"auth battery is present at all.",
		Fix: "Wire battery/auth, auth.New(auth.AuthConfig{…}) plus fwApp.Use(auth.SessionMiddleware(mgr)) (or auth.BFF), so authenticated callers reach the routes. If the entity is genuinely public data, set Exposure.Public (GOFASTR1702 then applies). Run `gofastr docs auth`.",
		Doc: "auth",
		Examples: []Example{{
			Bad:  "app.Entity(\"posts\", entity.EntityConfig{Exposure: &entity.ExposureConfig{}}) // CRUD on, not public, no auth wired",
			Good: "// Wire auth so signed-in callers can reach the API.\nimport \"github.com/DonaldMurillo/gofastr/battery/auth\"\n\nfunc wire(app *framework.App) {\n\tmgr := auth.New(auth.AuthConfig{})\n\tapp.Use(auth.SessionMiddleware(mgr))\n\tapp.Entity(\"posts\", entity.EntityConfig{})\n}",
		}},
	}}
}

func renderingRules() []Rule {
	return []Rule{{
		ID: RuleBespokeCSS, Slug: "rendering/bespoke-css",
		Title: "CSS outside the design system", Capability: CapRendering, Severity: SeverityError,
		Summary: "An app or generator ships its own CSS rules: CSS declarations in strings, not Go assigning token values.",
		Why: "Two styling surfaces means every future change has to be made twice and stays consistent " +
			"by luck. Bespoke CSS also loads in an order you do not control relative to component CSS, " +
			"so it wins or loses by specificity accident rather than by intent. The rule matches CSS " +
			"declarations, a property-colon-value shape in a string; a Go assignment of a design-system " +
			"token reference to a variable, `fill := \"var(--color-surface)\"`, is not one and does not fire.",
		Fix: "Compose `framework/ui` components and `core-ui/style` tokens. If the design system genuinely lacks what you need, add the component or token upstream and use it here. That is the fix, not a local rule.",
		Doc: "ui-getting-started",
		Examples: []Example{{
			Bad:  "const baseCSS = `.my-card { padding: 16px; border-radius: 8px; }`",
			Good: `ui.Card(ui.CardConfig{Padding: style.SpaceMD})`,
		}, {
			Bad:  "const btnCSS = `.btn { padding: var(--spacing-md); }`",
			Good: "fill := \"var(--color-surface)\" // a token reference assigned in Go is the encouraged shape",
		}},
	}, {
		ID: RuleHardNavigation, Slug: "rendering/hard-navigation",
		Title: "Full page reload used as navigation", Capability: CapRendering, Severity: SeverityError,
		Summary: "Client code assigns `location.href` or calls `location.reload()`.",
		Why: "It throws away the whole document to change one thing: scroll position, focus, form " +
			"state, and every open island go with it. It also re-downloads and re-parses the CSS and " +
			"runtime that were already there. Cross-page navigation in GoFastr is client-side with a cache.",
		Fix: "Let the runtime navigate, a plain `<a href>` is intercepted, or return `X-Gofastr-Location` from the server to redirect after an action.",
		Doc: "runtime-contract",
		Examples: []Example{{
			Bad:  `location.href = '/orders'`,
			Good: `<a href="/orders">Orders</a>  <!-- the runtime intercepts it -->`,
		}},
	}, {
		ID: RuleBespokeEventSource, Slug: "rendering/bespoke-event-source",
		Title: "Bespoke EventSource on an app surface", Capability: CapRendering, Severity: SeverityError,
		Summary: "Client code opens its own `EventSource` instead of using the shared SSE bus.",
		Why: "Browsers cap concurrent connections per origin, so every bespoke stream is one fewer " +
			"connection for everything else on the page, and the cap is low enough to hit. " +
			"GoFastr multiplexes every server push over one bus at `/__gofastr/sse`.",
		Fix: "Subscribe through the runtime's bus. For passive freshness (dashboards, counters, statuses) use `data-fui-poll` instead: no held connection at all.",
		Doc: "reactivity",
		Examples: []Example{{
			Bad:  `new EventSource('/my-feed')`,
			Good: `<div data-fui-poll="5s" data-fui-island="orders-count">`,
		}},
	}, {
		ID: RuleInlineStyle, Slug: "rendering/inline-style",
		Title: "Inline style attribute", Capability: CapRendering, Severity: SeverityError,
		Summary: "Markup carries a `style=\"…\"` attribute.",
		Why: "Inline styles beat every stylesheet rule, so the component they are applied to can no " +
			"longer be restyled or themed, including by the dark-mode tokens. They are also blocked " +
			"outright under a strict Content-Security-Policy.",
		Fix: "Use the component's config or a `core-ui/style` token. A value that genuinely varies per render belongs in a CSS custom property the component reads.",
		Doc: "theming",
		Examples: []Example{{
			Bad:  "html.Raw(`<div style=\"margin-top: 12px\">…</div>`)",
			Good: "ui.Stack(ui.StackConfig{Gap: \"md\"}, children...)",
		}},
	}, {
		ID: RuleUnknownThemeToken, Slug: "rendering/unknown-theme-token",
		Title: "var() references a token the theme does not emit", Capability: CapRendering, Severity: SeverityError,
		Summary: "Project CSS reads `var(--name)` where `name` is not a theme token.",
		Why: "An invalid var() is not a CSS error: it resolves to nothing and the declaration is " +
			"silently dropped, so a typo is invisible to the build, the browser console, and every " +
			"linter, and the only symptom is the styling not applying. Issue #214's reporter wrote " +
			"`--radius-lg` where the theme emits `--radii-lg`, and every rounded corner on the site " +
			"rendered square for days.",
		Fix: "Spell the token the theme emits (see `style.TokenNames()` or `gofastr docs theming`), declare the custom property in your own stylesheet if it is yours, or add a fallback: `var(--x, 8px)` degrades instead of dropping the declaration.",
		Doc: "theming",
		Examples: []Example{{
			Bad:  "border-radius: var(--radius-lg);",
			Good: "border-radius: var(--radii-lg);",
		}},
	}, {
		ID: RuleHardcodedTokenValue, Slug: "rendering/hardcoded-token-value",
		Title: "CSS hardcodes a value the theme declares as a token", Capability: CapRendering, Severity: SeverityError,
		Summary: "Design-system CSS sets a property to a literal that is exactly a theme token's value.",
		Why: "The design system's promise is that one token swap re-skins every surface. A literal copy of " +
			"a token's value silently opts out: re-theming --text-xs or --radii-sm leaves the rule behind, " +
			"and nothing shows the drift, because the rendered pixels are identical until the day someone " +
			"changes the token. core-ui/widget/theme carried `font-size: 0.75rem` beside a theme declaring " +
			"--text-xs: 0.75rem for months in exactly this way.",
		Fix: "Reference the token: `font-size: var(--text-xs)` in stylesheet strings, or the `{text.xs}` builder reference in StyleSheet/Set values. An off-scale value no token carries is a MISSING token: add it to the theme instead of hardcoding.",
		Doc: "theming",
		Examples: []Example{{
			Bad:  `ss.Rule(".eyebrow").Set("font-size", "0.75rem").End()`,
			Good: `ss.Rule(".eyebrow").Set("font-size", "{text.xs}").End()`,
		}},
	}}
}

func permissionRules() []Rule {
	return []Rule{{
		ID: RuleUnscopedPII, Slug: "permissions/unscoped-pii",
		Title: "Per-user data exposed without scoping", Capability: CapPermissions, Severity: SeverityError,
		Summary: "An entity with PII-shaped fields is auto-exposed with no owner field, tenant, or access rule.",
		Why: "Auto-CRUD requires a session, so this is not anonymous access: it is worse in a way " +
			"that is easy to miss in review: every logged-in user can read and write every *other* " +
			"user's row. Enabling auth does not close it; session middleware authenticates the caller " +
			"without scoping the rows.",
		Fix: "Set `OwnerField` to the column holding the user id, or declare `access:` permissions, or set `MultiTenant`. See `gofastr docs entity-declarations` → Per-user scoping.",
		Doc: "entity-declarations",
		Examples: []Example{{
			Bad:  "app.Entity(\"profiles\", entity.EntityConfig{}) // email, phone; no scoping",
			Good: "app.Entity(\"profiles\", entity.EntityConfig{Scope: &entity.ScopeConfig{OwnerField: \"user_id\"}})",
		}},
	}, {ID: RuleInlineScript, Slug: "rendering/inline-script",
		Title: "Inline script block", Capability: CapRendering, Severity: SeverityError,
		Summary: "Markup emits a `<script>` block with a body rather than a `src`.",
		Why: "The framework's default Content-Security-Policy is `default-src 'self'` with no " +
			"`unsafe-inline`, so the browser refuses to execute the block. Nothing fails at build " +
			"or render time: the page ships, the script silently never runs, and whatever it wired " +
			"up is simply missing in production while working in any environment with a laxer policy.",
		Fix: "Move the body to a file and reference it: `<script src=\"/static/x.js\">`. For behaviour " +
			"attached to server-rendered markup, prefer the runtime's `data-fui-*` hydration over a " +
			"script tag at all: see `gofastr docs runtime-contract`.",
		Doc: "security", Autofix: false,
		Examples: []Example{{
			Bad:  "html.Raw(`<script>document.title = \"Orders\"</script>`)",
			Good: "html.Raw(`<script src=\"/static/orders.js\" defer></script>`)",
		}},
	}, {
		ID: RuleUnguardedMutation, Slug: "permissions/unguarded-mutation",
		Title: "Mutating route with no access declaration", Capability: CapPermissions, Severity: SeverityWarn,
		Summary: "A POST, PUT, PATCH, or DELETE route declares no middleware, group, or access rule.",
		Why: "A write endpoint that nothing guards is reachable by anyone who can reach the process. " +
			"This is the single most common way an internal admin action becomes a public one, not " +
			"by decision, but by a route added outside the group that carried the guard.",
		Fix: "Register it inside a guarded `app.Group(...)`, or attach access explicitly. If it is genuinely public (a webhook receiver, a health probe), say so with `//gofastr:allow(GOFASTR1902) <why>`.",
		Doc: "access-control",
		Examples: []Example{{
			Bad:  `app.Delete("/admin/users/:id", deleteUser)`,
			Good: "admin := app.Group(\"/admin\", access.Require(\"users:delete\"))\nadmin.Delete(\"/users/:id\", deleteUser)",
		}},
	}, {
		ID: RuleAuthNotWired, Slug: "permissions/auth-not-wired",
		Title: "Auth configured but never mounted", Capability: CapPermissions, Severity: SeverityError,
		Summary: "`auth.New(...)` builds an auth manager, but nothing installs a middleware that reads the credential off the request.",
		Why: "The manager on its own authenticates nobody. Without `auth.SessionMiddleware` (cookie " +
			"sessions) or `auth.RequireAuth` (bearer tokens) in the chain, no request ever carries a " +
			"user, so every signed-in caller is treated as anonymous and gets 401, identically to a " +
			"real intruder. The app looks configured and the login form works; everything behind it is " +
			"unreachable. This shipped once from the blueprint generator, which enabled the battery and " +
			"never mounted the middleware. In a module with several binaries each is checked " +
			"separately, by import reachability: one app's mount says nothing about another app's " +
			"manager, which its binary never links.",
		Fix: "Add `fwApp.Use(auth.SessionMiddleware(authMgr))` for cookie sessions, or `auth.RequireAuth` on the routes that take bearer tokens. `auth.BFF` mounts the session middleware for you.",
		Doc: "auth",
		Examples: []Example{{
			Caption: "a manager nothing reads from",
			Bad:     "authMgr := auth.New(authCfg) // and no Use(auth.SessionMiddleware(authMgr)) anywhere",
			Good:    "authMgr := auth.New(authCfg)\nfwApp.Use(auth.SessionMiddleware(authMgr))",
		}},
	}}
}

func aiRules() []Rule {
	return []Rule{{
		ID: RuleHandrolledCRUD, Slug: "ai/handrolled-crud",
		Title: "Hand-rolled CRUD for a declared entity", Capability: CapAI, Severity: SeverityWarn,
		Summary: "Handlers implement list/get/create/update/delete for a table that already has an entity.",
		Why: "`app.Entity` already generates these, and generates more than the hand-written version " +
			"will: filtering, sorting, cursor pagination, includes, validation, hooks, owner scoping, " +
			"OpenAPI, an MCP tool surface, and the introspection the contract analyzers read. " +
			"Hand-rolled handlers get none of it and drift from the ones that do.",
		Fix: "Declare the entity and let auto-CRUD mount the routes. Keep hand-written handlers for the genuinely custom operations only.",
		Doc: "entity-declarations",
		Examples: []Example{{
			Bad:  "app.Get(\"/posts\", listPosts)\napp.Post(\"/posts\", createPost)\napp.Delete(\"/posts/:id\", deletePost)",
			Good: `app.Entity(posts) // mounts the full REST surface + OpenAPI + MCP`,
		}},
	}, {
		ID: RuleHandrolledBattery, Slug: "ai/handrolled-battery",
		Title: "Hand-rolled subsystem a battery provides", Capability: CapAI, Severity: SeverityWarn,
		Summary: "Code implements auth, email, queueing, storage, or scheduling directly.",
		Why: "These are the subsystems where a from-scratch version is 80% right and the missing 20% " +
			"is the security-relevant part: session rotation, retry backoff, signed URLs. The " +
			"batteries are wired into the app lifecycle, the audit log, and the test harness; a local one is not.",
		Fix: "Register the battery instead: `gofastr docs overview` lists them. Batteries are composable: keep your custom logic in a hook.",
		Doc: "overview",
		Examples: []Example{{
			Bad:  "hash, _ := bcrypt.GenerateFromPassword(pw, bcrypt.DefaultCost) // hand-rolled auth",
			Good: "app.Use(auth.New(auth.Config{})) // battery/auth",
		}},
	}, {
		ID: RuleRawSQLOverRepo, Slug: "ai/raw-sql-over-repository",
		Title: "Raw SQL against an entity's table", Capability: CapAI, Severity: SeverityInfo,
		Summary: "A raw query targets a table that has a declared entity and a generated repository.",
		Why: "Raw SQL bypasses every scoping rule the entity declares: soft delete, tenant filter, " +
			"owner field. A query written before those existed keeps returning rows they were added to hide.",
		Fix: "Use the entity's typed repository, which applies the scoping. Raw SQL is fine for reporting and migrations: annotate those `//gofastr:allow(GOFASTR2003) <why>`.",
		Doc: "query-dsl",
		Examples: []Example{{
			Bad:  "db.Query(\"SELECT * FROM invoices WHERE user_id = ?\", uid) // invoices is a declared entity",
			Good: "repo.List(ctx, filter) // the generated repository applies the scoping",
		}},
	}}
}
