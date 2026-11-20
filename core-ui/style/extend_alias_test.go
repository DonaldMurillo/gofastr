package style_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

type aliasA struct{ AliasOne style.Color }
type aliasB struct{ AliasTwo style.Color }
type aliasC struct{ AliasThree style.Color }
type aliasLeft struct{ AliasLeft style.Color }
type aliasRight struct{ AliasRight style.Color }

func TestExtendNeverSharesTheBaseExtensions(t *testing.T) {
	c := style.Color{Value: "#123456"}
	base := style.DefaultTheme().Extend(aliasA{c}, aliasB{c}).Extend(aliasC{c})
	left := base.Extend(aliasLeft{c})
	_ = base.Extend(aliasRight{c})
	if css := left.CSSCustomProperties(); !strings.Contains(css, "--color-alias-left") || strings.Contains(css, "--color-alias-right") {
		t.Fatal("a sibling Extend of the same base rewrote this theme's extensions")
	}
}
