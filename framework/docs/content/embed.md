# Embeddable surfaces (`framework/embed`)

`framework/embed` (mounted via `uihost.WithEmbed`) lets customers embed selected screens of your GoFastr app inside their own websites or portals (for example a pricing widget, status panel, or customer-scoped dashboard) with scoped grants, origin enforcement, and safe iframe sizing.

## Quickstart

1. Build an embed host with `framework/embed.New(...)` declaring signing keys, allowed surfaces, allowed origins, and scopes, and pass it to `uihost.New(site, uihost.WithEmbed(embedHost))`. (Until signing keys are configured, embed routes answer `503`.)
2. On the host page's backend, mint a **fresh per-viewer handshake nonce** bound to that subject, surface, and origin (never a full user session cookie).
3. Render the embed loader/iframe on an uncached (`Cache-Control: private, no-store`) host page; the frame exchanges the nonce for a scoped `X-Gofastr-Embed` grant token.

## Security model

Embedding a live app into a third-party site creates clickjacking and session-leak risks if done with plain cookies. `framework/embed` avoids both:

- **Grant header, not cookies:** embedded frames exchange their handshake nonce for a signed `X-Gofastr-Embed` grant header rather than using ambient third-party session cookies.
- **Never share-cache the host page carrying a nonce:** replaying the same handshake nonce yields the same grant. Even though grant-bearing requests bypass caches, putting a rendered nonce in a shared CDN/proxy cache lets multiple visitors exchange it and act as the same subject. Always mint a fresh nonce per viewer render and serve the host page with `Cache-Control: private, no-store`.
- **Strict scope ceiling:** an embed grant only accesses the explicitly allowed screens and declared API scopes (`access.HeldScopes`). It cannot reach `/admin`, `/print`, `/auth`, or arbitrary CRUD routes outside its grant.
- **Origin pinning:** `Content-Security-Policy: frame-ancestors <allowed-origin>` restricts which host origins may frame the surface; all non-embedded routes keep `frame-ancestors 'none'`.
- **Cache safety:** `battery/cache` bypasses caching whenever `X-Gofastr-Embed` is present so per-grant responses never leak across callers, and `battery/relay` strips `X-Gofastr-*` headers on outbound vendor traffic.
- **Navigation guard:** links inside an embed frame stay within the embed's allowed surface (forms using `localentity` mark `enctype="application/json"` so the embed navigation guard lets them through).

## Sizing and theming

- The embed loader negotiates frame height automatically via `postMessage` so embedded panels don't show awkward double scrollbars.
- Embeds inherit your app's `framework/ui` theme tokens and can opt into light/dark appearance matching the host page.

## Common mistakes

- **Caching the host page that renders an embed nonce:** a shared CDN/proxy cache hands the same nonce to multiple visitors, letting them all mint the same subject's grant. Mint a fresh nonce per viewer render on a `no-store` page.
- **Setting `frame-ancestors *` on authenticated embeds:** lets any malicious site frame your authenticated UI. Always pin the customer's exact origin(s).
- **Issuing an embed grant with wildcard (`*:*`) scopes:** keep grants strictly narrowed to the specific entities and read/write verbs the embedded screen needs.
- **Relying on session cookies inside a cross-site iframe:** modern browsers block third-party cookies by default (ITP/SameSite). Use `framework/embed` nonces and `X-Gofastr-Embed` grants instead.
- **Caching responses keyed only by URL when embed grants are active:** make sure any custom cache checks for `X-Gofastr-Embed` (GoFastr's built-in `CacheMiddleware` already does).
