# Web Bot Auth (experimental)

Web Bot Auth lets automated HTTP clients identify themselves with
signatures instead of IP lists or User-Agent strings. GoFastr supports
both halves, one stable and one experimental:

- **Publishing** (stable): `WithWebBotAuth` serves the site's own
  signing keys as a JWK Set at
  `/.well-known/http-message-signatures-directory`, so other origins
  can verify requests *this site* sends as a bot. This is one of the
  isitagentready.com production checks.
- **Verification** (experimental): the same option verifies
  RFC 9421 signatures on *inbound* requests, resolves the signer's key
  directory, and exposes the verified identity to handlers.

Verification implements
[draft-meunier-webbotauth-httpsig-protocol-02](https://datatracker.ietf.org/doc/draft-meunier-webbotauth-httpsig-protocol/)
(18 August 2026, the IETF Web Bot Auth working-group draft), pinned
here by name and date the way `webmcp.md` pins its origin trial. The
draft renamed and reorganized itself in mid-2026 and changed
`Signature-Agent` semantics between revisions, so this half of the
option is explicitly experimental: turning it on opts into
draft-tracking, and a revision bump may change verification behaviour.

**The generated SDKs, the app CLI, and `battery/webhook` deliberately
do not sign their outbound requests.** Signing from every generated
artifact would bake draft churn into a breaking-change engine for
every downstream app. Server-side verification has no such blast
radius: it lands only on hosts that turned it on deliberately. When
the protocol stabilizes, signing support can follow without having
been promised.

## Publishing your keys

```go
app := framework.NewApp(
    framework.WithWebBotAuth(framework.WebBotAuthConfig{
        Keys: []map[string]any{
            {
                "kty": "OKP", "crv": "Ed25519", "use": "sig",
                "kid": "<base64url JWK thumbprint of the key>",
                "x":   "<base64url public key bytes>",
            },
        },
    }),
)
```

`kid` should be the RFC 7638 / RFC 8037 thumbprint of the key: verifiers
match `keyid` against it, and a derived `kid` lets them check your
labelling instead of trusting it. Rotation is publishing the new key
alongside the old, then dropping the old one; the URL never changes.

## Verifying inbound requests

```go
app := framework.NewApp(
    framework.WithWebBotAuth(framework.WebBotAuthConfig{
        // Keys is optional here: publish and verify compose freely.
        Verify: &framework.WebBotAuthVerifyConfig{
            Require: false, // observe mode, the default
        },
    }),
)

app.Router().Get("/api/agent-feed", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if agent := framework.VerifiedAgent(r.Context()); agent != nil {
        // agent.URL: the resolved key directory, the protocol's identifier
        // agent.KeyID: the thumbprint that verified the request
    }
```

`Keys` and `Verify` are independent: a site can publish without
verifying, verify without publishing, or both.

### What a signature must satisfy

Ed25519 only (`alg="ed25519"`, OKP keys). Each signature must carry
`tag="web-bot-auth"`, valid `created` and `expires` parameters, a
`keyid` equal to the signing key's JWK thumbprint, coverage of
`@authority` or `@target-uri`, and coverage of the `Signature-Agent`
member it resolves keys from — the legacy bare-string header form is
accepted. The verified identity is the pair (resolved directory URL,
key thumbprint); it says a key that URL published signed the covered
message, nothing more. Authorization, reputation, and rate limits
remain the origin's policy.

### The two modes

| Mode | Behaviour |
|---|---|
| Observe (`Require: false`, default) | Nothing is ever blocked. Verified requests carry the agent identity in context; invalid signatures log a warning. A bug in draft-tracking verification middleware cannot take an app's traffic down. |
| Require (`Require: true`) | Requests that are not verified get `403` with an `Accept-Signature` response naming this profile. Use it on agent-only endpoints; every browser visitor fails the check. |

Verification outcomes follow the draft's three-way split: **verified**,
**invalid** (a check failed — logged), **unverified** (not enough
information, e.g. the directory was unreachable). Only `verified`
yields an identity; `nil` from `VerifiedAgent` covers the other two.

### What the fetcher does on your behalf

Resolving a signer's key directory is a server-side fetch to a
URL taken from an attacker-controlled header, so the fetcher is
hardened and each control is pinned by a test in `core/webbotauth`:

- **https only**, no userinfo, and `directory`-type values must be bare
  origins (the well-known path is derived, not accepted from the
  header). `jwks_uri` fetches the value as sent.
- **Redirects are never followed.** The draft requires non-200 to be a
  discovery failure; this also removes the re-check-per-hop problem.
- **`core/netguard` three times per fetch**: on the literal host, on
  every DNS answer, and — authoritatively — in the dial hook on the
  address actually connected to, which closes DNS-rebinding TOCTOU.
- **256 KiB body cap** (after content decoding), **5 s wall-clock
  timeout**, **32-key cap** per directory.
- **Positive and negative caches are separate bounded LRUs.** Junk
  URLs can only churn the negative cache; a real agent's keys are only
  replaced by a successful refetch. Failures are negative-cached
  (≤ 5 min) so a flood of unresolvable agent URLs cannot force a
  fetch per request. Concurrent lookups for one URL coalesce into a
  single fetch.
- **Rotation**: TTL honours the directory's `Cache-Control` / `Expires`
  (clamped to 1 min–24 h, default 1 h). A successful refetch replaces
  the cached set wholesale, so a removed key stops verifying after at
  most one TTL. A *failed* refetch never evicts: an operator's
  directory outage must not revoke their keys at every verifier at
  once (the draft is explicit), so stale keys keep serving until the
  directory answers again.

## Testing

`core/webbotauth` carries the conformance suite: RFC 9421's Ed25519
test vector (Appendix B.2.6), the draft's own Ed25519 vectors
(Appendix E.2.1–E.2.3, including the legacy header form and the signed
directory response), a cross-check against Node's WebCrypto, and
mutation proofs for every guard. All vectors are committed under
`core/webbotauth/testdata/` with their sources.

## Common mistakes

- **Treating `VerifiedAgent() == nil` as hostile.** Most HTTP traffic
  carries no signature. The draft is explicit that absence is not
  evidence about the sender; block or throttle on your own policy,
  not on the nil.
- **Turning on `Require` for a human-facing surface.** Every browser
  request is unverified and gets a 403. Require mode is for endpoints
  where the caller is supposed to be a signed agent.
- **Assuming a verified agent is benign.** The signature proves which
  key directory published the signing key. What that agent may do on
  your site is still your authorization logic.
- **Publishing a `kid` that is not the key thumbprint.** Verifiers
  match `keyid` against the computed thumbprint; an opaque label makes
  your key unselectable by conformant verifiers.
