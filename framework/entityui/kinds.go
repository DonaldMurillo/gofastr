package entityui

// builtinKinds are the field kinds entityui draws itself; an app kind of
// the same name replaces the built-in one.
var builtinKinds = []string{"email", "url", "color", "markdown", "code"}

func isBuiltinKind(name string) bool {
	for _, k := range builtinKinds {
		if k == name {
			return true
		}
	}
	return false
}
