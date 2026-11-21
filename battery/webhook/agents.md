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
codec, err := webhook.NewAESGCMSecretCodec(key) // key: 16/24/32 bytes, from your secret store
if err != nil {
    return err
}
store, err := webhook.NewSQLStore(db, webhook.WithSQLSecretCodec(codec))
if err != nil {
    return err
}
mgr := webhook.New(store, webhook.Options{
    MaxAttempts:  6,
    PollInterval: 2 * time.Second,
    // AllowPrivateNetworks left false on purpose, the SSRF guard.
})
mgr.Start()
defer mgr.Stop(context.Background()) // cancels the worker and waits for it to exit

if _, err := mgr.Publish(ctx, "order.shipped", payload); err != nil {
    return err
}

// Optional: fan EventBus events to webhook subscribers automatically.
cancel := webhook.Bridge(app.Events(), mgr, "order.shipped", "user.deleted")
defer cancel()
```

**AI-typical anti-pattern.** If you're about to write any of these,
stop and use `Manager` instead:
- `http.Post(url, "application/json", body)` in a goroutine to
  "deliver the event": no retry, no signing, no dead-letter, no
  visibility when the receiver is down
- `hmac.New(sha256.New, secret); h.Write(body); sig := hex.EncodeToString(h.Sum(nil))`:
  re-invents the signing primitive without timestamp tolerance
  (= replay-attackable)
- A `for i := 0; i < 5; i++ { try(); time.Sleep(...) }` retry loop
- A `subscribers` table you wrote yourself with `url`, `secret`,
  `active` columns and a fan-out loop in the handler

`Manager` handles signing, retry-with-backoff, delivery-state, and
dead-letter parking. Receiver verification uses
`webhook.VerifyTimestamped(secret, sigHeader, eventHeader, deliveryIDHeader, body, 5*time.Minute)`
(returns `bool`); the event and delivery id are part of the signature.
A receiver that already checks its own scheme (say
`X-Acme-Signature: sha256=<hex HMAC of "<ts>.<body>">`) gets it through
`Options.Signature: &webhook.SignatureFormat{...}`; don't sign by hand
in a custom transport.

**Security:**
- Leave `AllowPrivateNetworks: false`. Setting it true allows
  subscribers to point at internal addresses → classic SSRF.
- Use `NewAESGCMSecretCodec` to encrypt subscriber secrets at rest in
  the store (dev may acknowledge plaintext with `WithSQLAllowPlaintext`).
- HMAC signatures are timestamped; receivers MUST check the timestamp
  is within tolerance to defeat replay.
