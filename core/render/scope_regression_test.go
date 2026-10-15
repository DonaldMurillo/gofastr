package render

import "testing"

func TestTemplateTruncateNegativeCap(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	if got := FuncMap["Truncate"].(func(string, int) string)("abc", -1); got != "" {
		t.Fatal(got)
	}
}
func TestRegisterFuncRefusesReplacement(t *testing.T) {
	const name = "scope-duplicate"
	RegisterFunc(name, func() string { return "old" })
	defer func() {
		funcMu.Lock()
		delete(FuncMap, name)
		funcMu.Unlock()
		if r := recover(); r == nil {
			t.Error("duplicate function silently replaced")
		}
	}()
	RegisterFunc(name, func() string { return "new" })
}

func TestTemplateTruncateZeroKeepsMarker(t *testing.T) {
	if got := FuncMap["Truncate"].(func(string, int) string)("abc", 0); got != "…" {
		t.Fatal(got)
	}
}
