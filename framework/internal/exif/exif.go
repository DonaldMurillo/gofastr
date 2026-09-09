// Package exif holds the leaf EXIF readers shared by the framework's
// image-adjacent packages. It exists so framework/file (which must not
// link the image codecs, see the layering rule pinned by
// framework/crud's TestUploadPathDoesNotLinkImageCodecs) and
// framework/image can share one TIFF walker without either importing
// the other: it is a leaf with no gofastr imports, sitting beside
// internal/casing at L1.
package exif

import "encoding/binary"

// ParseTIFFOrientation reads a TIFF stream and returns the orientation
// tag (0x0112) from IFD0, or 0 if absent. Only the orientation tag is
// parsed; full EXIF support is intentionally out of scope.
//
// It replaces the two byte-identical copies framework/file/strip.go
// (parseTIFFOrientation, formerly a leaf-local twin kept because file
// could not import framework/image) and framework/image/exif.go
// (parseTIFFOrientation, readJPEGOrientation's IFD walker).
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
		if typ != 3 /* SHORT */ || count != 1 {
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
