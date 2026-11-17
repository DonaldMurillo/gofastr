package image

import (
	"encoding/binary"

	"github.com/DonaldMurillo/gofastr/framework/internal/exif"
)

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
		if marker == 0xFF {
			i++
			continue
		}
		i += 2
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
