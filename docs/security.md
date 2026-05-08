# Security Defaults

GoFastr provides defensive middleware in `core/middleware`.

```go
handler := middleware.Chain(
    middleware.RequestID(),
    middleware.Recovery(),
    middleware.SecurityHeaders(middleware.SecurityHeadersConfig{}),
)(router)
```

`SecurityHeaders` sets conservative defaults:

- `Content-Security-Policy`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: no-referrer`
- `X-Frame-Options: DENY`
- `Permissions-Policy`

Applications that embed third-party scripts, frames, images, or fonts should
override `ContentSecurityPolicy` explicitly instead of weakening it globally.
