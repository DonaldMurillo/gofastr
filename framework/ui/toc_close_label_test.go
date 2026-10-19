package ui

import (
	"strings"
	"testing"
)

func TestTOCPhoneSheetCloseLabel(t *testing.T) {
	out := string(TableOfContents(TOCConfig{Label: "Contenido", CloseLabel: "Cerrar contenido", PhoneSheet: true,
		Items: []TOCItem{{ID: "intro", Label: "Introducción"}}}))
	if !strings.Contains(out, `aria-label="Cerrar contenido"`) || strings.Contains(out, `aria-label="Close"`) {
		t.Fatalf("phone sheet did not use its localized dismiss label: %s", out)
	}
}
