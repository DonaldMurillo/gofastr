package upload

import "testing"

// The extension allow list compares with an ASCII-only fold: "GIF" is
// still "gif", but U+0130 and the KELVIN SIGN, which strings.ToLower
// folds onto i and k, are other extensions and are refused.
func TestValidateExtFoldsASCIIOnly(t *testing.T) {
	if err := ValidateExt("photo.GIF", []string{"gif"}); err != nil {
		t.Errorf("an upper-case ASCII extension was refused: %v", err)
	}
	if err := ValidateExt("photo.gif", []string{"GIF"}); err != nil {
		t.Errorf("an upper-case allow-list entry stopped matching: %v", err)
	}
	if err := ValidateExt("photo.gİf", []string{"gif"}); err == nil {
		t.Error("photo.gİf passed an allow list of gif")
	}
	if err := ValidateExt("map.Kml", []string{"kml"}); err == nil {
		t.Error("map.Kml passed an allow list of kml")
	}
}
