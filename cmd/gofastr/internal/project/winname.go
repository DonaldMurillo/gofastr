package project

import "strings"

// ValidWindowsFileName reports whether name can be a file in a Windows
// directory: none of <>:"/\|?* or a control character, no trailing dot
// or space, and no reserved device stem (CON, PRN, AUX, NUL, CONIN$,
// CONOUT$, COM1-9, LPT1-9 and their superscript forms), which Windows
// reserves before any extension too ("aux.exe").
func ValidWindowsFileName(name string) bool {
	if name == "" || strings.ContainsAny(name, `<>:"/\\|?*`) || strings.TrimRight(name, ". ") != name {
		return false
	}
	for _, r := range name {
		if r < 0x20 {
			return false
		}
	}
	base := strings.ToUpper(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = strings.TrimRight(base[:i], ". ")
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$",
		"COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return true
}
