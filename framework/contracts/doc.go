// Package contracts is GoFastr's semantic analysis layer behind `gofastr
// verify`: a catalog of [Rule]s as data, [Analyzer]s that may emit only the
// rules they declare, a [Pass] over the source tree, and a [Report] in
// text, JSON or SARIF. Every rule is enforced at its declared severity
// unless gofastr.settings.yml or a `//gofastr:allow(<rule>) <reason>`
// comment says otherwise, and the report lists each such change.
package contracts
