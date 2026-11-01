// Package names is the dependency-free list of repo analyzer names: the
// vocabulary of the //gofastr:allow(<name>) marker. It carries no
// go/analysis import on purpose, so framework/contracts (and every nested
// module that builds it) can consult the list without pulling x/tools
// into its go.sum.
package names

import "slices"

// Analyzers is every repo analyzer the vettool registers, by the name a
// marker uses: //gofastr:allow(<name>) <why>. internal/analyzers/suite's
// TestAllowNamesMatchRegistration fails when this list and the
// registration drift; framework/contracts consults Known so its
// unknown-suppression meta-rule accepts these names and its stale-
// suppression check leaves them to the analyzers.
var Analyzers = []string{
	"asciifold",
	"callbackunderlock",
	"clienttimeout",
	"compositekey",
	"credfetch",
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
	"intwrap",
	"keytrim",
	"laxcoerce",
	"laxenvelope",
	"layoutfunc",
	"mapwriter",
	"negdur",
	"nonfinite",
	"nowritedeadline",
	"nowaitdelay",
	"nostore",
	"quoteident",
	"rawtext",
	"recovercallback",
	"recoverlog",
	"reflectset",
	"reqparamlimit",
	"resolvedial",
	"rootread",
	"rootwrite",
	"secretcompare",
	"settlectx",
	"tightenshared",
	"timestampid",
	"unboundedbody",
	"unboundedresp",
	"unrechecked",
	"unseated",
	"worldreadable",
	"wrapwriter",
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
