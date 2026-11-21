package desktoptest

// FireDeepLink invokes the WindowConfig.OnDeepLink callback the battery installed, synchronously, the way the OS asks the app to open a URL on its scheme.
func (f *Shell) FireDeepLink(rawURL string) {
	f.mu.Lock()
	onDeepLink := f.runCfg.OnDeepLink
	f.mu.Unlock()
	if onDeepLink != nil {
		onDeepLink(rawURL)
	}
}

// AllowsNavigation reports whether the battery lets a window's main frame navigate to rawURL.
func (h *Harness) AllowsNavigation(rawURL string) bool {
	allow := h.config().AllowNavigation
	return allow == nil || allow(rawURL)
}

// OpenURL delivers a deep link the way the OS does while the app runs.
func (h *Harness) OpenURL(rawURL string) {
	h.Shell.FireDeepLink(rawURL)
}
