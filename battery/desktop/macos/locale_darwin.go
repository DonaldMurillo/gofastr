//go:build darwin && arm64

package macos

import "github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"

// preferredLanguages is the user's language list from System Settings,
// best first ([NSLocale preferredLanguages], e.g. "fr-CA", "en-US"). A
// Finder-launched app has no LANG, so this, not the environment, is
// what names the menu language. Call it on the main thread after the
// frameworks load.
func preferredLanguages() []string {
	arr := objc.ID(objc.Send(objc.Class("NSLocale"), objc.Sel("preferredLanguages")))
	if arr == 0 {
		return nil
	}
	n := int(objc.Send(arr, objc.Sel("count")))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, objc.GoString(objc.ID(objc.Send(arr, objc.Sel("objectAtIndex:"), uintptr(i)))))
	}
	return out
}
