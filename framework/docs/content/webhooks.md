# Outbound webhooks (`battery/webhook`)

`battery/webhook` delivers signed JSON event payloads to customer-registered HTTP endpoints with durable retries, configurable backoff, SSRF protection, and multi-replica lease claiming.

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/battery/webhook"

store, err := webhook.NewSQLStore(db)
if err != nil {
	return err
}
mgr := webhook.New(store, webhook.Options{
	MaxAttempts: 6,
})

// Register a customer subscriber for specific event types:
_, err = mgr.Subscribe(ctx, webhook.Subscriber{
	URL:    "https://partner.example.com/hooks",
	Secret: signingSecret, // 32-byte random secret for HMAC-SHA256
	Events: []string{"order.created", "order.paid"},
})

// Start background delivery workers (call mgr.Stop(ctx) on shutdown):
mgr.Start()

// Emit an event (persisted and delivered asynchronously; use PublishFor when scoped to a user):
_, err = mgr.Publish(ctx, "order.paid", []byte(`{"order_id":"ord_123"}`))
```

Use a SQL-backed `LeasedStore` in production so multiple replicas share a single durable queue and claim deliveries with leases (see [Horizontal scaling](scaling.md)). Delivery is **at-least-once**: a crash or lease expiry after an endpoint receives a request can cause a retry, so receivers must deduplicate by the delivery ID header.

## Payload signing and verification

Every outbound request carries a timestamped HMAC-SHA256 signature header (`t=<unix>,v1=<hex>`) computed over:

```text
<timestamp>\n<event>\n<delivery_id>\n<raw_body>
```

- Receivers verify the signature in constant time (or with `webhook.VerifyTimestamped`) using the event name and delivery ID headers, rejecting timestamps outside their tolerance window (`webhook.DefaultTimestampTolerance`, 5 minutes) and deduplicating by delivery ID.
- The send path signs each delivery with the subscriber's single configured `Secret` (it does not dual-sign with two secrets automatically). If you rotate secrets, coordinate a receiver-side overlap window that accepts both old and new secrets before switching the subscriber's `Secret`.

## SSRF and network hardening

Because customers supply webhook destination URLs, outbound delivery enforces strict network guards (`core/netguard`):
- **HTTPS required** in production (loopback `http://` only when explicitly enabled for local tests).
- **Private/internal IPs blocked:** RFC 1918, loopback, link-local (including cloud metadata `169.254.169.254`), and IPv6 ULA addresses are rejected at registration **and** in the dial hook after DNS resolution (closing DNS-rebinding TOCTOU).
- **Redirects refused:** `3xx` responses are not followed, preventing redirect-to-localhost bypasses and secret leakage.
- **Timeouts & response caps:** short per-attempt timeouts and small response body caps prevent slowloris or memory-exhaustion attacks from hostile receivers.

## Retries, backoff, and circuit breaking

- Non-2xx responses and transport timeouts retry up to `Options.MaxAttempts` (default `6`) using the `Options.Backoff` duration schedule (defaults: `30s`, `1m`, `5m`, `15m`, `1h`, `3h`).
- Each delivery attempt is logged (status code, latency, truncated error) for customer debugging and replay.
- Endpoints that fail continuously can be paused/disabled so dead customer servers do not starve healthy deliveries.

## Common mistakes

- **Assuming exactly-once delivery:** webhooks are **at-least-once**. Receivers must make handlers idempotent and dedupe by the delivery ID header.
- **Delivering webhooks synchronously inside an HTTP handler or SQL transaction:** a slow customer endpoint stalls your user's request and holds DB locks. Always persist first and deliver asynchronously.
- **Skipping DNS-time SSRF validation:** checking the URL string once at save time does not stop DNS rebinding; rely on GoFastr's built-in dial-time `core/netguard` checks.
- **Following HTTP redirects on webhook POSTs:** an attacker can register `https://ok.example/hook` and 302-redirect it to `http://169.254.169.254/`. Keep redirects disabled.
- **Using an in-memory webhook store across multiple replicas:** switch to `NewSQLStore` (`LeasedStore`) so deliveries survive restarts and replicas claim deliveries cleanly.
