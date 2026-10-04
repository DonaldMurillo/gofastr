// Package names is the dependency-free list of repo analyzer names: the
// vocabulary of the //gofastr:allow(<name>) marker. It carries no
// go/analysis import on purpose, so framework/contracts (and every nested
// module that builds it) can consult the list without pulling x/tools
// into its go.sum.
package names

import "slices"

// Analyzers is every repo analyzer the vettool registers, by the name a
// marker uses: //gofastr:allow(<name>) <why>. cmd/vettool's
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
	"emitident",
	"emptyerrbranch",
	"errleak",
	"fieldtypeswitch",
	"fixedtmp",
	"intwrap",
	"laxcoerce",
	"laxenvelope",
	"layoutfunc",
	"mapwriter",
	"negdur",
	"nonfinite",
	"nowaitdelay",
	"nostore",
	"recovercallback",
	"recoverlog",
	"reflectset",
	"reqparamlimit",
	"rootread",
	"rootwrite",
	"secretcompare",
	"timestampid",
	"unboundedbody",
	"unboundedresp",
	"unseated",
	"worldreadable",
}

// Lints is every source lint outside the vettool that shares the
// marker spelling: a rule that reads files, not types, and drops its
// own finding at a marked site. layerprefix (core-ui/check) hunts kit
// vocabulary in kernel and headless sources; its markers sit at the
// refusal filters and the lightbox wiring.
var Lints = []string{
	"layerprefix",
}

// Known reports whether name is a registered repo analyzer or a source
// lint that honours the marker.
func Known(name string) bool {
	return slices.Contains(Analyzers, name) || slices.Contains(Lints, name)
}
