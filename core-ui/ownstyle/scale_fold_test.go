package ownstyle

import "testing"

// CSS matches keywords and units ASCII case-insensitively, so a fallback
// compares to its token the same way: "16PX" is "16px", but U+0130 and
// the KELVIN SIGN, which strings.ToLower folds onto i and k, are other
// values.
func TestSameScaleValueFoldsASCIIOnly(t *testing.T) {
	if !SameScaleValue("16PX", "1rem") || !SameScaleValue("INHERIT", "inherit") {
		t.Error("an ASCII case difference no longer compares equal")
	}
	if SameScaleValue("İnherit", "inherit") || SameScaleValue("Keep", "keep") {
		t.Error("a Unicode look-alike compared equal to an ASCII value")
	}
}
