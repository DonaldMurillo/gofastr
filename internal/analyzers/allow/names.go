package allow

import "slices"

// Names is every repo analyzer the vettool registers, by the name a
// marker uses: //gofastr:allow(<name>) <why>. cmd/vettool's
// TestAllowNamesMatchRegistration fails when this list and the
// registration drift; framework/contracts consults Known so its
// unknown-suppression meta-rule accepts these names and its stale-
// suppression check leaves them to the analyzers.
var Names = []string{
	"asciifold",
	"callbackunderlock",
	"clienttimeout",
	"compositekey",
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
	"mapwriter",
	"recovercallback",
	"reflectset",
	"reqparamlimit",
	"rootwrite",
	"secretcompare",
	"timestampid",
	"unboundedbody",
	"worldreadable",
}

// Known reports whether name is a registered repo analyzer.
func Known(name string) bool {
	return slices.Contains(Names, name)
}
