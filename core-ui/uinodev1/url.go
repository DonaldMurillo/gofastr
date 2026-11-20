package uinodev1

// IsValidHostRelative reports whether s is a host-relative same-origin path a module may link to.
func IsValidHostRelative(s string) bool {
	if s == "" {
		return false
	}
	if s[0] != '/' {
		return false
	}
	if len(s) >= 2 && s[1] == '/' {
		return false
	}
	for i := range s {
		b := s[i]
		if b == '\\' {
			return false
		}
		if b <= 0x20 || b == 0x7F {
			return false
		}
	}
	if hasDangerousScheme(s) {
		return false
	}
	return true
}

var dangerousSchemes = []string{
	"javascript:",
	"data:",
	"vbscript:",
	"blob:",
	"file:",
}

func hasDangerousScheme(s string) bool {
	lo := make([]byte, len(s))
	for i := range s {
		b := s[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		lo[i] = b
	}
	for _, scheme := range dangerousSchemes {
		if containsSubstring(string(lo), scheme) {
			return true
		}
	}
	return false
}

func containsSubstring(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
