package style_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Sol review #2. isValidColor previously accepted any value that started with
// a colour-function prefix, ended in ")", and had balanced parens. That admits
// a colour FOLLOWED BY a second call, and admits resource-loading functions
// nested inside var() fallbacks — neither of which is a colour.
//
// The sink is real: an embedded surface's brand token is emitted as
// --color-primary, and `background: var(--color-primary)` resolves a colour
// plus an image, fetching attacker-controlled URLs wherever CSP allows.
func TestColor_RejectsTrailingCallAndResourceFunctions(t *testing.T) {
	base := style.DefaultTheme()

	hostile := []string{
		// A valid colour followed by a second invocation.
		"rgb(0 0 0) URL(https://attacker.example/pixel)",
		"rgb(0 0 0) url(https://attacker.example/pixel)",
		"oklch(62% 0.19 264) image-set('https://attacker.example/x' 1x)",
		// Resource functions smuggled through a var() fallback — no "url(" token.
		`var(--missing, image-set("https://attacker.example/pixel" 1x))`,
		`var(--missing, -webkit-image-set("https://attacker.example/x" 1x))`,
		`var(--missing, cross-fade(url(https://attacker.example/x), red))`,
		`var(--missing, element(#attacker))`,
		`var(--missing, paint(evil))`,
		`var(--missing, src("https://attacker.example/x"))`,
		// Nested inside an otherwise-legitimate colour function.
		`color-mix(in srgb, var(--x, image-set("https://attacker.example/x" 1x)) 50%, red)`,
		// Case variations — CSS function names are case-insensitive.
		"rgb(0 0 0) Url(https://attacker.example/x)",
		"RGB(0 0 0) IMAGE-SET('https://attacker.example/x' 1x)",
	}

	for _, v := range hostile {
		t.Run(v, func(t *testing.T) {
			out, err := style.ApplyTokens(base, map[string]string{"color-primary": v})
			if err == nil {
				t.Fatalf("ACCEPTED hostile colour %q", v)
			}
			// And it must never reach emitted CSS.
			if css := out.CSSCustomProperties(); strings.Contains(strings.ToLower(css), "attacker.example") {
				t.Errorf("payload reached CSS output for %q", v)
			}
		})
	}
}

// The grammar must not become so strict that real colours are rejected —
// otherwise it just moves the failure from security to usability.
func TestColor_AcceptsLegitimateValues(t *testing.T) {
	base := style.DefaultTheme()

	valid := []string{
		"#fff", "#ffff", "#4F46E5", "#4F46E5CC",
		"rgb(79 70 229)", "rgba(79, 70, 229, 0.5)",
		"hsl(243 75% 59%)", "hsla(243, 75%, 59%, 0.5)",
		"oklch(62% 0.19 264)", "oklab(0.62 -0.1 0.2)",
		"color-mix(in srgb, var(--color-primary) 15%, transparent)",
		"color-mix(in oklch, oklch(62% 0.19 264) 50%, white)",
		"var(--color-primary)",
		"var(--color-primary, #4F46E5)",
		"var(--color-primary, color-mix(in srgb, red 50%, blue))",
		"rgb(calc(10 * 2) 70 229)",
		"transparent", "currentColor", "rebeccapurple", "RED",
	}

	for _, v := range valid {
		t.Run(v, func(t *testing.T) {
			if _, err := style.ApplyTokens(base, map[string]string{"color-primary": v}); err != nil {
				t.Errorf("REJECTED legitimate colour %q: %v", v, err)
			}
		})
	}
}
