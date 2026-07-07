package style

import (
	"strings"
	"testing"
)

func TestCodeTokensEmit(t *testing.T) {
	css := DefaultTheme().CSSCustomProperties()
	for _, want := range []string{
		"--tk-kw: #C792EA;",
		"--tk-fn: #82AAFF;",
		"--tk-str: #C3E88D;",
		"--tk-num: #F78C6C;",
		"--tk-com: #676E95;",
		"--tk-type: #FFCB6B;",
		"--tk-pn: var(--color-code-text);",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("theme CSS missing %q", want)
		}
	}
}

func TestDarkCodeEmitsTkVars(t *testing.T) {
	th := DefaultTheme()
	th.DarkCode = map[string]string{"kw": "#112233"}
	css := th.CSSCustomProperties()
	if !strings.Contains(css, ":root[data-color-scheme=\"dark\"] {\n  --tk-kw: #112233;\n") {
		t.Errorf("dark block missing --tk-kw re-declaration:\n%s", css)
	}
	if !strings.Contains(css, "@media (prefers-color-scheme: dark)") {
		t.Error("prefers-color-scheme fallback block missing")
	}
	// A code-only dark palette must not flip the page colors.
	if strings.Contains(css, "background-color: var(--color-background)") {
		t.Error("code-only DarkCode should not emit the page color/background scope lines")
	}
}

func TestCodeGroupIsOptional(t *testing.T) {
	th := DefaultTheme()
	th.Code = CodeSet{}
	AutoFillNames(&th)
	if err := th.Validate(); err != nil {
		t.Fatalf("unset Code group must validate: %v", err)
	}
	if strings.Contains(th.CSSCustomProperties(), "--tk-") {
		t.Error("unset Code group must not emit --tk-* vars (component fallbacks own the palette)")
	}
}

func TestCodeTokenRefResolves(t *testing.T) {
	th := DefaultTheme()
	if got := th.ResolveAll("color: {code.kw}; border-color: {tk.str}"); got != "color: var(--tk-kw); border-color: var(--tk-str)" {
		t.Errorf("token refs resolved wrong: %s", got)
	}
}
