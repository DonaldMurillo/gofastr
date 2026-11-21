# battery/ sweep: adversarial review, 2026-11-21

The reviewer for DonaldMurillo/gofastr#874 (hard rule 13). CodeRabbit and
Bugbot skipped the PR for its size, so four agents that did not write it
reviewed it at `946496551`, one per area: auth; desktop; admin, queue,
semantic and webhook; the thirteen smaller batteries. Each was told to break
the change rather than confirm it and to prove every finding by mutation
against the code. Four more agents fixed what they found; the lead re-ran
the highest findings' mutations on the merged tree.

## What the review found

No behaviour change beyond the ones the PR describes, no tool-read marker
lost (after the two CI caught), no test weakened, and the docs the PR added
match the code. The weakness was in the registry: many new invariants cited
tests that still passed with their rule broken, and rules from deleted
comments that nothing guarded. The code was correct in every case but one.

| Area | Mutations of invariants | Held | Deleted-comment rules unguarded |
|---|---|---|---|
| auth | 47 | 34 | 7 |
| desktop | 17 | 9 | about 15 |
| admin, queue, semantic, webhook | 64 | 51 | about 10 |
| the thirteen others | about 95 | about 81 | 12 |

## Production code changed by the fixes

- `battery/storage/local.go`: a failed final rename in `Save`, with
  `WithTempDir` outside the base directory, returned the temp file's
  absolute path, which the CRUD handlers echo into a 400 body. The
  `*os.LinkError` is now scrubbed (`TestSaveTempDirErrorsHidePaths`).
  `core/upload` keeps its temp file under the root, so its scrub already
  covers it.
- `battery/semantic/routes.go`: `Handler` stamps `Cache-Control: no-store`
  on every answer; the DELETE 204 and the mux's 404 and 405 went without it
  (`TestHandlerAnswersAreNoStore`).
- `battery/desktop/internal/ffi`: compile-time offset pins for
  `callbackArgs` and the leading `callArgs` fields, so a moved field fails
  the build as `platform.ffi/arg-block-layout-pinned` says.

## Findings that held, and their fix

Every line is `mutation -> the test that now fails`. A starred line was
re-run by the lead on the merged tree.

auth:
- completeStepUp before the 2FA step is spent (one TOTP code, two verified sessions) -> TestTwoFAChallengeCodeSingleUseConcurrent, which now counts verified sessions in the store *
- link flow saves provider tokens after the re-check -> TestLinkClaimAfterRecheckLeavesNoTokens
- logout's cross-site check off under WithCSRFAuthRoutes -> TestCSRFAuthRoutesLetsFormPostCallbackThrough
- mail queue logs the panic value -> TestMailQueueRecoversSenderPanic
- 2FA enrol reads state, error ignored -> TestTwoFAEnrollFailsClosedOnReadError
- cross-site guard off on /2fa/challenge and /reset-password -> TestTwoFARoutesRejectCrossSite, TestResetPasswordCrossSiteRefused
- CSRF helpers return the raw token -> TestCSRFTokensAreMaskedPerRender
- OIDC claims cache and forced-kid set unbounded -> TestOIDCClaimsCacheBounded, TestJWKSForcedKidsBounded
- cookie fan-out raised past 8 -> TestSessionCookieFanoutBounded
- battery/auth imports framework/ui -> TestBatteryDoesNotImportTheDesignSystem; battery/auth -> authui is an `imports:` rule
- five uncited tests that do catch a mutation are now cited (subjectless grant, sign-in token order, provider body cap, memory passkey sign count, reset unlink order)

desktop:
- a grant-store read error lets the call through -> TestGrantReadErrorFailsClosed *
- install ignores VerifyArchive -> TestInstallRefusesArchiveOffTheFeed *
- install ignores the OS signature check -> TestInstallRefusesFailedSignatureCheck
- an older feed release installs -> TestInstallRefusesDowngrade
- the gate armed after the listener answers -> TestRunEndToEndWithFakeShell
- grant write unbounded -> TestGrantWriteIsBoundedByGrantWriteTimeout
- an empty bundle identifier on both sides -> TestInstallRefusesWithoutRunningIdentifier
- fs and folder errors echo the path -> TestFSErrorsNeverEchoThePath, TestFolderPickRefusesARelativeLinkOut
- deep-link scheme re-check, 200-byte cut and DEL mask off -> TestDeepLinkSchemeBounds, TestScrubDeepLinkCutsAndMasks
- zip entry cap, declared size cap and setuid stripping off -> TestExtractZipRefusesTooManyEntries, TestExtractZipRefusesEntryDeclaredTooLarge, TestExtractStripsSetuidSetgidSticky
- fetch errors carry the URL -> TestFetchErrorsCarryNoURL
- nil key, empty loader subject, staging cleanup -> TestUpdatesMethodsUnconfiguredRefuse, TestCheckWindowsTrustPolicy, assertNoStaging in the refusal tests
- callbackArgs reordered -> the build fails

admin, queue, semantic, webhook:
- pgvector interpolates Kind or a MetaMatch key -> TestPgVectorFilterNoInjection, TestPgVectorMetaMatchNoInjection
- Authorize lifts a token without AdminScope -> TestAuthorizeCannotLiftAScopelessToken *
- RBAC tier read from the context's roles -> TestTierIgnoresContextRoles *
- queue DB settle fences (done retention, Nack retry, Nack dead) -> TestDBStaleClaimCannotSettleTheNewOne, TestDBStaleNackLosesARaceToANewClaim
- webhook non-terminal settle replaces a dead row -> TestDeadNeverOverwritesSuccess
- webhook response read uncapped -> TestReadResponseStopsAtTheCap
- memory dead-letter list unbounded -> TestMemoryDeadLetterListIsBounded
- admin cross-site PUT, PATCH, DELETE -> TestGateRefusesEveryForgeableCrossSiteShape
- Redis HGet missing field -> TestRedisSettleOfUnclaimedJobIsANoOp
- inbound no-store, default client redirects, persist dir modes -> TestIngest_HappyPath, TestDefaultClientRefusesRedirect, TestPersistDirsAreOwnerOnly
- scheduler commit fence and lease expiry -> TestCommitNeedsTheSameFenceAndAnUnexpiredLease, TestPG_CommitNeedsTheSameFenceAndAnUnexpiredLease
- Redis claim compare -> the secretcompare analyzer

the thirteen others:
- a Bcc header written -> TestBccIsNeverAHeader
- credentialed responses stored or served to an anonymous caller -> TestCredentialedAndAnonymousNeverShareAnEntry *
- RenderPDF without the network gate, or with it after ExtraFlags -> TestRenderPDFInstallsNetworkGate (chromium)
- a failed STARTTLS falls through to cleartext -> TestSMTP_FailedSTARTTLSEndsTheSend *
- the service gate echoes a secret -> TestGateRefusalsEchoNoSecret
- the login door behind the cross-site check -> TestGateLoginAcceptsFilePageAndAnswersWithAPage
- relay logs an unescaped method; a read error as no body; no request deadline; a refused body left open -> TestProxyErrorLogsMethodEscaped, TestBodilessReadErrorCountsAsBody, TestProxiedRequestGetsTheRequestDeadline, TestRefusedContentTypeClosesUpstreamBody
- header checks for DEL and invalid UTF-8 off -> TestEmail_HeaderInjection
- storage scrub of a temp path off -> TestSaveTempDirErrorsHidePaths
- notify Sender not reserved; print owner check order; memory cap; Content-Range; variant key prefix -> the extended or new tests named in the registry
- log file mode, flush, rotate-first and access-path snapshot -> TestFileSinkTightensExistingLooseFile, TestFileSinkFlushesAndRotatesBeforeWrite, TestAccessLogPathIsTakenBeforeTheHandler

## Rule text corrected

email.smtp/fails-closed-without-tls, log.mcp/host-gate-holds-in-dev,
storage.local/key-is-its-clean-form, desktop.update/bundle-is-the-feed-release,
desktop.update/windows-trust-never-loosens, semantic.http/answers-are-no-store,
and print.md's print-CSS sentence. The `hmac.Equal` and semantic
bearer-compare clauses are now judged invariants.

## Left, with the reason

- Darwin and Windows rules: the desktop macOS, objc and two updater
  invariants cite tests that run only on a Mac or Windows machine, which CI
  never does. `primitives/desktop/platform` now says so in its `on_change`;
  five more rules from deleted comments are judged only.
- storage's fold check before MkdirAll: matters only on a case-insensitive
  filesystem; unverified here.
- webhook's `keep > total` clamp: no test can see it while the LimitReader
  holds, as the reviewer noted.
- battery/auth links framework/ui through framework and entityui, so the
  deleted claim that an API-only app never links the design system was
  already false. Cutting that chain is a separate decision.
