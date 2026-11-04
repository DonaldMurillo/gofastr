# Implementation regression notes

## 2026-11-04 - Local repository admission and middleware parity
- Scope: framework, auth, generated SDK
- Trigger: socket-free CRUD lost context identity and failed CSRF or used the wrong API prefix.
- Approach: pass `App.InProcessHandler()`; reject ambient credentials before setting the private marker, and preserve ordinary anonymous clearing and CSRF.
- Evidence: `go test ./cmd/gofastr/internal/appgen -run 'TestSDKRepoParity|TestSDKAppPrefixDefaults' -count=1` exercises generated Local/Remote repositories with default middleware, real session auth and CSRF, owner/tenant rules, and integer/composite keys.
- Next time: test the real middleware stack and mutate credential rejection, session clearing, and CSRF skip independently; preserve explicit empty prefix overrides.
- Status: active

### Admission guard mutation evidence
- Removing Cookie/Authorization refusal fails `TestInProcessHandlerRefusesCredentials` (204 dispatched instead of 403).
- Keeping users without the private marker fails both `TestSessionMiddleware_ClearsUserOnAnonymousFallthrough` and `TestSessionMiddleware_ClearsUserOnStoreError`.
- Skipping CSRF without the marker fails `TestCSRFChecksBrowserAttachedAuthorization` and `TestInProcessSessionAndCSRF/Network`.
- Removing either Local exception fails `TestSDKRepoParity` with 401 (session) or 403 (CSRF); removing app-prefix inference fails with 404. Removing the post-dispatch cancellation check fails `TestLocalCanceledDuringDispatch` with EOF.
