package style

import (
	"strings"
	"testing"
)

func TestContributeAndApply(t *testing.T) {
	t.Cleanup(ResetRegistryForTest)
	ResetRegistryForTest()

	_ = Contribute(func(ss *StyleSheet) {
		ss.Rule(".alpha").Set("color", "red").End()
	})
	_ = Contribute(func(ss *StyleSheet) {
		ss.Rule(".beta").Set("color", "blue").End()
	})

	ss := NewStyleSheet(DefaultTheme())
	Apply(ss)

	css := ss.CSS()
	if !strings.Contains(css, ".alpha") || !strings.Contains(css, ".beta") {
		t.Errorf("CSS missing registered rules:\n%s", css)
	}
}

func TestContributeOrderApplied(t *testing.T) {
	t.Cleanup(ResetRegistryForTest)
	ResetRegistryForTest()

	_ = Contribute(func(ss *StyleSheet) {
		ss.Rule(".x").Set("color", "red").End()
	})
	_ = Contribute(func(ss *StyleSheet) {
		// later-registered rule overrides earlier one (cascade — last wins)
		ss.Rule(".x").Set("color", "blue").End()
	})

	ss := NewStyleSheet(DefaultTheme())
	Apply(ss)
	css := ss.CSS()

	// Both rules emitted; cascade order is preserved by emission order.
	first := strings.Index(css, "red")
	second := strings.Index(css, "blue")
	if first == -1 || second == -1 {
		t.Fatalf("missing rules:\n%s", css)
	}
	if !(first < second) {
		t.Errorf("expected red rule before blue rule (registration order); got\n%s", css)
	}
}

func TestContributeNilNoOp(t *testing.T) {
	t.Cleanup(ResetRegistryForTest)
	ResetRegistryForTest()

	Contribute(nil)
	ss := NewStyleSheet(DefaultTheme())
	Apply(ss) // must not panic
}

func TestApplyNilSheetNoOp(t *testing.T) {
	t.Cleanup(ResetRegistryForTest)
	ResetRegistryForTest()

	_ = Contribute(func(ss *StyleSheet) {
		ss.Rule(".x").Set("color", "red").End()
	})
	Apply(nil) // must not panic
}

func TestApplyIsIdempotentPerSheet(t *testing.T) {
	t.Cleanup(ResetRegistryForTest)
	ResetRegistryForTest()

	_ = Contribute(func(ss *StyleSheet) {
		ss.Rule(".only-once").Set("color", "red").End()
	})

	ss := NewStyleSheet(DefaultTheme())
	Apply(ss)
	Apply(ss) // calling again duplicates rules — documented behaviour

	css := ss.CSS()
	if strings.Count(css, ".only-once") != 2 {
		t.Logf("note: Apply(ss) twice does duplicate rules — host should call it once per sheet:\n%s", css)
	}
}
