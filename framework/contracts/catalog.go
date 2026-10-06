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
	RuleDuplicateRoute        = "GOFASTR1001"
	RuleColonPathParam        = "GOFASTR1002"
	RuleUntestedRoute         = "GOFASTR1003"
	RuleStateAsRoute          = "GOFASTR1004"
	RuleNonUppercaseVerb      = "GOFASTR1005"
	RulePrefixSegmentBoundary = "GOFASTR1006"
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
	RuleSQLStringConcat    = "GOFASTR1401"
	RuleFormWithoutCSRF    = "GOFASTR1402"
	RuleHTMLConcat         = "GOFASTR1403"
	RuleInsecureCookie     = "GOFASTR1404"
	RuleHardcodedSecret    = "GOFASTR1405"
	RuleForwardedProtoEnum = "GOFASTR1406"
	RuleRawJSONBodyDecode  = "GOFASTR1407"
	RuleAbsoluteAttempts   = "GOFASTR1408"
	RuleUnfencedClaim      = "GOFASTR1409"
	RuleFoldedKey          = "GOFASTR1410"
	RuleFetchMetadata      = "GOFASTR1411"
	RuleURLAttrEscape      = "GOFASTR1412"
	RuleVarySet            = "GOFASTR1413"
	RuleDialectDrift       = "GOFASTR1414"
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
	RuleFallbackDrift       = "GOFASTR1808"
	RuleOwnerlessStylesheet = "GOFASTR1809"
	RuleKitClassSelector    = "GOFASTR1810"
	RuleImportant           = "GOFASTR1811"
	RuleRawMediaWidth       = "GOFASTR1812"
	RuleAnimationNoReduced  = "GOFASTR1813"
	RuleStaleStyleSource    = "GOFASTR1814"
	RuleUpstreamCandidate   = "GOFASTR1815"
	RuleDuplicateStyleName  = "GOFASTR1816"
	RuleKitRootStyle        = "GOFASTR1817"
	RuleOwnedHandleLeak     = "GOFASTR1818"
	RuleAppSheetSelector    = "GOFASTR1819"
	RuleTokenCustomProperty = "GOFASTR1820"
	RuleDuplicateTokenValue = "GOFASTR1821"
	RuleRepeatedLiteral     = "GOFASTR1822"
	RuleBareThemeLiteral    = "GOFASTR1823"
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
	}, {
		ID: RulePrefixSegmentBoundary, Slug: "routing/prefix-without-segment-boundary",
		Title:      "Path prefix matched without a segment boundary",
		Capability: CapRouting, Severity: SeverityError,
		Summary: "`strings.HasPrefix` compares a path-like value against a prefix with no trailing `/` and no equality check.",
		Why: "A prefix with no boundary matches every longer sibling that starts with the same letters: " +
			"`cmd` matches `cmdline`, `kiln` matches `kiln2`. The stability gate classified whole trees " +
			"by accident this way (probe TestClassifyRequiresSegmentBoundary: a package the manifest never " +
			"named inherited a sibling's tier, so the add-it-to-the-manifest gate stayed quiet), and the " +
			"same shape matched a document-script scope past its `/`-terminated prefix in framework/uihost " +
			"earlier in v0.80. Whatever the match decides — a tier, a scope, an exemption — it decides it " +
			"for trees nobody named. The rule keys on path-NAMED haystacks: an identifier matching " +
			"path/route/url/uri/dir, exactly rel, or a .URL field. A literal with no `/` at all fires on " +
			"those haystacks (`HasPrefix(importPath, \"cmd\")` is the original bug with the manifest entry " +
			"spelled as a literal); key- and prefix-named haystacks (API keys, token types, versions) are " +
			"value spaces where every longer sibling is the intent, and a path held in a neutrally named " +
			"variable is out of reach for a parse-only pass.",
		Fix: `Compare equality for the exact match, then the boundary: rel == root || strings.HasPrefix(rel, root+"/").`,
		Doc: "project-structure",
		Examples: []Example{{
			Bad:  `if strings.HasPrefix(rel, r.prefix) { tier = r.tier } // "cmd" also classifies cmdline`,
			Good: `if rel == root || strings.HasPrefix(rel, root+"/") { tier = r.tier }`,
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
	}, {
		ID: RuleForwardedProtoEnum, Slug: "security/forwarded-proto-without-enum",
		Title:      "`X-Forwarded-Proto` spliced into a URL without an http/https check",
		Capability: CapSecurity, Severity: SeverityError,
		Summary: "The `X-Forwarded-Proto` header value is used as a URL scheme, and the function never compares it against `\"http\"`/`\"https\"`.",
		Why: "The header is plain request input: any client sets it to anything. Spliced unchecked into an " +
			"origin, `https://evil.example/p` does not produce a wrong scheme, it produces the attacker's " +
			"whole origin — reflected into the `Link: rel=\"service\"` header, sitemap entries, and the " +
			"agent card's service URL, all of them cacheable (probe TestDiscoveryURLsIgnoreForwardedProto, " +
			"fixed in framework/uihost/agentready.go). Multi-hop proxy chains send compound values like " +
			"`https,http`, which is not a scheme at all. `Vary` narrows who receives the poisoned entry; " +
			"it does not clean the value.",
		Fix: `Honour the two legal values only: if v := r.Header.Get("X-Forwarded-Proto"); v == "http" || v == "https" { scheme = v } — the enum framework/pluginhost/assets.go and framework/uihost already apply.`,
		Doc: "security",
		Examples: []Example{{
			Bad:  "if u := req.Header.Get(\"X-Forwarded-Proto\"); u != \"\" {\n\tscheme = u\n}\nreturn scheme + \"://\" + req.Host",
			Good: "if u := req.Header.Get(\"X-Forwarded-Proto\"); u == \"http\" || u == \"https\" {\n\tscheme = u\n}\nreturn scheme + \"://\" + req.Host",
		}},
	}, {
		ID: RuleRawJSONBodyDecode, Slug: "security/raw-json-body-decode",
		Title:      "Request body decoded with `encoding/json` outside the strict binder",
		Capability: CapSecurity, Severity: SeverityError,
		Summary: "`json.NewDecoder`/`json.Unmarshal` on client-controlled bytes — a request body, a websocket frame, or bytes a same-package helper read from either and handed on — outside `core/handler`.",
		Why: "stdlib `encoding/json` keeps the LAST duplicate key and matches key names " +
			"case-insensitively, while form parsing keeps the FIRST duplicate — so one smuggled body " +
			"resolves to a different identity depending on Content-Type (probe " +
			"TestLoginJSONStrictTopLevelKeys: `{\"email\":A,\"EMAIL\":B}` authenticated B on the JSON " +
			"surface and A on the form surface of the same endpoint). The ambiguity itself is the " +
			"attack; which parser wins is an accident. `core/handler.Bind` refuses ambiguous bodies, " +
			"which is why everything else should go through it.",
		Fix: "Decode with `handler.Bind`, or `handler.DecodeStrict` / `handler.UnmarshalStrict` for envelopes, frames, and buffered bodies (`handler.CheckTopLevelKeys` when the caller normalises keys itself). A site that must decode raw carries `//gofastr:allow(GOFASTR1407) <why>` with a real reason.",
		Doc: "security",
		Examples: []Example{{
			Bad:  "creds := struct {\n\tEmail    string `json:\"email\"`\n\tPassword string `json:\"password\"`\n}{}\njson.NewDecoder(req.Body).Decode(&creds)",
			Good: "creds := Credentials{}\nhandler.Bind(req, &creds) // refuses duplicate and case-folded top-level keys\n\nvar env rpcEnvelope\nhandler.DecodeStrict(http.MaxBytesReader(w, req.Body, 1<<20), &env)",
		}},
	}, {
		ID: RuleAbsoluteAttempts, Slug: "security/absolute-attempts-counter",
		Title:      "Retry counter written from a host value, not `attempts = attempts + 1`",
		Capability: CapSecurity, Severity: SeverityError,
		Summary: "An UPDATE's SET assigns the attempts/retries/tries/failures counter from a placeholder (`$n`, `?`, `%s`) instead of the column's relative form.",
		Why: "The host value is a count computed in Go from a row read before the write, and the write is not the only writer: a lease-expiry " +
			"re-claimant and the stale worker it replaced both read N and both write N+1, and a crash between claim and settle writes nothing " +
			"at all — so the row's attempt budget under-counts and a poison delivery re-runs past it (2026-09-04 red probes " +
			"TestFailureSettleAttemptsMonotonic in framework/outbox, TestSettleAttemptsCountEveryPost and " +
			"TestClaimConsumesAttemptCrashLoop in battery/webhook). The database is the only arbiter of how many claims happened; " +
			"arithmetic done beside it is a stale snapshot.",
		Fix: "Move the increment into the claim UPDATE as `attempts = attempts + 1` (the battery/queue Dequeue spelling), so each claim itself consumes the attempt, and let settle write state only (status, error, next_attempt_at) — never the count. A reset to a literal `attempts = 0` guarded by a terminal status is fine.",
		Doc: "security",
		Examples: []Example{{
			Bad:  "db.Exec(`UPDATE webhook_deliveries SET attempts = $1, status = $2 WHERE id = $3`, d.Attempts+1, status, d.ID)",
			Good: "db.Exec(`UPDATE webhook_deliveries SET attempts = attempts + 1 WHERE id IN (SELECT id FROM webhook_deliveries WHERE status='pending' AND next_attempt_at <= $1 LIMIT $2)`, now, limit)",
		}},
	}, {
		ID: RuleUnfencedClaim, Slug: "security/unfenced-claim-write",
		Title:      "Claim-state write on a token-fenced queue table with no fence of its own",
		Capability: CapSecurity, Severity: SeverityError,
		Summary: "In a file whose other UPDATE/DELETE on the same table matches `claim_token = $n`, another UPDATE/DELETE touching claim state (claimed_at, a token column, attempts, or `status='claimed'`) carries no token predicate, no claimed_at staleness bound, and no terminal status guard.",
		Why: "The claim's lease-expiry clause deliberately re-claims rows whose worker may still be running, so a stale claimant is a designed " +
			"state — and its unfenced write by bare row id mutates the re-claimant's live row (probe TestDBReleaseCannotTouchReclaimant, " +
			"2026-09-04 red round: battery/queue's v0.66 Ack/Nack fencing never reached release, so a stale worker released the " +
			"re-claimant's claimed job back to pending and a third worker ran the handler concurrently). The token the claim mints is " +
			"the only thing that distinguishes my claim from a claim on this row.",
		Fix: "Fence the write the way Ack and Nack do: `WHERE id = $1 AND claim_token = $2`. The claim UPDATE itself needs no fence (its SET mints the token); a `claimed_at <= $n` staleness bound or a terminal `status='failed'` guard also proves the write cannot touch a live claim.",
		Doc: "security",
		Examples: []Example{{
			Bad:  "db.Exec(`DELETE FROM jobs WHERE id = $1 AND status='claimed' AND claim_token = $2`, id, tok)\ndb.Exec(`UPDATE jobs SET status='pending', attempts = attempts - 1 WHERE id = $1`, id)",
			Good: "db.Exec(`DELETE FROM jobs WHERE id = $1 AND status='claimed' AND claim_token = $2`, id, tok)\ndb.Exec(`UPDATE jobs SET status='pending', attempts = attempts - 1 WHERE id = $1 AND claim_token = $2`, id, tok)",
		}},
	}, {
		ID: RuleFetchMetadata, Slug: "security/fetch-metadata-copy",
		Title:      "A private copy of the Sec-Fetch-Site cross-site predicate",
		Capability: CapSecurity, Severity: SeverityError,
		Summary: "A file outside `core/handler` reads the `Sec-Fetch-Site` header itself (`Header.Get(\"Sec-Fetch-Site\")` or the canonical-key map form).",
		Why:     "The cross-site predicate is security-critical and exists in exactly one place, `core/handler.IsCrossSiteRequest` (Sec-Fetch-Site first, the Origin-host comparison as the fallback). Every private copy so far has diverged: the setup and kiln copies early-allowed `same-site` — and a sibling subdomain IS same-site, so the Strict cookie rides the attack while only the Origin compare can refuse it — while other copies disagreed about which values are trusted. A tenth copy is a tenth divergence waiting for its own probe.",
		Fix:     "Call `handler.IsCrossSiteRequest(r)` and keep only the response shape local. A transport that genuinely must read the header itself carries `//gofastr:allow(GOFASTR1411) <why>`.",
		Doc:     "security",
		Examples: []Example{{
			Bad:  "if sfs := r.Header.Get(\"Sec-Fetch-Site\"); sfs != \"\" && sfs != \"same-origin\" {\n\thttp.Error(w, \"forbidden\", http.StatusForbidden)\n}",
			Good: "if !handler.IsCrossSiteRequest(r) {\n\thttp.Error(w, \"forbidden\", http.StatusForbidden)\n}",
		}},
	}, {
		ID: RuleURLAttrEscape, Slug: "security/url-attr-html-escape",
		Title:      "HTML-escaped value in a URL attribute slot",
		Capability: CapSecurity, Severity: SeverityError,
		Summary: "`render.Escape`/`html.EscapeString` feeds an `href`/`src`/`action`/`formaction`/`poster`/`data` attribute.",
		Why:     "HTML escaping is scheme-blind: `javascript:alert(1)` escapes to itself, so the escaped value becomes a live URL the moment a user clicks the link, the form submits, or the poster loads (2026-09-06/07 probes: battery/print renderShell's stylesheet href and auto-print script src, core-ui infinitescroll's noscript form action, framework/ui menu.go's MenuAction Path). Escaping proves the value cannot break OUT of the attribute; it says nothing about what the attribute then executes.",
		Fix:     "Run the value through the scheme allow-list first — `core/urlsafe.Clean(v, urlsafe.Anchor)` or `urlsafe.CleanAnchor(v)` — and escape the cleaned result, the spelling menu.go's Href branch and `core-ui/html` setURLAttr already use. A rejected value degrades to an inert `\"#\"`.",
		Doc:     "security",
		Examples: []Example{{
			Bad:  "fmt.Fprintf(w, \"<a href=\\\"%s\\\">open</a>\", render.Escape(user.URL))",
			Good: "fmt.Fprintf(w, \"<a href=\\\"%s\\\">open</a>\", urlsafe.CleanAnchor(user.URL))",
		}},
	}, {
		ID: RuleVarySet, Slug: "security/vary-set-overwrites",
		Title:      "`Vary` written with Set in a middleware chain",
		Capability: CapSecurity, Severity: SeverityError,
		Summary: "`Header().Set(\"Vary\", …)` replaces the Vary entries earlier middlewares added.",
		Why:     "Vary is append-only in a middleware chain: a response crosses CORS (which Adds `Vary: Origin`), an idempotency layer, a cache layer, and a later Set replaces every earlier entry — a shared cache then serves one principal's variant to another. The 2026-09-07 probe TestIdempotencyVaryEatsCors pinned it live: CORS(...)(Idempotency(...)) on an over-cap POST produced ACAO with Vary listing ONLY Idempotency-Key.",
		Fix:     "Use `.Add(\"Vary\", …)`, the spelling core/middleware/cors.go, wellknown.go, embed, uihost, and the auth BFF all already use. Set is for headers one layer owns end to end; Vary is never one of them.",
		Doc:     "security",
		Examples: []Example{{
			Bad:  "w.Header().Set(\"Vary\", \"Idempotency-Key\")",
			Good: "w.Header().Add(\"Vary\", \"Idempotency-Key\")",
		}},
	}, {
		ID: RuleDialectDrift, Slug: "security/dialect-twin-where-drift",
		Title:      "Dialect twin queries whose WHERE predicates diverge",
		Capability: CapSecurity, Severity: SeverityError,
		Summary: "Sibling Postgres/SQLite queries — paired by name (claimDeliveriesPostgres/claimDeliveriesSQLite) or by a dialect if/switch arm — whose WHERE clauses differ by a predicate atom after normalization, and whose table sets match: a pair over different tables (pg_tables vs sqlite_master) is shaped by different catalogs and owes no predicate parity, so it stays quiet.",
		Why:     "A dialect twin is written twice because the SPELLING differs ($n vs ?, SKIP LOCKED vs a tx); the predicates are supposed to be copies. Nothing else reviews the two against each other, so an atom one side lacks ships silently and the two dialects select different rows: framework/outbox claimDeliveriesSQLite shipped without the `next_attempt_at IS NULL OR next_attempt_at <= $` backoff its Postgres twin has, so deliveries past their retry deadline were re-claimed on SQLite only and a poison delivery looped past its backoff on every SQLite app. The rule compares only twins over the same table set (a subset counts — the Postgres side may wrap its twin's SELECT in an UPDATE on the same table); twins over different system catalogs are quiet by design.",
		Fix:     "Decide which side is right and copy the predicate across verbatim. When the difference is a real dialect capability (a partial index, a RETURNING clause, a different system catalog), annotate the poorer statement `//gofastr:allow(GOFASTR1414) <why>` naming the capability.",
		Doc:     "security",
		Examples: []Example{{
			Bad:  "func claimPostgres(db *sql.DB, now string) error {\n\t_, err := db.Exec(`UPDATE jobs SET status='claimed'\n\t\tWHERE status='pending' AND claimed_until <= $1 AND next_attempt_at <= $1`, now)\n\treturn err\n}\n\nfunc claimSQLite(db *sql.DB, now string) error {\n\t_, err := db.Exec(`UPDATE jobs SET status='claimed'\n\t\tWHERE status='pending' AND claimed_until <= ?`, now)\n\treturn err\n}",
			Good: "func claimPostgres(db *sql.DB, now string) error {\n\t_, err := db.Exec(`UPDATE jobs SET status='claimed'\n\t\tWHERE status='pending' AND claimed_until <= $1 AND next_attempt_at <= $1`, now)\n\treturn err\n}\n\nfunc claimSQLite(db *sql.DB, now string) error {\n\t_, err := db.Exec(`UPDATE jobs SET status='claimed'\n\t\tWHERE status='pending' AND claimed_until <= ? AND next_attempt_at <= ?`, now, now)\n\treturn err\n}",
		}},
	}, {
		ID: RuleFoldedKey, Slug: "security/folded-key-save",
		Title:      "Local-filesystem Save with no folded-key refusal",
		Capability: CapSecurity, Severity: SeverityError,
		Summary: "A Save/Put/Write naming its stored object (a key/name parameter) on a type with a BaseDir/Root/Dir field reaches os.OpenFile/os.Create/os.Rename with no call whose name carries Fold in the file.",
		Why:     "On a case-insensitive or Unicode-normalization-insensitive filesystem (macOS APFS, most CIFS mounts), `tenanta/report.txt` and `TenantA/report.txt` are one file: the second save silently overwrites the first and each key's read returns the other writer's bytes (2026-09-05 red probe TestFoldedKeysDoNotAlias: battery/storage's local backend refuses the fold, core/upload's Save does not). A lexical key check cannot see the fold — only the filesystem's own resolution can.",
		Fix:     "Refuse the folded key before anything is created or written, the battery/storage spelling: walk every path component, Lstat it as spelled, and os.SameFile-match the entry the parent directory actually holds; a byte-different name that matches is a fold, refuse it with ErrInvalidKey.",
		Doc:     "security",
		Examples: []Example{{
			Bad:  "type LocalStorage struct{ baseDir string }\n\nfunc (s *LocalStorage) Save(key string, r io.Reader) error {\n\tdst := filepath.Join(s.baseDir, key)\n\ttmp, _ := os.CreateTemp(filepath.Dir(dst), \".tmp-*\")\n\treturn os.Rename(tmp.Name(), dst)\n}",
			Good: "func (ls *LocalStorage) Save(key string, r io.Reader) error {\n\tif err := ls.refuseFoldedKey(key, dstPath, root, ls.rootFor(root)); err != nil {\n\t\treturn err // key collides with an object stored under a different spelling\n\t}\n\treturn ls.finishSave(key, r)\n}",
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
		Summary: "An app or generator ships its own CSS rules in Go strings or a stylesheet.",
		Why: "Two styling surfaces means every future change has to be made twice and stays consistent " +
			"by luck. Bespoke CSS also loads in an order you do not control relative to component CSS, " +
			"so it wins or loses by specificity accident rather than by intent. The rule matches CSS " +
			"declarations, a property-colon-value shape in a string; a Go assignment of a design-system " +
			"token reference to a variable, `fill := \"var(--color-surface)\"`, is not one and does not fire. " +
			"A stylesheet FILE is never this rule's finding: a .css file with no owner is GOFASTR1809, and a *.style.css is an owned style, " +
			"checked by GOFASTR1806–1820. When both rules would describe one stylesheet, 1809 is the one reported.",
		Fix: "Compose `framework/ui` components and `core-ui/style` tokens. Add missing components or tokens upstream. CSS the kit does not cover gets an owner: a <name>.style.css file beside the layout, screen or component (`gofastr gen styles` generates its typed Go).",
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
		Fix: "Subscribe through the runtime's bus. For passive freshness (dashboards, counters, statuses) use `data-cui-poll` instead: no held connection at all.",
		Doc: "reactivity",
		Examples: []Example{{
			Bad:  `new EventSource('/my-feed')`,
			Good: `<div data-cui-poll="5s" data-cui-poll-src="/islands/orders/count">`,
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
		Summary: "Project CSS reads `var(--name)` where `name` is not a theme token: neither built in nor declared in one of the app's <name>.tokens.css files.",
		Why: "An invalid var() is not a CSS error: it resolves to nothing and the declaration is " +
			"silently dropped, so a typo is invisible to the build, the browser console, and every " +
			"linter, and the only symptom is the styling not applying. Issue #214's reporter wrote " +
			"`--radius-lg` where the theme emits `--radii-lg`, and every rounded corner on the site " +
			"rendered square for days.",
		Fix: "Spell the token the theme emits (see `style.TokenNames()` or `gofastr docs theming`). A value of the app's own is a token: declare it with @property in a <name>.tokens.css and run `gofastr gen styles`. In an owned style (*.style.css) a fallback does not waive the rule, since `var(--typo, 8px)` paints the fallback forever; a name set from outside the app (an embedding page) takes `/* gofastr:allow(GOFASTR1806) <reason> */` on the line above.",
		Doc: "theming",
		Examples: []Example{{
			Bad:  "border-radius: var(--radius-lg);",
			Good: "border-radius: var(--radii-lg);",
		}},
	}, {
		ID: RuleHardcodedTokenValue, Slug: "rendering/hardcoded-token-value",
		Title: "CSS hardcodes a value the theme declares as a token", Capability: CapRendering, Severity: SeverityError,
		Summary: "Design-system CSS or an owned style (*.style.css) sets a property to a literal that is exactly a theme token's value, built in or declared in a <name>.tokens.css.",
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
	}, {
		ID: RuleFallbackDrift, Slug: "rendering/fallback-drift",
		Title: "var() fallback disagrees with the token it stands in for", Capability: CapRendering, Severity: SeverityError,
		Summary: "Design-system CSS or an owned style (*.style.css) writes `var(--spacing-md, 12px)` where the theme declares --spacing-md as 8px.",
		Why: "A themed page resolves the variable, so the fallback never renders there; it only shows on a page " +
			"with no theme CSS. But the number is what the next reader learns the token means. Issue #365 " +
			"found about 450 such fallbacks teaching a 4/8/16/24/32 spacing ladder the theme does not " +
			"declare (it is 2/4/8/16/24), and a review bot read one of them as the token's value and " +
			"proposed a change that would have shrunk a real gap. The rule covers the length and time " +
			"scales, spacing, radii, text and duration, where a fallback can only be right or wrong; colour " +
			"and font fallbacks are left alone because `currentColor`, `inherit`, `transparent` and a " +
			"dark-surface hex are deliberate degraded-mode choices, not restatements of the token.",
		Fix: "Write the declared value as the fallback (`style.TokenNames()` or `gofastr docs theming` lists them); rem and px are compared by size, so `var(--spacing-lg, 1rem)` is fine for a 16px token. If the value you wanted is not the token's value, you wanted a different token.",
		Doc: "theming",
		Examples: []Example{{
			Bad:  "padding: var(--spacing-md, 12px);",
			Good: "padding: var(--spacing-md, 8px);",
		}},
	}, {
		ID: RuleOwnerlessStylesheet, Slug: "rendering/ownerless-stylesheet",
		Title: "Stylesheet with no owner", Capability: CapRendering, Severity: SeverityError,
		Summary: "A .css file in an app tree is not a <name>.style.css, so no layout, screen, component or the app owns it.",
		Why: "A loose stylesheet is unscoped and unchecked: its rules reach every page, it loads in an order nothing controls " +
			"relative to component CSS, and none of the owned-style checks (theme tokens, kit classes, !important, breakpoints) " +
			"run on it. An owned style compiles into an @scope bound to its owner's root, so its rules stop at that element and " +
			"its class names are typed Go. This rule replaces GOFASTR1801 for stylesheet files: a .css file is reported under " +
			"1809, never under 1801 as well. Skipped: *.style.css files, sheets whose declarations only assign custom properties " +
			"(theme knobs such as --ui-*), files under testdata/, and the design-system trees.",
		Fix: "Rename the file to <name>.style.css beside the layout, screen or component that owns it (app.style.css for page-wide " +
			"classes), run `gofastr gen styles`, and attach the handle: LayoutSpec{Style: x.Style}, Screen.WithStyle(x.Style), " +
			"x.Style.Scope(root) or App.WithStyle(appstyle.Style). A frontend that does not use GoFastr UI can state the exception " +
			"in a CSS comment before the first rule, /* gofastr:allow(GOFASTR1809) <why> */, or with allow-file.",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  "/* static/board.css */\n.column { display: grid; gap: var(--spacing-sm); }",
			Good: "/* board/board.style.css, attached with Screen.WithStyle(board.Style) */\n.column { display: grid; gap: var(--spacing-sm); }",
		}},
	}, {
		ID: RuleKitClassSelector, Slug: "rendering/kit-class-selector",
		Title: "Owned style selects a kit class or runtime attribute", Capability: CapRendering, Severity: SeverityError,
		Summary: "A selector in a *.style.css names a kit class (.fui-*) or a framework attribute ([data-cui-*], [data-fui-*], [data-hui-*]).",
		Why: "Kit classes belong to the kit; data-cui-*, data-fui-* and data-hui-* attributes belong to the runtime, the framework modules and the headless layer. They change without notice, and an owner " +
			"that selects them restyles the inside of a component it does not maintain. The compiled @scope already stops at kit " +
			"internals ([data-cui-internal]), so such a selector either matches nothing or reaches past the boundary on the kit " +
			"root, and either way the next kit release breaks it silently.",
		Fix: "Style the content you pass into the component's slots, use the component's config, or add the option upstream in framework/ui.",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  ".column .fui-card-header { color: var(--color-primary); }",
			Good: ".column-title { color: var(--color-primary); } /* on the heading passed into the card's slot */",
		}},
	}, {
		ID: RuleImportant, Slug: "rendering/important",
		Title: "!important in an owned style", Capability: CapRendering, Severity: SeverityError,
		Summary: "A declaration in a *.style.css carries !important.",
		Why: "Owned rules already win ties against kit rules by scope proximity (CSS Cascade 6 compares it before order of " +
			"appearance), so !important is never needed to beat the kit. What it does beat is every later fix: a theme change, " +
			"a component variant, a responsive override, and another owner's rule nested inside this one.",
		Fix: "Delete it. If the rule still loses, raise the specificity inside your own scope, or give the component the option upstream.",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  ".key { color: var(--color-text-muted) !important; }",
			Good: ".key { color: var(--color-text-muted); }",
		}},
	}, {
		ID: RuleRawMediaWidth, Slug: "rendering/raw-media-width",
		Title: "@media width that is not a theme breakpoint", Capability: CapRendering, Severity: SeverityError,
		Summary: "An @media query in a *.style.css tests min-width, max-width or width against a raw length instead of a custom media name.",
		Why: "Breakpoints are theme tokens. A raw 768px drifts from the theme the day the breakpoint moves, and two sheets " +
			"writing 767px and 768px disagree about which layout a window between them gets. var() is invalid in a media query, " +
			"so the owned-style compiler expands custom media names (--above-md, --below-lg) against the running theme's " +
			"breakpoints instead. Raw widths stay legal in @container, which measures a container, not the viewport.",
		Fix: "Write @media (--above-md) or (--below-lg); combine a custom medium with other features using `and`, e.g. (--above-md) and (hover: hover).",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  "@media (min-width: 768px) { .board { grid-template-columns: 1fr 1fr; } }",
			Good: "@media (--above-md) { .board { grid-template-columns: 1fr 1fr; } }",
		}},
	}, {
		ID: RuleAnimationNoReduced, Slug: "rendering/animation-without-reduced-motion",
		Title: "Animation with no reduced-motion block", Capability: CapRendering, Severity: SeverityWarn,
		Summary: "A rule in a *.style.css sets animation or animation-name, and no @media (--reduced-motion) block holds a rule for the same selector.",
		Why: "Motion can trigger nausea and vestibular symptoms, and WCAG 2.3.3 asks that non-essential animation can be turned " +
			"off. The owned-style compiler expands (--reduced-motion) to prefers-reduced-motion: reduce; the check looks for a " +
			"rule with the same selector inside such a block. It is a warning because some animation is essential (a progress " +
			"indicator), which the check cannot tell.",
		Fix: "Add @media (--reduced-motion) { <the same selector> { animation: none; } }, or a slower, non-moving alternative.",
		Doc: "accessibility",
		Examples: []Example{{
			Bad:  ".pulse { animation: pulse 1s infinite; }",
			Good: ".pulse { animation: pulse 1s infinite; }\n@media (--reduced-motion) { .pulse { animation: none; } }",
		}},
	}, {
		ID: RuleStaleStyleSource, Slug: "rendering/stale-style-source",
		Title: "Owned style CSS changed since its Go was generated", Capability: CapRendering, Severity: SeverityError,
		Summary: "A *.style.css has no sibling <name>_style.gen.go (a *.tokens.css no <name>_tokens.gen.go), or the sibling's Source hash does not match the CSS bytes.",
		Why: "The generated file is the only thing connecting a hand-written stylesheet to the type-checked class vocabulary: " +
			"the CSS constant, the ownstyle.Must registration and every class method are frozen at generate time. Edit the " +
			"CSS without regenerating and the Go silently serves the OLD bytes — the class methods keep naming classes the " +
			"sheet no longer declares, and the scope still loads the stale CSS. Nothing else can see the drift: the CSS is " +
			"valid CSS and the Go is valid Go.",
		Fix: "Run `gofastr gen styles` (regenerates every <name>_style.gen.go and <name>_tokens.gen.go whose CSS changed, and writes .gofastr/tokens.css for editor completion). Never hand-edit a generated file.",
		Doc: "cli",
		Examples: []Example{{
			Bad:  "/* board.style.css */\n:scope { display: grid; }\n/* edited after board_style.gen.go was generated;\n   its Source hash line no longer matches these bytes */",
			Good: "gofastr gen styles   # rewrites board_style.gen.go with the current Source hash",
		}},
	}, {
		ID: RuleUpstreamCandidate, Slug: "rendering/upstream-candidate",
		Title: "Owned style listed as an upstream candidate", Capability: CapRendering, Severity: SeverityInfo,
		Summary: "Every *.style.css is listed with its owner name and the number of classes it declares.",
		Why: "An owned style is CSS the kit does not cover. Some of it is specific to one page; some of it is a component or an " +
			"option every app will want. Listing each sheet on every run keeps that decision in view instead of letting owned " +
			"sheets pile up unreviewed: a sheet whose classes read like a card variant or a layout primitive belongs in " +
			"framework/ui, where every app inherits it.",
		Fix: "Nothing, when the style is specific to this app. When it is not, add the component, option or token upstream " +
			"(framework/ui, core-ui/style) and delete the sheet.",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  "/* board.style.css: a stack with a gap, which the kit already has */\n.lane { display: flex; flex-direction: column; gap: var(--spacing-sm); }",
			Good: "ui.Stack(ui.StackConfig{Gap: ui.GapSM}, cards...)",
		}},
	}, {
		ID: RuleDuplicateStyleName, Slug: "rendering/duplicate-style-name",
		Title: "Two owned styles share a name", Capability: CapRendering, Severity: SeverityError,
		Summary: "Two *.style.css files with the same file stem sit in one program: a main package plus the " +
			"packages its imports resolve to — build constraints honoured per target platform (darwin/linux/windows, " +
			"amd64/arm64), packages under a nested go.mod importing under the nested module's path. Style files " +
			"no program reaches (library packages meant to be composed into one app) form one program together.",
		Why: "The owner name is the file stem, and it is the registry key, the /__gofastr/comp/<name>.css URL and the " +
			"data-cui-scope value. Two sheets with one name cannot both register: the second ownstyle.Must panics at init, " +
			"so the program does not start. `gofastr gen styles` refuses both files for the same reason — it judges the " +
			"same programs `gofastr verify` does. Sheets are checked per program, so two binaries that each carry their " +
			"own copy of a siteheader package do not collide, and a main whose platform-specific files each import their " +
			"own card package is no duplicate: no one build links both. One gap remains: a build tag that names no " +
			"platform (a project's own `extra`) is treated as set, so the imports of a `//go:build !extra` file are " +
			"never followed.",
		Fix: "Rename one of the files (board-card.style.css, review-card.style.css) and run `gofastr gen styles`.",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  "board/card.style.css\nreview/card.style.css",
			Good: "board/board-card.style.css\nreview/review-card.style.css",
		}},
	}, {
		ID: RuleKitRootStyle, Slug: "rendering/kit-root-restyled",
		Title: "Owned style restyles a kit component's root", Capability: CapRendering, Severity: SeverityError,
		Summary: "A class passed to a framework/ui component's Class field, or the :scope of a sheet scoped onto a ui.X(…) root, has a rule setting a property that is not a placement property.",
		Why: "Kit roots stay in reach of an owned style so an owner can place them. Placement is all an owner may do there: " +
			"a colour, padding, border or custom property on the root restyles the component, and the next kit release that " +
			"changes its internals breaks the override with no compile error. The rule reads the Go to learn which class lands " +
			"on a kit root (a Class field in a ui.XConfig literal holding a handle method call such as Style.Column() or " +
			"Style.ColumnWith(…), or Style.Scope(ui.X(…)) for the sheet's :scope), then checks that class's rules in the " +
			"sheet. The handle must be named directly (Style, <Owner>Style, or pkg.Style); a handle copied into a local " +
			"variable first is not traced.",
		Fix: "Keep only placement properties on the root: grid-area, grid-column*, grid-row*, margin*, align-self, justify-self, " +
			"place-self, order, flex, flex-grow, flex-shrink, flex-basis, width, height, inline-size, block-size and their " +
			"min-/max- forms, display, position, inset*, top, right, bottom, left, z-index, visibility. Put the rest on the " +
			"content you pass into the component's slots, use the component's config, or add the option upstream.",
		Doc: "contracts",
		Examples: []Example{{
			Bad:  "/* board.style.css */ .column { grid-column: span 2; padding: var(--spacing-md); }\n// view.go\nui.Card(ui.CardConfig{Class: board.Style.Column()})",
			Good: "/* board.style.css */ .column { grid-column: span 2; }\n// view.go\nui.Card(ui.CardConfig{Class: board.Style.Column(), Padding: style.SpaceMD})",
		}},
	}, {
		ID: RuleOwnedHandleLeak, Slug: "rendering/owned-style-outside-owner",
		Title: "Screen or layout style used outside its owner's package", Capability: CapRendering, Severity: SeverityError,
		Summary: "A style handle attached with LayoutSpec.Style or Screen.WithStyle is used (its class methods called) in a package that is neither the handle's own nor the one that attaches it.",
		Why: "A layout's or screen's style compiles into an @scope bound to that owner's root, so its class names mean something " +
			"only inside that element. Called from another package (a shared component, another screen), a method returns a " +
			"class that matches nothing when the markup renders outside the owner, or matches by accident when it happens to " +
			"land inside, so the styling depends on where the caller is rendered today. The handle must be named directly " +
			"(Style, <Owner>Style, or pkg.Style) for the rule to see it; a handle copied into a variable is not traced.",
		Fix: "Keep a screen's or layout's classes in the style's own package (where its .style.css sits) or the one that attaches it. Markup another package renders is a " +
			"component: give it its own <name>.style.css and scope its root with Style.Scope.",
		Doc: "layouts",
		Examples: []Example{{
			Bad:  "// package widgets; board.Style is attached by Screen.WithStyle in package board\nfunc lane() string { return board.Style.Column() }",
			Good: "// package widgets, with its own widgets/lane.style.css\nfunc lane(content render.HTML) render.HTML { return Style.Scope(content) }",
		}},
	}, {
		ID: RuleAppSheetSelector, Slug: "rendering/app-sheet-non-class",
		Title: "App sheet selects something other than a class", Capability: CapRendering, Severity: SeverityError,
		Summary: "app.style.css has a selector whose subject is not a class, or declares a custom property.",
		Why: "app.style.css covers every page. An element or attribute subject there (h2, a, [type=text]) restyles every " +
			"instance on every page, the kit's own markup included: page-wide element defaults, which are the theme's job, where " +
			"one token change reaches everything. A custom property declared by the app sheet is a new token that bypasses the " +
			"theme, so a theme swap cannot reach it.",
		Fix: "Select classes only (.lede, .figure) and apply them where you want them. Element defaults belong in the theme; a new value is a new theme token.",
		Doc: "theming",
		Examples: []Example{{
			Bad:  "/* app.style.css */\nh2 { letter-spacing: -0.01em; }",
			Good: "/* app.style.css */\n.section-title { letter-spacing: -0.01em; }",
		}},
	}, {
		ID: RuleTokenCustomProperty, Slug: "rendering/theme-token-redeclared",
		Title: "Owned style redeclares a theme token", Capability: CapRendering, Severity: SeverityError,
		Summary: "A *.style.css declares a custom property whose name is a theme token (--color-primary, --spacing-md, …).",
		Why: "Theme tokens are what a theme swap changes. An owned sheet declaring --color-primary forks the token for " +
			"everything under its root: dark mode, a tenant theme and the next rebrand stop reaching that subtree, and every " +
			"var(--color-primary) read inside it looks like a theme read while it is not one.",
		Fix: "Declare a name of your own for a value that is yours (--board-lane-width) and read theme tokens directly where you need them. If the value should change with the theme, add a theme token.",
		Doc: "theming",
		Examples: []Example{{
			Bad:  ":scope { --color-primary: #0F766E; }",
			Good: ":scope { --board-accent: var(--color-primary); }",
		}},
	}, {
		ID: RuleDuplicateTokenValue, Slug: "rendering/duplicate-token-value",
		Title: "App token repeats another token's value", Capability: CapRendering, Severity: SeverityError,
		Summary: "A <name>.tokens.css declares a token whose value is already another token's of the same type, built in or the app's own.",
		Why: "Two names for one value look like two decisions and are one. The first time someone changes --color-primary, " +
			"--color-brand keeps the old value and the page has two blues where it had one, with nothing to say they were " +
			"meant to match. A token earns its name by being a value nothing else holds.",
		Fix: "Read the existing token where you meant the same value (`var(--color-primary)`), or give the new token a value of its own. When the match is a coincidence you have checked, put `/* gofastr:allow(GOFASTR1821) <reason> */` above the @property rule.",
		Doc: "theming",
		Examples: []Example{{
			Bad:  "@property --color-brand { syntax: \"<color>\"; inherits: true; initial-value: #4F46E5; }  /* --color-primary's value */",
			Good: "@property --color-brand { syntax: \"<color>\"; inherits: true; initial-value: #0F766E; }",
		}},
	}, {
		ID: RuleRepeatedLiteral, Slug: "rendering/repeated-literal",
		Title: "One literal written in several owned styles", Capability: CapRendering, Severity: SeverityWarn,
		Summary: "The same literal value appears in two or more *.style.css files of one program — the same grouping " +
			"GOFASTR1816 judges (per target platform, build constraints honoured; style files no program reaches form one " +
			"group together) — for properties of one token type " +
			"(a colour, a size, a spacing, …). Zeros, 100% and a z-index of -1 pass: they say none, fill or behind, not a size.",
		Why: "A value written in two sheets is a design decision with no name. The two copies drift the first time one sheet " +
			"is edited, and a theme swap reaches neither. The second copy is the moment a token was missing. Sheets are " +
			"checked per program, so a literal repeated only across two binaries that never link is not a finding.",
		Fix: "Declare the value once as a token in a <name>.tokens.css (`@property --size-reading-width { syntax: \"<length>\"; inherits: true; initial-value: 37rem; }`), run `gofastr gen styles`, and read `var(--size-reading-width)` in each sheet.",
		Doc: "theming",
		Examples: []Example{{
			Bad:  "/* article.style.css */ .body { max-width: 37rem; }\n/* help.style.css */ .answer { max-width: 37rem; }",
			Good: "/* article.style.css */ .body { max-width: var(--size-reading-width); }\n/* help.style.css */ .answer { max-width: var(--size-reading-width); }",
		}},
	}, {
		ID: RuleBareThemeLiteral, Slug: "rendering/bare-theme-literal",
		Title: "A look value written as a literal in kit CSS, where no theme can reach it", Capability: CapRendering, Severity: SeverityError,
		Summary: "Design-system CSS writes a value a theme owns as a bare literal: a border, outline or inset-ring width, " +
			"an outline offset, a border radius, a transition or animation duration up to 500ms, a z-index above 10, a " +
			"padding, margin or gap, a position offset, a width or height, a font size, line height or letter spacing, " +
			"an opacity between 0 and 1, a box shadow, or a colour (hex, rgb(), hsl(), oklch(), white, black). Each one " +
			"reads a token (--stroke-*, --radii-*, --duration-*, --z-*, --spacing-*, --text-*, --leading-*, --tracking-*, " +
			"--opacity-*, --shadow-*, --color-*) or a --ui-<component>-<part> knob, with the old value as the fallback. " +
			"Literals inside a var() fallback or a calc() that reads a token, zero, 1px hairlines and visually hidden boxes, " +
			"em, %, ch and viewport lengths, line-height 0 and 1, opacity 0 and 1, local stacking orders, loop periods over " +
			"500ms and animation-delay pass. Dev tooling (framework/dev) is held to the width, radius, motion and layer " +
			"arms only: its chrome is not an app theme's.",
		Why: "GOFASTR1807 judges a value whole, so `border: 1px solid var(--color-border)` and `transition: color 150ms ease` " +
			"passed it: no token value equals the shorthand, and a `padding: 6px` or `width: 18px` matches no token at all. " +
			"A theme that sets --stroke-thin to 3px, every radius to 0, a tighter spacing scale or a larger control size " +
			"then reaches none of those declarations, and the kit cannot be restyled by one theme block alone.",
		Fix: "Read the token or knob with today's value as the fallback: `var(--stroke-thin, 1px) solid`, " +
			"`border-radius: var(--radii-full, 9999px)`, `var(--duration-fast, 150ms)`, `z-index: var(--z-dropdown, 100)`, " +
			"`padding: var(--spacing-md, 8px)`, `width: var(--ui-checkbox-box-size, 18px)`, `line-height: var(--leading-snug, 1.4)`, " +
			"`box-shadow: var(--shadow-md)`, `color: var(--ui-gallery-caption-fg, white)`. An off-step value is a calc() over " +
			"a token (`calc(var(--spacing-sm, 4px) * 1.5)`), and geometry that follows a size is a calc() over its knobs, " +
			"so both still move with the theme.",
		Doc: "theming",
		Examples: []Example{{
			Bad: "border: 1px solid var(--color-border); transition: color 150ms ease; padding: 6px 12px; width: 18px;",
			Good: "border: var(--stroke-thin, 1px) solid var(--color-border); transition: color var(--duration-fast, 150ms) ease; " +
				"padding: calc(var(--spacing-sm, 4px) * 1.5) calc(var(--spacing-sm, 4px) * 3); width: var(--ui-checkbox-box-size, 18px);",
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
			"attached to server-rendered markup, prefer the runtime's `data-cui-*` hydration over a " +
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
