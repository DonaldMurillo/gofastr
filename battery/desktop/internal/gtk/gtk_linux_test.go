//go:build linux && (amd64 || arm64)

package gtk

import (
	"reflect"
	"testing"
)

// TestLoadIsLazy pins the discipline internal/objc's init comment
// spells out: importing the package must load nothing. cmd/gofastr
// blank-imports the desktop battery for its AGENTS inventory, and an
// eager dlopen would drag GTK and WebKitGTK into every CLI invocation on
// a Linux box. This test runs before any other in the package only
// because Go runs tests in source order within a file and this is the
// first file — so it also asserts the count instead of trusting order.
func TestLoadIsLazy(t *testing.T) {
	if n := DlopenCount(); n != 0 {
		t.Fatalf("%d dlopen calls happened before the first Load; package init must open nothing", n)
	}
}

// TestSmokeResolvesWebViewAndInit is the slice's gate: dlopen the GTK 3
// and WebKitGTK 4.1 stack and resolve the two entry points that prove
// both halves are real — gtk_init_check for GTK and
// webkit_web_view_get_type for WebKitGTK. It skips, rather than fails,
// on a machine without the libraries, because a Linux dev box with no
// desktop stack is a legitimate place to run `go test ./...`.
func TestSmokeResolvesWebViewAndInit(t *testing.T) {
	if err := Load(); err != nil {
		if Missing(err) {
			t.Skip("GTK/WebKitGTK not installed: ", err)
		}
		t.Fatal(err)
	}
	if Sym.GtkInitCheck == 0 {
		t.Error("gtk_init_check resolved to 0")
	}
	if Sym.WebkitWebViewGetType == 0 {
		t.Error("webkit_web_view_get_type resolved to 0")
	}
	if DlopenCount() == 0 {
		t.Error("Load reported success without opening a library")
	}
}

// TestEveryRequiredSymbolResolves is what keeps the table honest. A
// WebKitGTK release that renames or drops one of these fails here, on a
// machine that has the libraries, instead of at window-open time in a
// user's app. Optional libraries (gdk-pixbuf, libnotify) are excluded:
// their absence is a supported configuration.
func TestEveryRequiredSymbolResolves(t *testing.T) {
	if err := Load(); err != nil {
		if Missing(err) {
			t.Skip("GTK/WebKitGTK not installed: ", err)
		}
		t.Fatal(err)
	}
	for _, s := range table() {
		if s.lib.optional {
			continue
		}
		if *s.target == 0 {
			t.Errorf("%s (%s) resolved to 0", s.name, s.lib.soname)
		}
	}
}

// TestTableCoversEverySymbolsField catches the copy-paste failure this
// shape invites: adding a field to Symbols and forgetting the table
// entry, which would leave a silently-zero function pointer for the
// shell to call. Reflection over the struct is the only way to see the
// gap, since a zero uintptr is a legal value.
func TestTableCoversEverySymbolsField(t *testing.T) {
	covered := map[string]bool{}
	base := reflect.ValueOf(&Sym).Pointer()
	for _, s := range table() {
		off := reflect.ValueOf(s.target).Pointer() - base
		covered[fieldNameAt(t, off)] = true
	}
	v := reflect.TypeOf(Sym)
	for i := range v.NumField() {
		if name := v.Field(i).Name; !covered[name] {
			t.Errorf("Symbols.%s has no entry in table(); it would stay zero forever", name)
		}
	}
}

func fieldNameAt(t *testing.T, off uintptr) string {
	t.Helper()
	v := reflect.TypeOf(Sym)
	for i := range v.NumField() {
		if v.Field(i).Offset == off {
			return v.Field(i).Name
		}
	}
	t.Fatalf("table() entry points at offset %d, which is not a Symbols field", off)
	return ""
}
