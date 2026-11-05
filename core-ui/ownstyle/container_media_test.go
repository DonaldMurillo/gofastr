package ownstyle

import (
	"strings"
	"testing"
)

func TestContainerRefusesCustomMedia(t *testing.T) {
	for _, source := range []string{
		"@container (--above-md){.a{color:inherit}}",
		"@container card (--missing){.a{color:inherit}}",
		"@container ((--above-md) or (width > 10px)){.a{color:inherit}}",
		"@container style((--active)) and (--above-md){.a{color:inherit}}",
		"@container (--above-md) or style((--active)){.a{color:inherit}}",
		"@container style((--active)) and ((--below-md)){.a{color:inherit}}",
		".a{@media (--dark){@container (--below-md){color:inherit}}}",
	} {
		sheet, diagnostics := Parse(source)
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		if _, err := Compile(sheet, "demo", KindScoped, defaultTokens(t)); err == nil || !strings.Contains(err.Error(), "custom media") {
			t.Errorf("Compile accepted custom media in @container: %v", err)
		}
		found := false
		for _, diagnostic := range Check("demo.style.css", source, KindScoped, defaultTokens(t)) {
			found = found || diagnostic.Severity == SeverityError && strings.Contains(diagnostic.Message, "custom media")
		}
		if !found {
			t.Error("Check accepted custom media in @container")
		}
	}
	for _, source := range []string{
		"@container (min-width: 10px){.a{color:inherit}}",
		"@container style(--active: true){.a{color:inherit}}",
		"@container style((--active)){.target{color:inherit}}",
		"@container style(((--active))){.target{color:inherit}}",
		"@container STYLE((--active)){.target{color:inherit}}",
		"@container style((--active) and (not (--disabled))){.target{color:inherit}}",
		"@container (style((--active)) or style((--busy))){.target{color:inherit}}",
		"@container card style((--active)) and (width > 10px){.target{color:inherit}}",
		".target{@media (--dark){@container style((--active) or (--busy)){color:inherit}}}",
	} {
		sheet, diagnostics := Parse(source)
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		if _, err := Compile(sheet, "demo", KindScoped, defaultTokens(t)); err != nil {
			t.Errorf("valid container query %q refused: %v", source, err)
		}
		for _, diagnostic := range Check("demo.style.css", source, KindScoped, defaultTokens(t)) {
			if diagnostic.Severity == SeverityError {
				t.Errorf("valid container query %q refused: %s", source, diagnostic.Message)
			}
		}
	}
}
