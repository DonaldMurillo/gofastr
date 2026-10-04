package ui

// ParseButtonVariant maps a variant spelling to its ButtonVariant,
// reporting whether the spelling is a built-in variant or one an app
// registered through RegisterButtonVariant.
// It is the single mapping behind the three host-side renderers that
// turn a caller-supplied variant string into a button (kiln's node
// renderer, the resource screens, and uihost's trusted node renderer);
// each of those keeps its own default for an unrecognised spelling, so
// ok=false carries no variant. The comparison is case-sensitive, like
// every variant lookup in this package.
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
