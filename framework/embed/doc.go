// Package embed lets a GoFastr app hand out pieces of itself: a surface that
// listed origins may frame. The app mints a single-use nonce for one viewer
// ([Host.MintNonce]), the frame exchanges it for a refreshable grant
// ([Host.Exchange]), and [Host.Middleware] authenticates requests that carry it.
// The rules and their reasons are in the registry: go run ./cmd/primitives show embed.
package embed
