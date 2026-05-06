package email

import (
	"encoding/base64"
	"strings"
)

// b64Encode encodes data as base64 with 76-character line wrapping.
func b64Encode(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	return insertLineBreaks(encoded, 76)
}

// insertLineBreaks inserts \r\n every n characters.
func insertLineBreaks(s string, n int) string {
	if len(s) <= n {
		return s
	}

	var sb strings.Builder
	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		sb.WriteString(s[i:end])
		if end < len(s) {
			sb.WriteString("\r\n")
		}
	}
	return sb.String()
}
