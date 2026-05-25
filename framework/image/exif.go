package image

import "encoding/binary"

// readJPEGOrientation scans a JPEG byte stream for an APP1 (Exif) marker
// and returns the EXIF orientation tag (1..8) when present, or 0 when
// absent or malformed. Only the orientation tag is parsed — full EXIF
// support is intentionally out of scope here.
//
// The JPEG file structure is: 0xFFD8 (SOI), a sequence of markers of the
// form 0xFFxx [len:2] [data:len-2], then 0xFFD9 (EOI). APP1 is 0xFFE1.
// Inside APP1 the payload starts with the ASCII identifier "Exif\x00\x00"
// followed by a TIFF stream we then walk.
func readJPEGOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0
	}
	i := 2
	for i+4 < len(data) {
		if data[i] != 0xFF {
			return 0
		}
		marker := data[i+1]
		i += 2
		// Stand-alone markers without a length field.
		if marker == 0x00 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD9) {
			continue
		}
		if i+2 > len(data) {
			return 0
		}
		segLen := int(binary.BigEndian.Uint16(data[i:]))
		if segLen < 2 || i+segLen > len(data) {
			return 0
		}
		seg := data[i+2 : i+segLen]
		i += segLen
		if marker != 0xE1 {
			continue
		}
		if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
			continue
		}
		return parseTIFFOrientation(seg[6:])
	}
	return 0
}

// parseTIFFOrientation reads a TIFF stream and returns the orientation tag
// (0x0112) from IFD0, or 0 if absent.
func parseTIFFOrientation(tiff []byte) int {
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
	for n := 0; n < numEntries; n++ {
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
