// Package wstest builds raw WebSocket client fixtures for the tests of
// the framework's WebSocket-adjacent packages (harness/control/ws and
// the harness end-to-end suite). Tests only: nothing outside a _test.go
// file imports it, so it never links into a binary.
package wstest

import "encoding/binary"

// MaskedTextFrame builds a client-side text frame with a 4-byte mask
// (required by RFC 6455 for client→server frames).
func MaskedTextFrame(payload []byte) []byte {
	mask := []byte{0xAB, 0xCD, 0xEF, 0x12}
	header := []byte{0x80 | 0x1} // FIN + text opcode
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload))|0x80) // mask bit
	case len(payload) <= 0xFFFF:
		header = append(header, 126|0x80)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(len(payload)))
		header = append(header, ext[:]...)
	default:
		header = append(header, 127|0x80)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(len(payload)))
		header = append(header, ext[:]...)
	}
	header = append(header, mask...)
	body := make([]byte, len(payload))
	for i := range payload {
		body[i] = payload[i] ^ mask[i%4]
	}
	return append(header, body...)
}
