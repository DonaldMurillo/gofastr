// verify-card.mjs — independent verification of GoFastr's signed A2A
// agent card using Node's WebCrypto (crypto.subtle), an implementation
// that shares no code with the Go stack. This is the Gate B signature
// check for the card-signing feature: our own Go verifier passing means
// nothing; this passing means the JWS/JWKS/JCS construction is real.
//
// Prerequisites:  go run testdata/signing/make-fixture.go
// Run:            node testdata/signing/verify-card.mjs
//
// What it verifies, per A2A v1.0 §8.4 and RFC 7515/7517:
//   1. The JWKS parses and contains ONLY public members.
//   2. Each card signature's protected header carries alg, typ:"JOSE",
//      kid, and jku pointing at the fixture's pinned JWKS URL.
//   3. The signature verifies over
//      ASCII(BASE64URL(protected) "." BASE64URL(canon(card - signatures)))
//      where canon is RFC 8785 JCS (recursively key-sorted JSON.stringify
//      — the construction RFC 8785 Appendix A documents).
//   4. ES256 signatures arrive in JOSE r||s form, which is exactly what
//      WebCrypto expects, so byte-order/format errors would fail here.

import { readFileSync } from "node:fs";

const b64u = (buf) => Buffer.from(buf).toString("base64url");

function canon(v) {
  if (v === null || typeof v !== "object") return JSON.stringify(v);
  if (Array.isArray(v)) return "[" + v.map(canon).join(",") + "]";
  const keys = Object.keys(v).sort();
  return "{" + keys.map((k) => JSON.stringify(k) + ":" + canon(v[k])).join(",") + "}";
}

const card = JSON.parse(readFileSync(new URL("card.json", import.meta.url), "utf8"));
const jwks = JSON.parse(readFileSync(new URL("jwks.json", import.meta.url), "utf8"));
const pinnedJwksURL = "https://card-signing.fixture.test/.well-known/jwks.json";

// ── 1. JWKS hygiene ─────────────────────────────────────────────────
const privateMembers = ["d", "p", "q", "dp", "dq", "qi", "k", "oth"];
for (const jwk of jwks.keys) {
  for (const m of privateMembers) {
    if (m in jwk) {
      console.error(`FAIL: JWKS key ${jwk.kid} carries private member "${m}"`);
      process.exit(1);
    }
  }
}

// ── 2. Rebuild the canonical payload ────────────────────────────────
const signatures = card.signatures;
if (!Array.isArray(signatures) || signatures.length === 0) {
  console.error("FAIL: card has no signatures array");
  process.exit(1);
}
const unsigned = structuredClone(card);
delete unsigned.signatures;
const payload = canon(unsigned);

// ── 3. Verify each signature via WebCrypto ──────────────────────────
for (const sig of signatures) {
  const headerJSON = Buffer.from(sig.protected, "base64url").toString("utf8");
  const header = JSON.parse(headerJSON);
  if (header.typ !== "JOSE") {
    console.error(`FAIL: typ = ${header.typ}, want "JOSE"`);
    process.exit(1);
  }
  if (header.jku !== pinnedJwksURL) {
    console.error(`FAIL: jku = ${header.jku}, want ${pinnedJwksURL}`);
    process.exit(1);
  }
  const jwk = jwks.keys.find((k) => k.kid === header.kid);
  if (!jwk) {
    console.error(`FAIL: kid ${header.kid} not in JWKS`);
    process.exit(1);
  }

  let algorithm, key;
  if (jwk.kty === "EC") {
    algorithm = { name: "ECDSA", namedCurve: jwk.crv, hash: { name: `SHA-${header.alg.slice(2)}` } };
    key = await crypto.subtle.importKey("jwk", jwk, { name: "ECDSA", namedCurve: jwk.crv }, false, ["verify"]);
  } else if (jwk.kty === "OKP" && jwk.crv === "Ed25519") {
    algorithm = { name: "Ed25519" };
    key = await crypto.subtle.importKey("jwk", jwk, { name: "Ed25519" }, false, ["verify"]);
  } else {
    console.error(`FAIL: unsupported JWK kty=${jwk.kty} crv=${jwk.crv}`);
    process.exit(1);
  }

  const input = `${sig.protected}.${b64u(payload)}`;
  const sigBytes = Buffer.from(sig.signature, "base64url");
  const ok = await crypto.subtle.verify(algorithm, key, sigBytes, Buffer.from(input, "utf8"));
  if (!ok) {
    console.error(`FAIL: signature kid=${header.kid} alg=${header.alg} does NOT verify (WebCrypto)`);
    console.error(`      signing input was: ${input.slice(0, 120)}…`);
    process.exit(1);
  }
  console.log(`ok: kid=${header.kid} alg=${header.alg} verified via WebCrypto`);
}

console.log(`ALL ${signatures.length} SIGNATURES VERIFY (WebCrypto, independent implementation)`);
