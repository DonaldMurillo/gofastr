package contracts

import "testing"

// Property: no spelling of a catch-all suppression becomes a live
// suppression. `all` and `*` are rejected outright by add(); any other
// wildcard-ish token (`**`, case/whitespace variants of `all`) must at
// worst land in the unknown-rule diagnostic, never in byFile. A token
// that silently matched everything would let one comment in a PR waive
// every rule, including rules written after the directive.
func TestSuppressionCatchAllSpellingsInert(t *testing.T) {
	for _, ref := range []string{"all", "ALL", " All ", "*", "**", "all,GOFASTR1403"} {
		_, sup := probePass(t, map[string]string{
			"a.go": "package a\n\n//gofastr:allow(" + ref + ") because upstream\nfunc f() {}\n",
		})
		if len(sup.byFile) != 0 {
			t.Errorf("SECURITY: [catch-all] //gofastr:allow(%q) produced a live suppression: %+v", ref, sup.byFile)
		}
		if len(sup.issues) == 0 {
			t.Errorf("SECURITY: [catch-all] //gofastr:allow(%q) was accepted silently — a catch-all must at minimum be reported", ref)
		}
	}
}
