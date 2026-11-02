// Package repodecoders pins the 2026-11-02 guard bash miss: the rule
// knew the stdlib decoders and not the repo's own strict and lossless
// ones, the spellings CLAUDE.md steers every inbound seam to, nor the
// yaml.v3 Decoder.
package repodecoders

import (
	"io"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
	yaml "gopkg.in/yaml.v3"
)

type args struct{ A int }

func strictBlank(b []byte) args {
	var a args
	_ = handler.UnmarshalStrict(b, &a) // want `handler.UnmarshalStrict error discarded`
	return a
}

func strictBare(r *http.Request) args {
	var a args
	handler.DecodeStrict(r.Body, &a) // want `handler.DecodeStrict error discarded`
	return a
}

func exactBlank(b []byte) map[string]any {
	var m map[string]any
	_ = jsonexact.Unmarshal(b, &m) // want `jsonexact.Unmarshal error discarded`
	return m
}

func yamlStream(r io.Reader) args {
	var a args
	_ = yaml.NewDecoder(r).Decode(&a) // want `Decode error discarded`
	return a
}

// Silent: the checked spelling and the map-index optional-field probe.
func strictChecked(b []byte) (args, error) {
	var a args
	if err := handler.UnmarshalStrict(b, &a); err != nil {
		return args{}, err
	}
	return a, nil
}

func optionalField(m map[string][]byte) int {
	var n int
	_ = jsonexact.Unmarshal(m["n"], &n)
	return n
}
