package exif

import (
	"encoding/binary"
	"testing"
)

func tiff(order binary.ByteOrder, tag, typ uint16, count uint32, value uint16) []byte {
	b := make([]byte, 8+2+12)
	if order == binary.LittleEndian {
		copy(b, "II\x2a\x00")
	} else {
		copy(b, "MM\x00\x2a")
	}
	order.PutUint32(b[4:], 8)
	order.PutUint16(b[8:], 1)
	e := b[10:]
	order.PutUint16(e[0:], tag)
	order.PutUint16(e[2:], typ)
	order.PutUint32(e[4:], count)
	order.PutUint16(e[8:], value)
	return b
}

func TestParseTIFFOrientation(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want int
	}{
		{"little endian", tiff(binary.LittleEndian, 0x0112, 3, 1, 6), 6},
		{"big endian", tiff(binary.BigEndian, 0x0112, 3, 1, 8), 8},
		{"other tag", tiff(binary.LittleEndian, 0x0100, 3, 1, 6), 0},
		{"not SHORT", tiff(binary.LittleEndian, 0x0112, 4, 1, 6), 0},
		{"count two", tiff(binary.LittleEndian, 0x0112, 3, 2, 6), 0},
		{"value 9", tiff(binary.LittleEndian, 0x0112, 3, 1, 9), 0},
		{"value 0", tiff(binary.LittleEndian, 0x0112, 3, 1, 0), 0},
		{"bad magic", append([]byte("IIxx"), tiff(binary.LittleEndian, 0x0112, 3, 1, 6)[4:]...), 0},
		{"entries past the end", tiff(binary.LittleEndian, 0x0112, 3, 1, 6)[:20], 0},
		{"short", []byte("II*"), 0},
	} {
		if got := ParseTIFFOrientation(tc.data); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}
