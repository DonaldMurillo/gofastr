package image

import (
	"encoding/binary"

	"github.com/DonaldMurillo/gofastr/framework/internal/exif"
)

// readJPEGOrientation scans a JPEG byte stream for an APP1 (Exif) marker
// and returns the EXIF orientation tag (1..8) when present, or 0 when
// absent or malformed. Only the orientation tag is parsed. Full EXIF
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
		return exif.ParseTIFFOrientation(seg[6:])
	}
	return 0
}
