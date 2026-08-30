package n

// allowFont matches the validator name filter and every arm appears in
// the package's test literals: silent.
func allowFont(ext string) bool {
	switch ext {
	case "woff", "woff2", "ttf":
		return true
	}
	return false
}

// dispatch is behavior dispatch: no validator name, no bool returned
// from the switch, so its untested "batch" arm is not an enumeration.
func dispatch(mode string) string {
	switch mode {
	case "stream":
		return "s"
	case "batch":
		return "b"
	}
	return ""
}

// blockedTypes is the slice shape with every element pinned by the
// tests: silent.
var blockedTypes = []string{"csv", "tsv"}
