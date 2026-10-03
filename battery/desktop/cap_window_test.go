package desktop

import (
	"encoding/base64"
	"testing"
)

func TestWindowCapabilityMethods(t *testing.T) {
	b, _ := newTestBattery(t)
	win := &fakeWindow{title: "Original"}
	b.windowMu.Lock()
	b.window = win
	b.windowMu.Unlock()

	// title
	res, err := callMethod(t, b, "window", "title", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.(windowTitleOutput).Title != "Original" {
		t.Fatalf("title = %+v", res)
	}

	// setTitle
	if _, err := callMethod(t, b, "window", "setTitle", `{"title":"Notes — demo"}`); err != nil {
		t.Fatal(err)
	}
	if win.Title() != "Notes — demo" {
		t.Fatalf("SetTitle not applied: %q", win.Title())
	}
	if _, err := callMethod(t, b, "window", "setTitle", `{"title":""}`); err == nil {
		t.Fatal("empty title accepted")
	}

	// snapshot round-trips the fixed PNG
	res, err = callMethod(t, b, "window", "snapshot", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	png, err := base64.StdEncoding.DecodeString(res.(windowSnapshotOutput).PNG)
	if err != nil {
		t.Fatal(err)
	}
	if string(png) != string(png1x1) {
		t.Fatal("snapshot bytes differ from the fixture")
	}
}

func TestWindowSnapshotBeforeWindowUnsupported(t *testing.T) {
	b, _ := newTestBattery(t) // no window set
	_, err := callMethod(t, b, "window", "snapshot", `{}`)
	if de, ok := err.(*Error); !ok || de.Code != CodeUnsupported {
		t.Fatalf("err = %v, want unsupported", err)
	}
	_, err = callMethod(t, b, "window", "title", `{}`)
	if de, ok := err.(*Error); !ok || de.Code != CodeUnsupported {
		t.Fatalf("title err = %v, want unsupported", err)
	}
}
