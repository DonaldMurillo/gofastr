# Outbound webhooks (`battery/webhook`)

`battery/webhook` delivers signed JSON event payloads to customer-registered HTTP endpoints with durable retries, exponential backoff, SSRF protection, and multi-replica lease safety.

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/battery/webhook"

store := webhook.NewSQLStore(db)
dispatcher := webhook.NewDispatcher(store, webhook.Config{
	Concurrency: 4,
})

// Register a customer endpoint subscribed to specific event types:
_, err := store.CreateEndpoint(ctx, webhook.Endpoint{
	URL:    "https://partner.example.com/hooks",
	Secret: signingSecret, // 32-byte random secret for HMAC-SHA256
	Events: []string{"order.created", "order.paid"},
})

// Emit an event (persisted and delivered asynchronously):
err = dispatcher.Dispatch(ctx, "order.paid", map[string]any{"order_id": "ord_123"})
```

Use a SQL-backed `LeasedStore` in production so multiple replicas never deliver the same webhook twice (see [Horizontal scaling](scaling.md)).

## Payload signing and verification

Every outbound request carries an HMAC-SHA256 signature header computed over `<timestamp>.<raw_body>` using the endpoint's secret:

- Customers verify the signature in constant time and reject timestamps older than a few minutes (replay protection).
- Support zero-downtime secret rotation by signing with both the new and retiring secret during the transition window.

## SSRF and network hardening

Because customers supply webhook destination URLs, outbound delivery enforces strict network guards (`core/netguard`):
- **HTTPS required** in production (loopback `http://` only when explicitly enabled for local tests).
- **Private/internal IPs blocked:** RFC 1918, loopback, link-local (including cloud metadata `169.254.169.254`), and IPv6 ULA addresses are rejected at registration **and** in the dial hook after DNS resolution (closing DNS-rebinding TOCTOU).
- **Redirects refused:** `3xx` responses are not followed, preventing redirect-to-localhost bypasses and secret leakage.
- **Timeouts & response caps:** short per-attempt timeouts and small response body caps prevent slowloris or memory-exhaustion attacks from hostile receivers.

## Retries, backoff, and circuit breaking

- Non-2xx responses and transport timeouts retry with exponential backoff + jitter up to `MaxRetries`.
- Each delivery attempt is logged (status code, latency, truncated error) for customer debugging and replay.
- Endpoints that fail continuously can be automatically disabled/paused after consecutive failures so dead customer servers do not starve healthy deliveries.

## Common mistakes

- **Delivering webhooks synchronously inside an HTTP handler or SQL transaction:** a slow customer endpoint stalls your user's request and holds DB locks. Always persist first and deliver asynchronously.
- **Skipping DNS-time SSRF validation:** checking the URL string once at save time does not stop DNS rebinding; rely on GoFastr's built-in dial-time `core/netguard` checks.
- **Following HTTP redirects on webhook POSTs:** an attacker can register `https://ok.example/hook` and 302-redirect it to `http://169.254.169.254/`. Keep redirects disabled.
- **Using an in-memory webhook store across multiple replicas:** switch to `LeasedStore` / SQL storage so deliveries survive restarts and never double-send.
