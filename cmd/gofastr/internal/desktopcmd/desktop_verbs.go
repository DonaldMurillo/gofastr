package desktopcmd

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
