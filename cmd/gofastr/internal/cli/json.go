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

// BeginJSON reserves stdout for one JSON document until end runs.
// It swaps the os.Stdout global and is not safe for concurrent use.
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

// JSONWriter returns the stdout a JSON session reserved, or os.Stdout without one;
// an override plugin writes its JSON document there.
func JSONWriter() *os.File {
	if currentJSON != nil && currentJSON.writer != nil {
		return currentJSON.writer
	}
	return os.Stdout
}

// InJSONSession reports whether BeginJSON is currently active.
func InJSONSession() bool {
	return currentJSON != nil
}

// EmitJSON marshals v and writes it as EmitRawJSON does.
func EmitJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	return EmitRawJSON(data)
}

// EmitRawJSON writes data, which must be one JSON value, as the session's document.
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
