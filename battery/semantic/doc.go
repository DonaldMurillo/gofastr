// Package semantic provides a local semantic-search battery for GoFastr.
//
// The package is built around a single per-app [Index] that stores
// [Chunk]s with vector embeddings and serves [Query]s via brute-force
// cosine similarity, with optional hybrid keyword fusion, metadata
// filtering, MMR diversity, and a pluggable rerank hook.
//
// Components are intentionally separated so users can swap parts:
//
//   - [Embedder] turns text into vectors. [Open] has no default: it
//     errors if Options.Embedder is nil. [NewOllamaEmbedder] is an HTTP
//     client against an Ollama-compatible server (no CGO, no bundled
//     model) and the recommended production choice; [NewStubEmbedder]
//     is a deterministic, dependency-free stub for tests and offline dev.
//   - [Chunker] splits a [Document] into [Chunk]s. The default
//     [FixedWindow] is language-agnostic and tokenizer-free.
//   - [Store] holds vectors and metadata. The default [FlatStore] keeps
//     everything in memory; [NewPgVector] provides a durable alternative
//     backed by Postgres + pgvector for multi-replica apps.
//
// See battery/semantic/README.md for the architecture, retrieval pipeline,
// and milestone plan.
package semantic
