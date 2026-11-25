package style

import (
	"math"
	"strconv"
	"strings"
)

func contrastRatio(a, b string) (ratio float64, ok bool) {
	ar, ag, ab, okA := parseColor(a)
	br, bg, bb, okB := parseColor(b)
	if !okA || !okB {
		return 0, false
	}
	la := srgbLuminance(ar, ag, ab)
	lb := srgbLuminance(br, bg, bb)
	if la > lb {
		return (la + 0.05) / (lb + 0.05), true
	}
	return (lb + 0.05) / (la + 0.05), true
}

func parseColor(s string) (r, g, b float64, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "#") {
		return hexRGB(s)
	}
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return 0, 0, 0, false
	}
	fn, args := s[:open], s[open+1:len(s)-1]
	comps, alpha, ok := colorArgs(args)
	if !ok || len(comps) != 3 || alpha < 1 {
		return 0, 0, 0, false
	}
	switch fn {
	case "rgb", "rgba":
		var out [3]float64
		for i, c := range comps {
			v, ok := cssNumber(c, 255)
			if !ok {
				return 0, 0, 0, false
			}
			out[i] = clamp01(v / 255)
		}
		return out[0], out[1], out[2], true
	case "hsl", "hsla":
		h, okH := cssHue(comps[0])
		sat, okS := cssNumber(comps[1], 100)
		lig, okL := cssNumber(comps[2], 100)
		if !okH || !okS || !okL {
			return 0, 0, 0, false
		}
		r, g, b := hslToRGB(h, clamp01(sat/100), clamp01(lig/100))
		return r, g, b, true
	case "oklab":
		l, okL := cssNumber(comps[0], 1)
		a, okA := cssNumber(comps[1], 0.4)
		bb, okB := cssNumber(comps[2], 0.4)
		if !okL || !okA || !okB {
			return 0, 0, 0, false
		}
		r, g, b := oklabToSRGB(l, a, bb)
		return r, g, b, true
	case "oklch":
		l, okL := cssNumber(comps[0], 1)
		c, okC := cssNumber(comps[1], 0.4)
		h, okH := cssHue(comps[2])
		if !okL || !okC || !okH {
			return 0, 0, 0, false
		}
		c = math.Max(c, 0)
		rad := h * math.Pi / 180
		r, g, b := oklabToSRGB(l, c*math.Cos(rad), c*math.Sin(rad))
		return r, g, b, true
	}
	return 0, 0, 0, false
}

func colorArgs(args string) (comps []string, alpha float64, ok bool) {
	alpha = 1
	if strings.Contains(args, "var(") || strings.Contains(args, "(") {
		return nil, 0, false
	}
	main, alphaPart, hasSlash := strings.Cut(args, "/")
	comps = strings.FieldsFunc(main, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if !hasSlash && len(comps) == 4 {
		alphaPart, comps, hasSlash = comps[3], comps[:3], true
	}
	if hasSlash {
		a, ok := cssNumber(strings.TrimSpace(alphaPart), 1)
		if !ok {
			return nil, 0, false
		}
		alpha = a
	}
	return comps, alpha, true
}

func cssNumber(s string, full float64) (float64, bool) {
	if s == "none" {
		return 0, true
	}
	if p, isPct := strings.CutSuffix(s, "%"); isPct {
		v, err := strconv.ParseFloat(p, 64)
		return v / 100 * full, err == nil
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

func cssHue(s string) (float64, bool) {
	scale := 1.0
	switch {
	case strings.HasSuffix(s, "deg"):
		s = strings.TrimSuffix(s, "deg")
	case strings.HasSuffix(s, "turn"):
		s, scale = strings.TrimSuffix(s, "turn"), 360
	case strings.HasSuffix(s, "rad"):
		s, scale = strings.TrimSuffix(s, "rad"), 180/math.Pi
	}
	v, ok := cssNumber(s, 360)
	return v * scale, ok
}

func isPlainHex(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return false
	}
	_, _, _, ok := hexRGB(s)
	return ok
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func hslToRGB(h, s, l float64) (r, g, b float64) {
	h = math.Mod(math.Mod(h, 360)+360, 360)
	f := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		a := s * math.Min(l, 1-l)
		return l - a*math.Max(-1, math.Min(k-3, math.Min(9-k, 1)))
	}
	return f(0), f(8), f(4)
}

func oklabToSRGB(l, a, b float64) (float64, float64, float64) {
	l_ := l + 0.3963377774*a + 0.2158037573*b
	m_ := l - 0.1055613458*a - 0.0638541728*b
	s_ := l - 0.0894841775*a - 1.2914855480*b
	L, M, S := l_*l_*l_, m_*m_*m_, s_*s_*s_
	lr := 4.0767416621*L - 3.3077115913*M + 0.2309699292*S
	lg := -1.2684380046*L + 2.6097574011*M - 0.3413193965*S
	lb := -0.0041960863*L - 0.7034186147*M + 1.7076147010*S
	enc := func(v float64) float64 {
		v = clamp01(v)
		if v <= 0.0031308 {
			return 12.92 * v
		}
		return 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return enc(lr), enc(lg), enc(lb)
}

func hexRGB(s string) (r, g, b float64, ok bool) {
	s = trimHex(s)
	switch {
	case len(s) == 4 && (s[3] == 'f' || s[3] == 'F'):
		s = s[:3]
	case len(s) == 8 && strings.EqualFold(s[6:], "ff"):
		s = s[:6]
	}
	short := len(s) == 3
	if !short && len(s) != 6 {
		return 0, 0, 0, false
	}
	scale := 255.0
	var comps [3]float64
	for i := range comps {
		var v float64
		var ok bool
		if short {
			v, ok = hexPair(s[i], 0, true)
		} else {
			v, ok = hexPair(s[2*i], s[2*i+1], false)
		}
		if !ok {
			return 0, 0, 0, false
		}
		comps[i] = v / scale
	}
	return comps[0], comps[1], comps[2], true
}

func hexPair(a, b byte, short bool) (float64, bool) {
	hi, ok := hexVal(a)
	if !ok {
		return 0, false
	}
	if short {
		return float64(hi*16 + hi), true
	}
	lo, ok := hexVal(b)
	if !ok {
		return 0, false
	}
	return float64(hi*16 + lo), true
}

func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

func trimHex(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	return s
}

func srgbLuminance(r, g, b float64) float64 {
	conv := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*conv(r) + 0.7152*conv(g) + 0.0722*conv(b)
}
