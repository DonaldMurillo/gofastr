package desktop

import "testing"

func TestNotificationsCapabilityValidation(t *testing.T) {
	b, shell := newTestBattery(t)
	if _, err := callMethod(t, b, "notifications", "show", `{"title":"Saved","body":"your note"}`); err != nil {
		t.Fatal(err)
	}
	got := shell.notified()
	if len(got) != 1 || got[0].Title != "Saved" || got[0].Body != "your note" {
		t.Fatalf("notified = %+v", got)
	}
	// title required
	if _, err := callMethod(t, b, "notifications", "show", `{"body":"no title"}`); err == nil {
		t.Fatal("missing title accepted")
	}
	// title too long
	if _, err := callMethod(t, b, "notifications", "show", `{"title":"`+longStr(201)+`"}`); err == nil {
		t.Fatal("oversize title accepted")
	} else if de, ok := err.(*Error); !ok || de.Code != CodeInvalidInput {
		t.Fatalf("err = %v", err)
	}
	// body too long
	if _, err := callMethod(t, b, "notifications", "show", `{"title":"t","body":"`+longStr(2001)+`"}`); err == nil {
		t.Fatal("oversize body accepted")
	}
	// Permission declared.
	cap, _ := b.reg.lookup("notifications")
	if cap.Methods[0].Permission != "notifications:show" {
		t.Fatalf("permission = %q", cap.Methods[0].Permission)
	}
}

// longStr returns n 'a' characters.
func longStr(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
