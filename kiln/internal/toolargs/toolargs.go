// Package toolargs is the one decode for a kiln tool call's arguments,
// shared by every transport: the chat panel's HTTP endpoint and
// agent.Dispatch (the native loop, MCP and ACP adapters).
package toolargs

import (
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// Decode decodes body into dst strictly (duplicate and case-folded keys
// are ambiguity, not data) and exactly: an `any` slot (entity
// properties, a field default, page props, action params, seed rows)
// keeps an integer past 2^53 with its digits, where a plain decode
// rounded 9007199254740993 to 9007199254740992 before the value was
// journaled.
func Decode(body []byte, dst any) error {
	if err := handler.UnmarshalStrict(body, dst, handler.UseNumber()); err != nil {
		return err
	}
	return jsonexact.Settle(dst)
}
