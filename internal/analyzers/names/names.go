// Package names holds the repo analyzer vocabulary and parses allow markers
// without importing go/analysis.
package names

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Analyzers is every repo analyzer the vettool registers, by the name a
// marker uses: //gofastr:allow(<name>) <why>. internal/analyzers/suite's
// TestAllowNamesMatchRegistration fails when this list and the
// registration drift; framework/contracts consults Known so its
// unknown-suppression meta-rule accepts these names and its stale-
// suppression check leaves them to the analyzers.
var Analyzers = []string{
	"asciifold",
	"bareduration",
	"callbackunderlock",
	"clienttimeout",
	"compositekey",
	"credfetch",
	"deferreset",
	"controlbytes",
	"discardeddecode",
	"discardederr",
	"discardmutator",
	"divlimit",
	"embedskip",
	"emitident",
	"emptyerrbranch",
	"errleak",
	"fieldtypeswitch",
	"fixedtmp",
	"floatnum",
	"growmap",
	"intwrap",
	"iounderlock",
	"keytrim",
	"laxcoerce",
	"laxenvelope",
	"layoutfunc",
	"mapwriter",
	"negdur",
	"nonfinite",
	"nowritedeadline",
	"nullarray",
	"optzero",
	"poisonedconn",
	"nowaitdelay",
	"nostore",
	"quoteident",
	"rawtext",
	"recovercallback",
	"recoverlog",
	"reflectset",
	"reqparamlimit",
	"sanitizefix",
	"resolvedial",
	"rootread",
	"rootwrite",
	"secretcompare",
	"settlectx",
	"siblingresult",
	"silentskip",
	"textbind",
	"tightenshared",
	"timestampid",
	"unboundedbody",
	"unboundedresp",
	"unrechecked",
	"unseated",
	"worldreadable",
	"wrapwriter",

	// 2026-11 bug-bash rules (response)
	"recoverwrite",
	"statusrange",
	"doublewrite",
	"listheader",
	// 2026-11 bug-bash rules (request-misc)
	"authpresence",
	"fanoutreq",
	"multipartparse",
	"ratelimitkey",
	"rsakeysize",

	// 2026-11 bug-bash rules (read hooks)
	"hookedread",
}

// Lints is every source lint outside the vettool that shares the
// marker spelling: a rule that reads files, not types, and drops its
// own finding at a marked site. layerprefix (core-ui/check) hunts kit
// vocabulary in kernel and headless sources; its markers sit at the
// refusal filters and the lightbox wiring. runtimeprose (core-ui/check)
// holds runtime prose to say(); its markers sit in runtime scripts, which
// gofastr verify reads for GOFASTR1802/1803 and their directives.
var Lints = []string{
	"layerprefix",
	"runtimeprose",
}

// Known reports whether name is a registered repo analyzer or a source
// lint that honours the marker.
func Known(name string) bool {
	return slices.Contains(Analyzers, name) || slices.Contains(Lints, name)
}

// SplitRuleList splits a marker's rule list on commas, spaces and tabs.
func SplitRuleList(list string) []string {
	return strings.FieldsFunc(list, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
}

var marker = regexp.MustCompile(`^(?://|/\*)\s*gofastr:allow(-file)?\(([^)]*)\)([^\n]*)`)

// Marker is a parsed directive. The caller validates its rules and reason
// and decides which lines the directive covers.
type Marker struct {
	File   bool
	Rules  []string
	Reason string
}

// ParseMarker recognizes a directive at the start of a comment token.
func ParseMarker(comment string) (Marker, bool) {
	m := marker.FindStringSubmatch(comment)
	if m == nil {
		return Marker{}, false
	}
	reason := strings.TrimSpace(m[3])
	if strings.HasPrefix(comment, "/*") {
		reason = strings.TrimSuffix(reason, "*/")
	}
	reason = strings.TrimLeftFunc(reason, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("-—:", r)
	})
	out := Marker{
		File:   m[1] != "",
		Reason: strings.TrimSpace(reason),
	}
	for _, rule := range SplitRuleList(m[2]) {
		if rule = strings.TrimSpace(rule); rule != "" {
			out.Rules = append(out.Rules, rule)
		}
	}
	return out, true
}
