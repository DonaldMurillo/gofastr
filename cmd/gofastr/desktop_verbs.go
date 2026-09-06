package main

// desktopExtraVerb dispatches the desktop verbs added after run, build,
// and types (the release tooling: keygen, feed). It reports whether
// args[0] was one of them.
func desktopExtraVerb(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "keygen":
		runDesktopKeygen(args[1:])
		return true
	case "feed":
		runDesktopFeed(args[1:])
		return true
	}
	return false
}
