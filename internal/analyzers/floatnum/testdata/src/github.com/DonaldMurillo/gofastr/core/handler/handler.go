// Package handler stubs the strict decoders floatnum reads.
package handler

import "io"

type StrictOption func()

func UseNumber() StrictOption { return nil }

func UnmarshalStrict(b []byte, v any, opts ...StrictOption) error { return nil }

func DecodeStrict(r io.Reader, v any) error { return nil }
