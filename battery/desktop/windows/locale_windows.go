//go:build windows && amd64

package windows

import (
	"syscall"
	"unsafe"
)

// muiLanguageName asks GetUserPreferredUILanguages for names ("fr-CA"),
// not LCIDs.
const muiLanguageName = 0x8

// preferredLanguages is the user's display-language list from Windows
// settings, best first. A GUI app usually has no LANG, so this, not
// the environment, names the menu language.
func preferredLanguages() []string {
	var num, size uint32
	r, _, _ := callWindows(capKernel, "GetUserPreferredUILanguages", muiLanguageName,
		uintptr(unsafe.Pointer(&num)), 0, uintptr(unsafe.Pointer(&size)))
	if r == 0 || size == 0 {
		return nil
	}
	buf := make([]uint16, size)
	r, _, _ = callWindows(capKernel, "GetUserPreferredUILanguages", muiLanguageName,
		uintptr(unsafe.Pointer(&num)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return nil
	}
	return splitMultiSZ(buf)
}

// splitMultiSZ splits a double-NUL-terminated UTF-16 string list.
func splitMultiSZ(buf []uint16) []string {
	var out []string
	start := 0
	for i, c := range buf {
		if c != 0 {
			continue
		}
		if i == start {
			break
		}
		out = append(out, syscall.UTF16ToString(buf[start:i]))
		start = i + 1
	}
	return out
}
