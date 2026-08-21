// Package contracts is GoFastr's semantic analysis layer: the rules that
// say whether a codebase is a *good GoFastr application*, not merely a
// compiling one.
//
// The compiler verifies correctness. `go vet` verifies suspicious
// constructs. Neither can answer "does this route have auth", "is this
// screen covered by a test", "did someone hand-roll CSS the design system
// already provides", or "is the dependency direction still intact".
// Those are framework semantics, and this package is where they live.
//
// # The three moving parts
//
//   - A [Rule] is the *documentation*: a stable ID (GOFASTR1002), a
//     capability, a default severity, and, mandatory, the Why, the Fix,
//     and a bad/good example pair. Rules are data. They are readable
//     without running anything, which is what makes the catalog useful to
//     an agent over MCP.
//   - An [Analyzer] is the *detector*: it declares which rules it can
//     emit and walks a [Pass] to produce [Diagnostic]s. Analyzers never
//     invent messages for rules they did not declare. [Run] rejects
//     that, so the catalog can never drift from what actually fires.
//   - A [Config] is the *relaxation*: strict is the zero value. Every
//     rule in the catalog is enforced at its declared severity unless
//     configuration explicitly turns it down. There is no opt-in; there
//     is only visible, reviewable opt-out.
//
// # Strictness is the default; config is the only way to change it
//
// This inverts the usual linter posture, deliberately. A rule that must
// be switched on is a rule nobody switches on. The cost of the inversion
// is that adding a rule to the catalog can break existing builds, which
// is the point, and why every rule ships with a `Fix` and a suppression
// path.
//
// Precisely: nothing is enforced *less* than the catalog declares unless
// someone writes it down. Configuration can move a rule either way: a
// team wanting `routing/untested-route` to be an error rather than a
// warning is making a real choice and should be able to, but the default
// is never quieter than declared, and every change is listed in the
// report footer whichever direction it goes.
//
// The two escape hatches are:
//
//	# gofastr.contracts.yml: visible in review, applies to a whole tree
//	contracts:
//	  performance:
//	    severity: warn
//	  rules:
//	    GOFASTR1003: off
//
//	//gofastr:allow(GOFASTR1003) covered by the e2e suite in examples/site
//
// Both require a human to write a reason down. A suppression that stops
// matching anything is itself reported (GOFASTR0002), so the escape
// hatches cannot silently accumulate.
//
// # Output is for agents first
//
// [Report] renders as text for humans, JSON for agents, and SARIF for
// IDEs and code scanning. The JSON form carries the whole rule, Why,
// Fix, examples, doc URL, beside each diagnostic, so a coding agent that
// receives one has everything it needs to make the change without a
// second round-trip.
package contracts
