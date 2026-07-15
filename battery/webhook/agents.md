# battery/webhook

Outbound webhooks with HMAC signing, retry-with-exponential-backoff,
dead-letter parking, lease-based worker claim, and an optional bridge
that auto-fans `framework/event` events to subscribers.

**Use this when** the prompt mentions: webhook, "tell our integration
partner", signed callback, deliver-event, subscribe-to-event,
integrations endpoint, "POST to a customer URL", retry on failure.

**Import:** `github.com/DonaldMurillo/gofastr/battery/webhook`

**Shape:**
```go
store := webhook.NewSQLStore(db)
mgr := webhook.New(store, webhook.Options{
    MaxAttempts:        6,
    PollInterval:       2 * time.Second,
    SignatureTolerance: 5 * time.Minute,
    // AllowPrivateNetworks left false on purpose — SSRF guard.
})
mgr.Start()
defer mgr.Stop(context.Background()) // bounds the drain; returns error

n, err := mgr.Publish(ctx, "order.shipped", payload)
_, _ = n, err // n = number of subscribers the event fanned out to

// Optional: fan EventBus events to webhook subscribers automatically.
cancel := webhook.Bridge(app.Events(), mgr, "order.shipped", "user.deleted")
defer cancel()
```

**AI-typical anti-pattern** — if you're about to write any of these,
stop and use `Manager` instead:
- `http.Post(url, "application/json", body)` in a goroutine to
  "deliver the event" — no retry, no signing, no dead-letter, no
  visibility when the receiver is down
- `hmac.New(sha256.New, secret); h.Write(body); sig := hex.EncodeToString(h.Sum(nil))`
  — re-invents the signing primitive without timestamp tolerance
  (= replay-attackable)
- A `for i := 0; i < 5; i++ { try(); time.Sleep(...) }` retry loop
- A `subscribers` table you wrote yourself with `url`, `secret`,
  `active` columns and a fan-out loop in the handler

`Manager` handles signing, retry-with-backoff, delivery-state, and
dead-letter parking. Receiver verification uses
`webhook.VerifyTimestamped(secret, sigHeader, body, 5*time.Minute)`
(returns `bool`).

**Security:**
- Leave `AllowPrivateNetworks: false`. Setting it true allows
  subscribers to point at internal addresses → classic SSRF.
- Use `AESGCMSecretCodec` to encrypt subscriber secrets at rest in
  the store. The plaintext secret never leaves memory after that.
- HMAC signatures are timestamped; receivers MUST check the timestamp
  is within tolerance to defeat replay.
