package file

import (
	"fmt"
	"net/http"
	"unicode/utf16"
	"unicode/utf8"
)

type wideText struct {
	width int
	big   bool
	start int
}

func detectWideText(b []byte) (wideText, bool) {
	switch {
	case len(b) >= 4 && b[0] == 0x00 && b[1] == 0x00 && b[2] == 0xFE && b[3] == 0xFF:
		return wideText{width: 4, big: true, start: 4}, true
	case len(b) >= 4 && b[0] == 0xFF && b[1] == 0xFE && b[2] == 0x00 && b[3] == 0x00:
		return wideText{width: 4, big: false, start: 4}, true
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return wideText{width: 2, big: true, start: 2}, true
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return wideText{width: 2, big: false, start: 2}, true
	case len(b) >= 8 && b[0] == '<' && b[1] == 0x00 && b[2] == 0x00 && b[3] == 0x00 &&
		b[4] != 0x00 && b[4] < 0x80 && b[5] == 0x00 && b[6] == 0x00 && b[7] == 0x00:
		return wideText{width: 4, big: false, start: 0}, true
	case len(b) >= 8 && b[0] == 0x00 && b[1] == 0x00 && b[2] == 0x00 && b[3] == '<' &&
		b[4] == 0x00 && b[5] == 0x00 && b[6] == 0x00 && b[7] != 0x00 && b[7] < 0x80:
		return wideText{width: 4, big: true, start: 0}, true
	case len(b) >= 4 && b[0] == '<' && b[1] == 0x00 && b[2] != 0x00 && b[2] < 0x80 && b[3] == 0x00:
		return wideText{width: 2, big: false, start: 0}, true
	case len(b) >= 4 && b[0] == 0x00 && b[1] == '<' && b[2] == 0x00 && b[3] != 0x00 && b[3] < 0x80:
		return wideText{width: 2, big: true, start: 0}, true
	}
	return wideText{}, false
}

func (w wideText) unit(b []byte, i int) uint32 {
	if w.width == 2 {
		if w.big {
			return uint32(b[i])<<8 | uint32(b[i+1])
		}
		return uint32(b[i]) | uint32(b[i+1])<<8
	}
	if w.big {
		return uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
	}
	return uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16 | uint32(b[i+3])<<24
}

func (w wideText) containsFold(body, needle []byte) bool {
	span := len(needle) * w.width
	for i := w.start; i+span <= len(body); i += w.width {
		match := true
		for j := range needle {
			u := w.unit(body, i+j*w.width)
			if u >= 0x80 {
				match = false
				break
			}
			a, b := byte(u), needle[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
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

func (w wideText) decodeHead(body []byte) []byte {
	end := min(len(body), w.start+512*w.width)
	out := make([]byte, 0, 512)
	for i := w.start; i+w.width <= end; i += w.width {
		u := w.unit(body, i)
		r := rune(u)
		if w.width == 2 && utf16.IsSurrogate(r) {
			r = utf8.RuneError
			if i+2*w.width <= len(body) {
				if d := utf16.DecodeRune(rune(u), rune(w.unit(body, i+w.width))); d != utf8.RuneError {
					r = d
					i += w.width
				}
			}
		}
		if u > utf8.MaxRune {
			r = utf8.RuneError
		}
		out = utf8.AppendRune(out, r)
	}
	return out
}

func rejectWideText(data []byte) error {
	w, ok := detectWideText(data)
	if !ok {
		return nil
	}
	for _, tok := range hardActiveTokens {
		if w.containsFold(data, tok) {
			return fmt.Errorf("%w: HTML/XML/SVG content (wide text)", ErrFileFieldUnsafeContent)
		}
	}
	head := w.decodeHead(data)
	trim := trimLeadingSpace(head)
	for _, tok := range softActiveTokens {
		if containsTokenFold(trim, tok) {
			return fmt.Errorf("%w: HTML/XML/SVG content (wide text)", ErrFileFieldUnsafeContent)
		}
	}
	if ct := mimeBase(http.DetectContentType(head)); isMarkupMIME(ct) {
		return fmt.Errorf("%w: %s (wide text)", ErrFileFieldUnsafeContent, ct)
	}
	return nil
}
