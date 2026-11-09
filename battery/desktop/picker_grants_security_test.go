package desktop

import (
	"os"
	"strings"
	"testing"
)

//gofastr:test-reads /framework/docs/content/desktop.md

func TestPickerGrantDocsAreExact(t *testing.T) {
	battery, _ := newTestBattery(t)
	capability := battery.dialogsCapability()
	for _, claim := range []string{"exact", "fs.list", "confined", "never fs.writeText"} {
		if !strings.Contains(capability.Description, claim) {
			t.Errorf("dialogs description does not say %q: %s", claim, capability.Description)
		}
	}
	for _, method := range capability.Methods {
		claims := []string{"exact"}
		if method.Name == "openFolder" {
			claims = []string{"fs.readText", "fs.list", "os.Root", "never fs.writeText"}
		}
		for _, claim := range claims {
			if !strings.Contains(method.Description, claim) {
				t.Errorf("%s does not say %q: %s", method.Name, claim, method.Description)
			}
		}
	}
	doc, err := os.ReadFile("../../framework/docs/content/desktop.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{"`openFolder` grants `fs.stat`, `fs.readText` and `fs.list`", "never `fs.writeText`", "`openFile` and `saveFile` grant only the exact"} {
		if !strings.Contains(string(doc), contract) {
			t.Errorf("desktop reference is missing %q", contract)
		}
	}
}
