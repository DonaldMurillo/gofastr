package desktoptest

// The deep-link seam: the OS hands the app a URL on its custom scheme
// (a cold launch may deliver it before the window is up). The fake
// shell plays that hand.

// FireDeepLink invokes the WindowConfig.OnDeepLink callback the
// battery installed, synchronously, the way the OS asks the app to
// open a URL on its scheme. The battery queues the link when no window
// is up yet.
func (f *Shell) FireDeepLink(rawURL string) {
	f.mu.Lock()
	onDeepLink := f.runCfg.OnDeepLink
	f.mu.Unlock()
	if onDeepLink != nil {
		onDeepLink(rawURL)
	}
}

// OpenURL is the OS opening a deep link while the app runs: the page's
// answer arrives through the recorded evals (navigate) and events
// (deep_link), the same observables the menus use.
func (h *Harness) OpenURL(rawURL string) {
	h.Shell.FireDeepLink(rawURL)
}
