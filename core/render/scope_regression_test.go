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

// RegisterFunc replaces a function already registered under the name:
// that is how a host overrides a built-in (audit-deps.md).
func TestRegisterFuncReplaces(t *testing.T) {
	const name = "scope-duplicate"
	defer func() {
		funcMu.Lock()
		delete(FuncMap, name)
		funcMu.Unlock()
	}()
	RegisterFunc(name, func() string { return "old" })
	RegisterFunc(name, func() string { return "new" })
	funcMu.RLock()
	fn := FuncMap[name].(func() string)
	funcMu.RUnlock()
	if got := fn(); got != "new" {
		t.Fatalf("FuncMap[%q]() = %q, want the replacement", name, got)
	}
}

func TestTemplateTruncateZeroKeepsMarker(t *testing.T) {
	if got := FuncMap["Truncate"].(func(string, int) string)("abc", 0); got != "…" {
		t.Fatal(got)
	}
}
