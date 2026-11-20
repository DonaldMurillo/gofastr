# Embeddable surfaces (`battery/embed`)

`battery/embed` lets customers embed selected screens of your GoFastr app inside their own websites or portals (for example a pricing widget, status panel, or customer-scoped dashboard) with scoped grants, origin enforcement, and safe iframe sizing.

## Quickstart

1. Mount `battery/embed` on your app and declare which screens may be embedded along with their allowed origins and permissions.
2. Mint a short-lived, subject- and origin-scoped embed grant token on your backend (never a full user session cookie).
3. Drop the embed loader script or iframe snippet onto the host page.

## Security model

Embedding a live app into a third-party site creates clickjacking and session-leak risks if done with plain cookies. `battery/embed` avoids both:

- **Grant header, not cookies:** embedded frames authenticate requests with the `X-Gofastr-Embed` grant header rather than ambient third-party session cookies.
- **Strict scope ceiling:** an embed grant only accesses the explicitly allowed screens and declared API scopes (`access.HeldScopes`). It cannot reach `/admin`, `/print`, `/auth`, or arbitrary CRUD routes outside its grant.
- **Origin pinning:** `Content-Security-Policy: frame-ancestors <allowed-origin>` restricts which host origins may frame the surface; all non-embedded routes keep `frame-ancestors 'none'`.
- **Cache safety:** `battery/cache` bypasses caching whenever `X-Gofastr-Embed` is present so per-grant responses never leak across callers, and `battery/relay` strips `X-Gofastr-*` headers on outbound vendor traffic.
- **Navigation guard:** links inside an embed frame stay within the embed's allowed surface (forms using `localentity` mark `enctype="application/json"` so the embed navigation guard lets them through).

## Sizing and theming

- The embed loader negotiates frame height automatically via `postMessage` so embedded panels don't show awkward double scrollbars.
- Embeds inherit your app's `framework/ui` theme tokens and can opt into light/dark appearance matching the host page.

## Common mistakes

- **Setting `frame-ancestors *` on authenticated embeds:** lets any malicious site frame your authenticated UI. Always pin the customer's exact origin(s).
- **Issuing an embed grant with wildcard (`*:*`) scopes:** keep grants strictly narrowed to the specific entities and read/write verbs the embedded screen needs.
- **Relying on session cookies inside a cross-site iframe:** modern browsers block third-party cookies by default (ITP/SameSite). Use `X-Gofastr-Embed` grants instead.
- **Caching responses keyed only by URL when embed grants are active:** make sure any custom cache checks for `X-Gofastr-Embed` (GoFastr's built-in `CacheMiddleware` already does).
