package app

// Test-visible twins of the shell builders in layout_shell_test.go, so
// the external app_test package's fixtures read the same way.
var (
	// ChromeShell builds the classic banner/sidebar/footer chrome shell.
	ChromeShell = chromeShell
	// HeaderShell builds a header-only chrome shell.
	HeaderShell = headerShell
	// SidebarShell builds a sidebar-only chrome shell.
	SidebarShell = sidebarShell
	// BareShell builds a layout whose body is the primary slot alone.
	BareShell = bareShell
)
