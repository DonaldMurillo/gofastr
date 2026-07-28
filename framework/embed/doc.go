// Package embed lets a GoFastr app hand out pieces of itself.
//
// An app author marks a screen or island embeddable and names the exact
// origins allowed to frame it. Their customer pastes one <script> tag into an
// unrelated website and gets a live, themed, authenticated piece of the app.
//
// # Delivery
//
// An iframe, loaded by a small script. Inside the frame GoFastr is same-origin
// with itself, so the runtime's origin guards, its same-origin fetches, and its
// ownership of the document all hold unchanged. That is the reason this shape
// is tractable: nothing about the runtime has to be relaxed to make it work.
//
// # The cookie fact
//
// Inside the frame the session cookie is never sent, even though the frame is
// same-origin with the app. SameSite is computed against the top-level browsing
// context and the full ancestor chain; the top level is the customer's site, so
// every request from inside the frame is a cross-site context. GoFastr's session
// cookie is SameSite=Strict and the CSRF cookie is Lax; neither is sent.
//
// Identity can therefore only arrive explicitly, which makes CSRF against embed
// routes structurally impossible. Embed routes go further and REJECT cookies
// rather than merely not requiring them — a route that honours a cookie when one
// happens to be present would hand a signed-in user's full session to a third
// party's frame.
//
// # The credential
//
// A single-use handshake nonce exchanged for a stateless grant.
//
// The app author mints a nonce server-side for one specific viewer
// ([Host.MintNonce]) and renders it into the embed snippet. The nonce is an
// HMAC over (surface, subject, scopes, origin, nonce id, expiry) — nothing is
// stored at mint time, so minting scales like signing.
//
// Only the exchange touches a store: the nonce id is INSERTed against a unique
// constraint, and the constraint violation is what "already used" means. That is
// atomic across replicas with no read-then-write race, the same shape as the
// migration and seed locks.
//
// Single use exists to make a SHARED token impossible. The predictable customer
// failure with a time-window token is hardcoding one into a page template, so
// every visitor arrives as the same identity — and nothing about a TTL prevents
// that. Replay defence comes along for free.
//
// A browser has several ways to fire the exchange twice (the customer's page
// prefetches the iframe, a dev double-mounts the loader, a user refreshes), so
// the exchange is POST-only and idempotent within the grant's lifetime: a repeat
// of the same nonce returns the same grant instead of failing. Without that, the
// feature surfaces as "the embed randomly doesn't load".
//
// # Origins
//
// Exact origins only, no wildcards — every subdomain is listed separately.
// Origins are compared NORMALIZED, not as strings: https://acme.com,
// https://acme.com/, https://acme.com:443 and https://ACME.com are one origin
// and four strings, and a customer's trailing slash would otherwise silently
// never match.
//
// The browser-enforced control is the embed document's CSP frame-ancestors
// directive, which lists every allowed origin. It has to list them all: no
// Origin header is sent on a navigation GET, so at the moment the header is
// written the server does not know who is framing it. Listing ten origins does
// not let an eleventh frame the page — the browser enforces against the real
// ancestor chain. The only cost is that the allowlist is public to anyone who
// fetches the embed URL.
//
// # Runtime
//
// The frame gets its own runtime composition (kernel + rpc + signals +
// widgets-boot + boot + boot-embed) which omits the nav fragment. That is how
// SPA navigation is disabled inside frames: by absence, so no config mistake and
// no later refactor can re-enable it.
package embed
