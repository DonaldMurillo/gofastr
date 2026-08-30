// Deterministic corpus generator for the core/jcs independent-implementation
// cross-check (Gate B). Run:  node gen-corpus.mjs
//
// Writes:
//   corpus.json         — array of JSON values (the corpus)
//   corpus-expected.txt — line N = hex of the canonical form of corpus[N]
//
// The reference canonicalizer is the construction RFC 8785 Appendix A itself
// documents: recursively key-sorted JSON.stringify. Node's JSON.stringify
// serializes numbers via ECMAScript Number::toString and strings with the
// same minimal escape set JCS mandates, so a key-sorted stringify is a JCS
// reference implementation sharing no code with the Go package.
//
// The corpus deliberately avoids unpaired-surrogate strings: the Go package
// must reject them (RFC 8785 §3.2.2.2), and JSON.stringify would escape
// them rather than error, so they are pinned as Go unit-test error cases
// instead of corpus values.

import { writeFileSync } from "node:fs";

// ── reference canonicalizer (RFC 8785 Appendix A shape) ─────────────
function canon(v) {
  if (v === null || typeof v !== "object") return JSON.stringify(v);
  if (Array.isArray(v)) return "[" + v.map(canon).join(",") + "]";
  const keys = Object.keys(v).sort(); // default sort = UTF-16 code units
  return "{" + keys.map((k) => JSON.stringify(k) + ":" + canon(v[k])).join(",") + "}";
}

// ── deterministic PRNG (mulberry32) ─────────────────────────────────
function mulberry32(seed) {
  let a = seed >>> 0;
  return function () {
    a = (a + 0x6d2b79f5) | 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const rnd = mulberry32(0x8785a2a);

function randomDouble() {
  // Random IEEE 754 bit patterns; resample NaN/Inf (not valid JSON).
  for (;;) {
    const hi = (rnd() * 4294967296) >>> 0;
    const lo = (rnd() * 4294967296) >>> 0;
    const buf = new ArrayBuffer(8);
    new Uint32Array(buf)[0] = lo;
    new Uint32Array(buf)[1] = hi;
    const f = new Float64Array(buf)[0];
    if (Number.isFinite(f)) return f;
  }
}

// ── corpus ──────────────────────────────────────────────────────────
const corpus = [];

// 1. Hand-built hostile values (keys where UTF-16 order diverges from
//    UTF-8 byte order: supplementary-plane chars vs U+E000–U+FFFF).
corpus.push({
  "\u20ac": "Euro Sign",
  "\r": "Carriage Return",
  "\n": "Newline",
  "1": "One",
  "\u0080": "Control\u007f",
  "\ud83d\ude02": "Smiley",
  "\u00f6": "Latin Small Letter O With Diaeresis",
  "\ufb33": "Hebrew Letter Dalet With Dagesh",
  "</script>": "Browser Challenge",
});
corpus.push({
  "\ud83d\ude00": "grinning (D83D DE00)",
  "\ue000": "private use (E000)",
  "\uffff": "max BMP (FFFF)",
  "\udbff\udfff": "max code point (10FFFF)",
  "\ud800\udc00": "min supplementary (10000)",
  "\ufffe": "noncharacter FFFE",
});
corpus.push({
  "": "empty key",
  a: 1,
  aa: 2,
  ab: 3,
  A: 4,
  AA: 5,
  Z: 6,
  "a\u0000b": "NUL inside key",
  "a\"b": "quote in key",
  "a\\b": "backslash in key",
  "a/b": "slash stays literal",
  "a\u0008b": "backspace in key",
  "\u001f": "max control",
  "\u007f": "DEL literal",
  "0": "zero",
  "-1": "minus",
});
corpus.push({ "\u2028": "line separator", "\u2029": "para separator", "\u00a0": "nbsp", "\u3000": "ideographic space" });
corpus.push({ numbers: [333333333.33333329, 1e30, 4.5, 2e-3, 1e-27], string: "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/", literals: [null, true, false] });

// 2. Number torture set: every boundary of the ECMAScript formatting
//    rules plus systematic randoms.
const numberCases = [
  0, -0, 1, -1, 0.1, 0.5, 100, 1e21, 1e20, 9.999999999999999e20,
  1e-7, 1e-6, 9.999999999999999e-7, 0.000001, 5e-324, -5e-324,
  1.7976931348623157e308, -1.7976931348623157e308, 9007199254740992,
  -9007199254740992, 9007199254740991, 295147905179352830000,
  9.999999999999997e22, 1e23, 1.0000000000000001e23,
  999999999999999700000, 999999999999999900000, 0.002,
  -0.0000033333333333333333, 1424953923781206.2, 2.2250738585072014e-308,
  0.3, 0.7, 1e-27, 4.50, 2e-3, 123456789.123456789,
  1e100, 1e-100, 6.02214076e23, 1.602176634e-19,
];
for (const n of numberCases) corpus.push(n);
for (let i = 0; i < 80; i++) corpus.push(randomDouble());
for (let i = 0; i < 20; i++) corpus.push(Math.floor(rnd() * 1e6));
for (let i = 0; i < 10; i++) corpus.push(-(rnd() * 1e10));

// 3. Structural cases: nesting, empties, mixed arrays, key-order chaos.
corpus.push([]);
corpus.push({});
corpus.push([[], [[]], [[], [[]]]]);
corpus.push({ a: {}, b: [], c: null, d: "", e: [], f: {} });
let deep = 42;
for (let i = 0; i < 60; i++) deep = [deep];
corpus.push(deep);
let deepObj = { leaf: true };
for (let i = 0; i < 40; i++) deepObj = { child: deepObj, i };
corpus.push(deepObj);
corpus.push([1, "1", null, true, false, { "1": [1, { "": [null] }] }, [[[[]]]]]);
corpus.push({ z: 1, y: { x: 2, w: [3, { v: 4, u: { t: 5 } }] }, s: "six" });
corpus.push({
  abilities: ["跳躍", "مرحبا", "🎉", "🇺🇳", "גשר", "∑ƒ(x)dx"],
  meta: { "héllo wörld": "naïve café", 日本語: "中文", "emoji 🎉 key": "value\twith\ttabs" },
});

// 4. Values as they appear in an A2A agent card (the package's first
//    consumer): strings, bools, string arrays, no numbers.
corpus.push({
  name: "Test Agent",
  description: "",
  capabilities: { streaming: false, pushNotifications: false },

  skills: [{ id: "mcp", name: "MCP tools", tags: ["mcp"] }],
  defaultInputModes: ["text/plain"],
  defaultOutputModes: ["text/plain"],
  supportedInterfaces: [{ url: "https://example.test/mcp", protocolBinding: "JSONRPC", protocolVersion: "1.0" }],
});

// 5. String corpus: every short escape, \u00xx controls, astral text.
const strings = [];
for (let c = 0; c < 0x20; c++) strings.push(String.fromCharCode(c));
strings.push(String.fromCharCode(0x7f), String.fromCharCode(0x80), String.fromCharCode(0x7ff), String.fromCharCode(0x800));
strings.push('quote " backslash \\ solidus / \b \t \n \f \r mix');
strings.push("😀😃😄 the quick bröwn föx jumpéd 🦊 over 𝕥𝕙𝕖 lazy dog");
strings.push("​".repeat(50)); // zero-width space run
strings.push("mixed \u0000\u0001\u001f controls and émojis 🎉");
corpus.push(strings);

// 3b. Deterministic random structures: random depth, random key pools
//     (incl. the UTF-16/UTF-8 divergence chars), random scalars.
const keyPool = [
  "a", "b", "aa", "ab", "A", "z", "0", "10", "2", "", " ", "\u20ac", "\ufb33",
  "\ud83d\ude00", "\ue000", "\uffff", "\udbff\udfff", "k\u0001", "k\u0001\u0002",
  "signature", "protected", "header", "kid", "alg", "typ", "jku", "url",
];
function randomScalar() {
  const r = rnd();
  if (r < 0.35) return randomDouble();
  if (r < 0.55) return Math.floor(rnd() * 1e9) - 5e8;
  if (r < 0.7) return rnd() < 0.5;
  if (r < 0.8) return null;
  if (r < 0.9) return keyPool[Math.floor(rnd() * keyPool.length)] + " value 😀";
  return strings[Math.floor(rnd() * strings.length)];
}
function randomStructure(depth) {
  if (depth <= 0 || rnd() < 0.3) return randomScalar();
  if (rnd() < 0.4) {
    const arr = [];
    const n = Math.floor(rnd() * 6);
    for (let i = 0; i < n; i++) arr.push(randomStructure(depth - 1));
    return arr;
  }
  const obj = {};
  const n = Math.floor(rnd() * 8) + 1;
  for (let i = 0; i < n; i++) {
    // Object keys dedupe automatically; distinctness not required.
    obj[keyPool[Math.floor(rnd() * keyPool.length)]] = randomStructure(depth - 1);
  }
  return obj;
}
for (let i = 0; i < 60; i++) corpus.push(randomStructure(4));

// ── write fixtures ──────────────────────────────────────────────────
const expected = corpus.map((v) => Buffer.from(canon(v), "utf8").toString("hex"));
writeFileSync(new URL("corpus.json", import.meta.url), JSON.stringify(corpus, null, 1) + "\n");
writeFileSync(new URL("corpus-expected.txt", import.meta.url), expected.join("\n") + "\n");
console.log(`corpus: ${corpus.length} values, ${expected.length} expected lines`);
