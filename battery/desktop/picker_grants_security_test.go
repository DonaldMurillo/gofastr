package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

//gofastr:test-reads /framework/docs/content/desktop.md

func TestPickerGrantDocsAreExact(t *testing.T) {
	battery, _ := newTestBattery(t)
	capability := battery.dialogsCapability()
	if !strings.Contains(capability.Description, "exact") || !strings.Contains(capability.Description, "stat") || !strings.Contains(capability.Description, "descendants") {
		t.Fatalf("dialogs still overclaims picker authority: %s", capability.Description)
	}
	for _, method := range capability.Methods {
		if !strings.Contains(method.Description, "exact") {
			t.Errorf("%s does not describe its exact grant: %s", method.Name, method.Description)
		}
		if method.Name == "openFolder" && (!strings.Contains(method.Description, "fs.stat") || !strings.Contains(method.Description, "nothing under")) {
			t.Errorf("openFolder does not describe stat-only authority: %s", method.Description)
		}
	}
	doc, err := os.ReadFile("../../framework/docs/content/desktop.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{"`openFolder` grants only `fs.stat`", "nothing under the folder", "`openFile` and `saveFile` grant only the exact"} {
		if !strings.Contains(string(doc), contract) {
			t.Errorf("desktop reference is missing %q", contract)
		}
	}
}

func TestFolderPickerGrantsOnlyItself(t *testing.T) {
	battery, shell := newTestBattery(t)
	folder := t.TempDir()
	child := filepath.Join(folder, "child.txt")
	if err := os.WriteFile(child, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	shell.folderResult = folder
	if _, err := callMethod(t, battery, "dialogs", "openFolder", `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callMethod(t, battery, "fs", "stat", `{"path":`+quoteJSON(folder)+`}`); err != nil {
		t.Fatalf("picked folder stat refused: %v", err)
	}
	for _, method := range []string{"stat", "readText", "writeText"} {
		input := `{"path":` + quoteJSON(child)
		if method == "writeText" {
			input += `,"text":"changed"`
		}
		_, err := callMethod(t, battery, "fs", method, input+`}`)
		if refusal, ok := err.(*Error); !ok || refusal.Code != CodeDenied {
			t.Errorf("child %s = %v, want denied", method, err)
		}
	}
	data, err := os.ReadFile(child)
	if err != nil || string(data) != "private" {
		t.Fatalf("child changed: %q (%v)", data, err)
	}
}
