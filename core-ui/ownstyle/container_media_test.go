package ownstyle

import (
	"strings"
	"testing"
)

func TestContainerRefusesCustomMedia(t *testing.T) {
	for _, source := range []string{
		"@container (--above-md){.a{color:inherit}}",
		"@container card (--missing){.a{color:inherit}}",
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
	} {
		sheet, _ := Parse(source)
		if _, err := Compile(sheet, "demo", KindScoped, defaultTokens(t)); err != nil {
			t.Errorf("valid container query refused: %v", err)
		}
	}
}
