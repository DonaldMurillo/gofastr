# Web Bot Auth (experimental)

Web Bot Auth lets automated HTTP clients prove their identity with cryptographic signatures (RFC 9421) instead of spoofable `User-Agent` strings or brittle IP allowlists.

GoFastr supports both halves independently via `framework.WithWebBotAuth`:
- **Publishing (stable):** serves your public Ed25519 JWK Set at `/.well-known/http-message-signatures-directory` so other origins can verify requests your bots send.
- **Verification (experimental):** tracks IETF draft `draft-meunier-webbotauth-httpsig-protocol-02` to verify inbound Ed25519 signatures and expose the verified agent identity to handlers. (Generated SDKs, CLIs, and `battery/webhook` do not sign outbound requests while the draft evolves.)

## Publishing your keys

```go
app := framework.NewApp(
	framework.WithWebBotAuth(framework.WebBotAuthConfig{
		Keys: []map[string]any{
			{
				"kty": "OKP", "crv": "Ed25519", "use": "sig",
				"kid": "<base64url JWK thumbprint>",
				"x":   "<base64url public key>",
			},
		},
	}),
)
```

Use the RFC 7638 / RFC 8037 JWK thumbprint as `kid`. Rotate keys by publishing the new key beside the old one, then removing the old key after clients refresh.

## Verifying inbound requests

```go
app := framework.NewApp(
	framework.WithWebBotAuth(framework.WebBotAuthConfig{
		Verify: &framework.WebBotAuthVerifyConfig{
			Require: false, // observe mode (default); true returns 403 on unverified
		},
	}),
)

app.Router().Get("/api/agent-feed", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if agent := framework.VerifiedAgent(r.Context()); agent != nil {
		// agent.URL: resolved key directory URL
		// agent.KeyID: verified JWK thumbprint
	}
}))
```

Signature rules:
- Ed25519 (`alg="ed25519"`), `tag="web-bot-auth"`, valid `created`/`expires` window bounded by `MaxValidity` (default 5 minutes), `keyid` matching the JWK thumbprint, covering `@authority` or `@target-uri` plus `Signature-Agent`.
- **Observe mode (`Require: false`, default):** never blocks traffic; populates `VerifiedAgent(ctx)` when valid and logs invalid signatures.
- **Require mode (`Require: true`):** rejects unverified requests with `403` and an `Accept-Signature` header (use only on agent-only endpoints, never browser routes).

## Per-agent rate limits

Pair `framework.AgentRateLimitKey` with `framework/ratelimit` behind an outer IP-keyed ceiling so verified agents on shared egress IPs get fair budgets without letting one sender mint unlimited identities:

```go
app.Router().Use(perIP.MiddlewareByKey(nil))
app.Router().Use(agentBudget.MiddlewareByKey(framework.AgentRateLimitKey(false)))
```

Key directory fetches are hardened by `core/netguard` (HTTPS-only, no redirects, DNS + dial-time IP checks against SSRF, 256 KiB cap, 5s timeout, 32-key cap, bounded positive/negative LRU caches, and concurrent fetch limits).

## Common mistakes

- **Treating `VerifiedAgent(ctx) == nil` as malicious:** normal browsers and unsigned clients are unverified; apply your own policy.
- **Enabling `Require: true` on human-facing routes:** every browser request will receive `403`.
- **Equating a verified signature with authorization:** Web Bot Auth proves *who signed the request*, not what they are permitted to do.
- **Setting an arbitrary string as `kid`:** conformant verifiers match `keyid` against the computed JWK thumbprint.
