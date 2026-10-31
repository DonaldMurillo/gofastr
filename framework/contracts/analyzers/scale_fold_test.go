package analyzers

import "testing"

// The contracts twin of ownstyle's sameScaleValue folds the same way:
// ASCII only, as CSS matches keywords and units.
func TestSameScaleValueFoldsASCIIOnly(t *testing.T) {
	if !sameScaleValue("16PX", "1rem") || !sameScaleValue("INHERIT", "inherit") {
		t.Error("an ASCII case difference no longer compares equal")
	}
	if sameScaleValue("İnherit", "inherit") || sameScaleValue("Keep", "keep") {
		t.Error("a Unicode look-alike compared equal to an ASCII value")
	}
}
