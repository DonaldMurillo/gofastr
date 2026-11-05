package app

import "fmt"

// ValidateIntercepts checks that every intercept origin names a registered
// screen pattern. Hosts call it after registering screens, before serving
// requests; uihost.Mount calls it automatically. Missing origins panic with
// the target and origin patterns so an overlay cannot silently stay disabled.
func (r *Router) ValidateIntercepts() {
	for _, pattern := range r.Paths() {
		screen, _ := r.ScreenByPattern(pattern)
		if screen.Intercept == nil {
			continue
		}
		for _, origin := range append([]string{screen.Intercept.From}, screen.Intercept.AlsoFrom...) {
			if _, ok := r.ScreenByPattern(origin); !ok {
				panic(fmt.Sprintf("app: screen %q intercept origin %q has no registered screen", screen.Path, origin))
			}
		}
	}
}
