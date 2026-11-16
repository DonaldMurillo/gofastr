package ui

// ParseButtonVariant maps a case-sensitive spelling to a built-in or
// registered ButtonVariant; ok is false, with no variant, for any other.
func ParseButtonVariant(s string) (ButtonVariant, bool) {
	switch s {
	case "primary":
		return ButtonPrimary, true
	case "secondary":
		return ButtonSecondary, true
	case "danger":
		return ButtonDanger, true
	case "ghost":
		return ButtonGhost, true
	}
	if buttonMods.has(s, kindVariant) {
		return ButtonVariant(s), true
	}
	return ButtonVariant(""), false
}
