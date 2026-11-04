# Implementation regression notes

## 2026-11-04 - Local 2FA assurance is not a session credential
- Scope: battery/auth, generated Local SDK
- Trigger: cookie-only `RequireTwoFA` refused Local CRUD, while inheriting a session token could outlive an inner tenant's principal clearing and authorize account mutations.
- Approach: record the issuing manager, resolved tenant and user ids, and any-live-non-pending-cookie verification fact; preserve it for marked calls, require all three bindings at the receiving gate, and clear or replace it with the principal. Resolve the user once, then judge later cookies by session state only, matching the network gate.
- Recheck: retain the original bounded candidate tokens in the principal-check closure, not the assurance fact. Recompute for the established user and tenant; a revoked first cookie can fall back to another of that user's sessions, never another user's session.
- Boundary: every built-in auth route uses `browserAccountOnly`; the five session-account gates also call the shared refusal directly. Local account and credential operations return 403, never inherit browser credentials, and never re-check session revocation from the assurance snapshot.
- Evidence: `go test ./battery/auth -run '^TestInProcess' -count=1` covers real outer cookies, the full Local middleware stack, both cookie orders, pending/expired/revoked/other-user candidates, nested clearing, store failures, forged network assurance, and all account routes/gates.
- Mutation evidence: dropping the manager or tenant comparison fails `ManagerBinding/OtherManager` or `TenantBinding/tenant-b`; selected-token-only rechecks fail `RecheckCandidates/LaterVerified`. Removing recheck tenant/user binding, revocation refusal, same-user aggregation, pending exclusion, or either aggregation direction fails its targeted case. Existing marker, principal, anonymous-clearing and account-route probes remain active.
- Next time: carry a realm-bound assurance fact, not a credential; test equal user ids across managers and tenants, then recheck both cookie orders, revocation and attempted principal switches through the Local middleware stack.
- Status: active

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
