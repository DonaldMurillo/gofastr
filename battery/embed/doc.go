// Package embed provides a local semantic-search battery for GoFastr.
//
// The package is built around a single per-app [Index] that stores
// [Chunk]s with vector embeddings and serves [Query]s via brute-force
// cosine similarity, with optional hybrid keyword fusion, metadata
// filtering, MMR diversity, and a pluggable rerank hook.
//
// Components are intentionally separated so users can swap parts:
//
//   - [Embedder] turns text into vectors. The default is an ONNX-backed
//     all-MiniLM-L6-v2 (added in M1.5); a deterministic stub
//     ([NewStubEmbedder]) ships for tests and offline development.
//   - [Chunker] splits a [Document] into [Chunk]s. The default
//     [FixedWindow] is language-agnostic and tokenizer-free.
//   - [Store] holds vectors and metadata. The default [FlatStore] keeps
//     everything in memory and persists to disk in M2.
//
// See battery/embed/README.md for the architecture, retrieval pipeline,
// and milestone plan.
package embed
