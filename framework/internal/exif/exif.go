// Package exif reads the orientation tag of an EXIF TIFF block, for framework/image and framework/file.
package exif

import "encoding/binary"

// ParseTIFFOrientation returns IFD0's orientation tag (1 to 8), or 0 when it is absent or malformed.
func ParseTIFFOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I' && tiff[2] == 0x2A && tiff[3] == 0x00:
		order = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M' && tiff[2] == 0x00 && tiff[3] == 0x2A:
		order = binary.BigEndian
	default:
		return 0
	}
	ifd0Offset := int(order.Uint32(tiff[4:]))
	if ifd0Offset < 8 || ifd0Offset+2 > len(tiff) {
		return 0
	}
	numEntries := int(order.Uint16(tiff[ifd0Offset:]))
	entries := tiff[ifd0Offset+2:]
	if numEntries*12 > len(entries) {
		return 0
	}
	for n := range numEntries {
		e := entries[n*12 : n*12+12]
		tag := order.Uint16(e[0:2])
		if tag != 0x0112 {
			continue
		}
		typ := order.Uint16(e[2:4])
		count := order.Uint32(e[4:8])
		if typ != 3 || count != 1 {
			return 0
		}
		v := int(order.Uint16(e[8:10]))
		if v < 1 || v > 8 {
			return 0
		}
		return v
	}
	return 0
}
