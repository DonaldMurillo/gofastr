// Package handler is the stub of core/handler's strict decoders.
package handler

import "io"

type StrictOption func()

func UnmarshalStrict(data []byte, dst any, opts ...StrictOption) error { return nil }

func DecodeStrict(r io.Reader, dst any) error { return nil }
