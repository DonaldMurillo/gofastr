package shellfx

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/win32"
)

func tray(tip string) {
	objc.NSString("Show the window")                   // want `NSString\("Show the window"\) hands English to the OS`
	objc.NSStringErr("Settings")                       // want `NSStringErr\("Settings"\) hands English to the OS`
	objc.NSString("CFBundleShortVersionString")        // a key, not words
	objc.NSString("WKJavaScriptExceptionMessage")      // a key, not words
	objc.NSString("")                                  // empty
	objc.NSString(tip)                                 // the caller rendered it
	win32.SetWindowText(0, "Untitled window")          // want `SetWindowText\("Untitled window"\) hands English to the OS`
	win32.AppendMenu(0, 0, 0, "Quit")                  // want `AppendMenu\("Quit"\) hands English to the OS`
	win32.MessageBox(0, "Something went wrong", "", 0) // want `MessageBox\("Something went wrong"\) hands English to the OS`
	win32.MessageBox(0, tip, "About", 0)               // want `MessageBox\("About"\) hands English to the OS`
	label := "Open file"
	win32.AppendMenu(0, 0, 0, label) // want `AppendMenu\("Open file"\) hands English to the OS`
}
