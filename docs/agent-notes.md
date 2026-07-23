# Agent Notes

## 2026-07-20 - Framework maturity review
- Scope: architecture, release readiness, documentation
- Trigger: Assess where GoFastr stands after v0.38.0.
- Approach: Cross-check roadmap claims against implementation/tests before treating them as open work.
- Evidence: PR #122 removed the stale `ROADMAP.md` 7.1-7.6 findings and corrected the obsolete `AuthorizeTopic` example and Go-version reference after cross-checking them against implementation and tests.
- Next time: Cross-check status docs against implementation first, then prioritize external adoption and the v1 public-API freeze over adding surface area.
- Status: active

## 2026-07-22 - Audit skips must distinguish lane boundaries from drift
- Scope: `gofastr audit lint`, browser E2E
- Trigger: Missing gallery fixtures were silently hidden behind `t.Skip`, while legitimate `testing.Short()` guards produced noise.
- Approach: Allow canonical short-lane guards, fail hard for missing required fixtures, and keep SQL detection anchored to actual statement forms.
- Evidence: `go run ./cmd/gofastr audit lint ./examples/site` reports zero findings; focused banner/tree E2E tests pass.
- Next time: Use skips only for declared environment or lane constraints; treat missing in-repo UI contracts as failures.
- Status: active

## 2026-07-23 - Component galleries still need a real document outline
- Scope: site component demos and example screens
- Trigger: The full axe crawl found nested main landmarks, heading skips, and repeated default nav labels that isolated component tests allowed.
- Approach: Let the site layout own the sole main landmark, give demo stages an h2 before component-owned headings, and configure unique landmark labels when examples repeat a navigation primitive.
- Evidence: Targeted runtime axe scans report zero violations on all ten affected routes in both schemes; the all-pages axe test passes.
- Next time: Test reusable primitives in a composed page shell as well as in isolation.
- Status: active

## 2026-07-22 - Runtime budgets should drive module boundaries
- Scope: `core-ui/runtime`
- Trigger: Core and widget gzip budgets required overrides after optional behavior accumulated in large modules.
- Approach: Split optional widget helpers, focus handling, and deep-link behavior into demand-loaded modules; keep strict budgets override-free.
- Evidence: `go test -run 'TestRuntimeModuleSizeBudgets|TestTypicalPagePayloadBudget|TestRuntimeJSSyntax' ./core-ui/runtime` passes with no overrides.
- Next time: Add optional behavior behind a marker-driven or parent-module-driven split before raising a payload budget.
- Status: active

## 2026-07-22 - Route-aware mounts preserve framework diagnostics
- Scope: framework routing and UI host integration
- Trigger: Entity/page collisions were actionable only when entities were registered second.
- Approach: Let mountables optionally expose concrete `RoutePatterns` and check them against registered entity CRUD paths before mounting.
- Evidence: `TestEntityScreenCollision` covers both registration orders in `framework/collision_test.go`.
- Next time: Give framework-owned mount adapters route introspection so domain-specific diagnostics run before generic router panics.
- Status: active

## 2026-07-22 - Auth-owned presets avoid framework import cycles
- Scope: browser-backend authentication posture
- Trigger: A proposed `framework.WithBFFPosture` needed `AuthManager`, but `battery/auth` already imports `framework`.
- Approach: Expose `auth.WithBFFPosture` as a `framework.AppOption`; derive its API boundary from `AppConfig`, and exempt only the auth-owned logout path from generic CSRF because that handler enforces same-origin submission itself.
- Evidence: BFF tests cover untrusted origins, cookie-only login, both API-prefix option orders, and the real `ui.SignOut` flow.
- Next time: Keep one owner for shared security boundaries and test public components through the complete middleware stack.
- Status: active

## 2026-07-22 - Group public config additively before removal
- Scope: `entity.EntityConfig` API evolution
- Trigger: Related scope, pagination, and exposure fields were flattened across a large public struct.
- Approach: Add pointer sub-configs as authoritative groups, normalize them into the existing runtime fields, and retain deprecated flat fields through the documented compatibility window.
- Evidence: Entity normalization and grouped blueprint generation tests cover both Go and declaration paths.
- Next time: Introduce grouped public shapes additively and keep one normalization boundary instead of rewriting every downstream consumer at once.
- Status: active

## 2026-07-23 - Public variants and security posture need hostile contract tests
- Scope: sidebar variants and BFF authentication posture
- Trigger: Exported sidebar variants were cosmetic, while auth tests missed configured cookies, whitespace bearer forms, exact logout paths, and ordinary OPTIONS.
- Approach: Test public configuration through rendered markup, demand-loaded runtime behavior, a real browser, and the complete middleware stack.
- Evidence: Focused auth, runtime, component, and sidebar browser tests cover every corrected boundary.
- Next time: Attack the configured surface rather than a convenient default, and require behavioral E2E coverage before calling an exported variant implemented.
- Status: active
