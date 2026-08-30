// gateb_exchange.mjs: the Gate B cross-check against Node 22's
// WebCrypto (independent Ed25519 implementation).
//
// 1. Verify the signature Go produced over the base Go assembled
//    (read from gateb-exchange.json) — proves Go's signer + Node's
//    verifier agree on the bytes.
// 2. Generate a fresh Ed25519 key in Node, sign the same base, and
//    write node-crosscheck.json — the committed artifact that
//    TestGateB_NodeSignedBase verifies back in Go.
//
// Run: node testdata/gateb_exchange.mjs   (from core/webbotauth/)

import { readFileSync, writeFileSync } from "node:fs";
import { webcrypto } from "node:crypto";

const exchange = JSON.parse(readFileSync(new URL("./gateb-exchange.json", import.meta.url), "utf8"));
const { base, pub_x_b64url: goPub, sig_b64: goSig } = exchange;

const b64url = (s) => Buffer.from(s, "base64url");

// 1. Verify Go's signature over Go's base.
const goKey = await webcrypto.subtle.importKey(
  "raw", b64url(goPub), { name: "Ed25519" }, false, ["verify"],
);
const goOk = await webcrypto.subtle.verify(
  { name: "Ed25519" }, goKey, Buffer.from(goSig, "base64"), Buffer.from(base, "utf8"),
);
console.log(`[gate-b] node verifies go signature: ${goOk}`);
if (!goOk) {
  console.error("[gate-b] FAILED: node could not verify go's signature over go's base");
  process.exit(1);
}

// 2. Node signs the same base with its own key.
const nodeKeyPair = await webcrypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
const rawPub = new Uint8Array(await webcrypto.subtle.exportKey("raw", nodeKeyPair.publicKey));
const nodeSig = new Uint8Array(await webcrypto.subtle.sign(
  { name: "Ed25519" }, nodeKeyPair.privateKey, Buffer.from(base, "utf8"),
));

// Sanity: node verifies its own signature too.
const roundTrip = await webcrypto.subtle.verify(
  { name: "Ed25519" }, nodeKeyPair.publicKey, nodeSig, Buffer.from(base, "utf8"),
);
if (!roundTrip) {
  console.error("[gate-b] FAILED: node could not verify its own signature");
  process.exit(1);
}

const artifact = {
  note: "Signature produced by Node " + process.version + " WebCrypto Ed25519 over the signature base assembled by core/webbotauth (Go). See gates_test.go.",
  base,
  pub_x_b64url: Buffer.from(rawPub).toString("base64url"),
  sig_b64: Buffer.from(nodeSig).toString("base64"),
};
writeFileSync(new URL("./node-crosscheck.json", import.meta.url), JSON.stringify(artifact, null, 2) + "\n");
console.log("[gate-b] wrote testdata/node-crosscheck.json");
