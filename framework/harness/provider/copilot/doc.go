// Package copilot will implement the GitHub Copilot Provider.
//
// Lands in v0.2 (NOT v0.1). Copilot's chat endpoint is
// reverse-engineered, not a documented public API, so the adapter
// carries its own brittleness budget. When v0.2 ships:
//
//   - OAuth device-code flow against GitHub
//   - Token exchange to a short-lived Copilot internal token (refresh
//     transparently; honor `endpoints.api` in the exchange response)
//   - Required headers: Editor-Version, Copilot-Integration-Id (a
//     known-good ID is required; the whitelist is server-side)
//   - Streaming response shape differs subtly from official OpenAI
//   - Failover: Copilot 401 → OpenRouter with same model name (pre-wired)
//
// Until then this package is intentionally empty; the architecture
// doc commits to the surface at § Providers → v0.2 — GitHub Copilot.
package copilot
