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

## 2026-11-04 - Global toast catalog versus per-router assets
- Scope: framework/uihost, core-ui/widget
- Trigger: a second host registered a default beside the first host's custom stack and left its style missing on the second router.
- Approach: reuse all registered stack definitions and attach only missing CSS routes at their published `StyleURL`; toast stacks have no per-stack script.
- Evidence: `go test ./framework/uihost -run 'Test.*Toast' -count=1` covers simultaneous custom hosts, prefixed assets, repeated boot, unrelated widgets, and the original two-default-host case; mutations of stack-kind, missing-route, and default-registration guards each fail.
- Next time: boot every host before rendering the first, assert the region count, and fetch every linked stylesheet on each router.
- Status: active
