package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"unicode/utf16"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

type jsonSession struct {
	writer  *os.File
	emitted bool
}

var currentJSON *jsonSession

// BeginJSON reserves the current stdout for one JSON document and redirects
// ordinary output to stderr until end runs. Nested calls share that session.
// Like the command dispatcher and stdout capture helpers, it is not concurrent.
func BeginJSON() (end func()) {
	if currentJSON != nil {
		return func() {}
	}
	session := &jsonSession{writer: os.Stdout}
	currentJSON = session
	os.Stdout = os.Stderr
	return func() {
		if currentJSON == session {
			os.Stdout = session.writer
			currentJSON = nil
		}
	}
}

// EmitJSON marshals v before writing the session's only document. Without a
// session it writes to stdout. A second document is refused without writing.
func EmitJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	return EmitRawJSON(data)
}

// EmitRawJSON validates one complete JSON value and writes one trailing newline.
// A write attempt consumes the session even if its writer reports an error.
func EmitRawJSON(data []byte) error {
	if !json.Valid(data) {
		return errors.New("output is not one JSON document")
	}
	writer := os.Stdout
	if currentJSON != nil {
		if currentJSON.emitted {
			return errors.New("JSON document already emitted")
		}
		currentJSON.emitted = true
		writer = currentJSON.writer
	}
	var document bytes.Buffer
	document.Grow(len(data) + 1)
	for _, character := range string(bytes.TrimSpace(data)) {
		switch {
		case character == '\r':
			document.WriteByte(' ')
		case character != '\n' && character != '\t' && textsafe.IsUnsafe(character):
			if character > 0xffff {
				high, low := utf16.EncodeRune(character)
				fmt.Fprintf(&document, `\u%04x\u%04x`, high, low)
			} else {
				fmt.Fprintf(&document, `\u%04x`, character)
			}
		default:
			document.WriteRune(character)
		}
	}
	document.WriteByte('\n')
	_, err := writer.Write(document.Bytes())
	return err
}
