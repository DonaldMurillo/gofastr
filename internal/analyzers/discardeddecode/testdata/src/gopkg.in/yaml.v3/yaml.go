// Package yaml is the stub of gopkg.in/yaml.v3's decoder.
package yaml

import "io"

type Decoder struct{}

func NewDecoder(r io.Reader) *Decoder { return &Decoder{} }

func (d *Decoder) Decode(v any) error { return nil }
