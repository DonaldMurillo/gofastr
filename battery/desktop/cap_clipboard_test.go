package desktop

import "testing"

func TestClipboardCapability(t *testing.T) {
	b, shell := newTestBattery(t)
	// writeText then readText through the shell.
	if _, err := callMethod(t, b, "clipboard", "writeText", `{"text":"copy me"}`); err != nil {
		t.Fatal(err)
	}
	if w := shell.clipWritesList(); len(w) != 1 || w[0] != "copy me" {
		t.Fatalf("writes = %v", w)
	}
	shell.mu.Lock()
	shell.clipText = "from shell"
	shell.mu.Unlock()
	res, err := callMethod(t, b, "clipboard", "readText", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.(clipboardTextOutput).Text != "from shell" {
		t.Fatalf("read = %+v", res)
	}
	// Permissions declared.
	cap, _ := b.reg.lookup("clipboard")
	perms := map[string]string{}
	for _, m := range cap.Methods {
		perms[m.Name] = m.Permission
	}
	if perms["readText"] != "clipboard:read" || perms["writeText"] != "clipboard:write" {
		t.Fatalf("clipboard permissions = %v", perms)
	}
}
