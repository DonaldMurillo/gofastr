# battery/ sweep: mutation proofs, 2026-11-20

Each line is `mutation -> failing test` for an invariant the `battery/`
sweep added or re-pointed (`primitives/PLAN.md` step 2). The four groups
ran their own proofs; the lead's spot checks, run on the merged tree, come
first. A crash means the test binary died, which is how an unrecovered
goroutine panic fails.

The auth log was taken before main was merged in; its second part reran
every mutation whose code or cited test main changed. Two pre-merge
replacement tests (`TestExpiredStateRefused`, `TestOIDCWeakRSAJWKSkipped`)
were later deleted when the originals were fixed, and the passkey
registration owner clause was dropped because main's session binding now
holds it.

## Lead spot checks (merged tree)

- oauth2.go: verifyState expiry check off -> TestOAuth2Plugin_StateExpiry
- oidc_jwks.go: RSA floor 2048 -> 1024 -> TestOIDCSec_WeakRSAKey
- desktop/ui/layout.go: SidebarTopInset 52 -> 40 -> TestSidebarTopInsetMeasured
- desktop/internal/update/apply.go: MoveTree existing-destination check off -> TestMoveTreeRefusesExistingDestination
- auth/jwt.go: future-iat skew 2m -> 2h -> TestJWTRejectsFutureIssuedAt
- auth/password_change.go: lost swap not refused -> TestPasswordChangeLosesToReset
- webhook/webhook.go: deliveriesTotal.Add(2) -> survived; test tightened, then TestWebhookMetrics_CountersAndCollector
- webhook/webhook.go: http-status failuresTotal.Add(2) -> TestWebhookMetrics_CountersAndCollector
- queue/memory.go: ErrPermanent retried -> TestMemoryPermanentDeadLettersAtOnce
- email/message.go: Date header not added -> TestMessageHasDateAndMessageID
- relay/relay.go: OnStop no longer closes idle connections -> TestShutdownClosesIdleTransportConns
- cache/memory.go: negative TTL stored -> TestMemoryCache_NegativeTTLNotImmortal
- admin/shell.go: account menu drawn with no user -> TestAccountMenuWithNoUser
- print/chromepdf/chromepdf.go: AllowedHosts doc says "does widen" -> TestAllowedHostsRespectsCSPDocs
- admin/gate.go + core/handler.GetUser: nil user passes all three layers -> TestGateRefusesASeededNilUser
- desktop/desktop.go: relative Config.DataDir accepted -> TestConfigDataDirMustBeAbsolute
- desktop/datadir.go: override dir made 0755 -> TestConfigDataDirIsCreatedPrivate
- search/memory.go: empty-owner guard off; dry run deletes -> TestMemoryEraseSkipsEmptyOwnerAndDryRun
- search/memory.go: Close keeps the eraser -> TestEraseRemovesMemoryDocuments
- desktop/internal/update/predecessor.go: no wait; returns false -> TestAwaitPredecessorWaitsForALiveProcess
- auth/mailqueue.go: run recover off -> TestMailQueueRecoversSenderPanic (crash)
- auth/mailqueue.go: both recovers off -> TestDuplicateNoticeSenderPanicRecovered (crash)

## desktop

handshake.go: !b.tokenUsed.CompareAndSwap(false, true) -> false -> TestEnterFlow, TestEnterSingleUseUnderConcurrency
handshake.go: if !tokenMatches(r.URL.Query().Get("t"), b.bootToken) || !b.tokenUsed.CompareAndSwap(false, ... -> TestEnterFlow
handshake.go: if !b.gateArmed.Load() { next.ServeHTTP(w, r) return } if r.URL.Path -> if r.URL.Path -> TestGateArmingDecidesWhetherItGates
handshake.go: if handler.IsForgedCrossSiteWrite(r) { -> if false { -> TestDesktopOpenPrefixRefusesCrossSiteWrite
handshake.go: if c.Name != b.cookieName() || c.Value == "" { continue } if subtle.ConstantTimeCompare([]by... -> TestGateAcceptsStaleDuplicateCookieBeforeRealOne
handshake.go: if b.sessionCookieKey != "" { -> if false { -> TestRunEndToEndWithFakeShell
handshake.go: validNavigatePath(p) -> p != "" -> TestTamperedStoredPathIsNotTheBootRedirect
handshake.go: pin == "" || r.Host != pin -> pin == "" && r.Host != pin -> TestDesktopOpenPrefixKeepsHostPin, TestGateRefusesWrongHost
handshake.go: if !b.requestHasSession(r) { -> if false { -> TestGateRefusesForgedCookieOfRightLength, TestGateRefusesWithoutCookie
localuser.go: if !b.gateArmed.Load() || b.authOwnsIdentity { -> if b.authOwnsIdentity { -> TestLocalUserNotInjectedWhenGateUnarmed, TestUnarmedGateInstallsNoIdentity
localuser.go: if !b.gateArmed.Load() || b.authOwnsIdentity { -> if !b.gateArmed.Load() { -> TestAnonDecisionIsNotOverridden
localuser.go: if b.openPath(r) && !b.requestHasSession(r) { -> if false { -> TestDesktopOpenPrefixPassesAnonymous
localuser.go: b.authOwnsIdentity = localapp.Install() -> localapp.Install() -> TestAnonDecisionIsNotOverridden, TestSecondBatteryKeepsLocalIdentity
handshake.go: HttpOnly: true, -> HttpOnly: false, -> TestEnterFlow
handshake.go: SameSite: http.SameSiteStrictMode, -> SameSite: http.SameSiteLaxMode, -> TestEnterFlow
handshake.go: if b.app != nil && !b.authOwnsIdentity { -> if b.app != nil { -> TestArmGateLeavesTheNoteToAuth
bridge.go: if !b.reg.isFrozen() { writeNotFound(w) -> if false { writeNotFound(w) -> TestCallRefusedBeforeRun, TestCapsNotEnumerableBeforeRun
bridge.go: if handler.IsCrossSiteRequestStrict(r) { -> if handler.IsCrossSiteRequest(r) { -> TestChokepointCrossSiteByOrigin
bridge.go: if probe == nil { -> if false { -> TestBridgeRefusesNullBody
bridge.go: if bodyHasNUL(body) { -> if false { -> TestBridgeRefusesNULInBody, TestNULRefusalBeatsThePermissionGate
bridge.go: _ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(callBudget)) ->  -> TestSlowBridgeCallStillAnswers
bridge.go: case <-ctx.Done(): return false, CodeCancelled } -> } -> TestQueuedPromptGivesUpWithItsRequest
bridge.go: context.WithTimeout(context.WithoutCancel(ctx), grantWriteTimeout) -> context.WithTimeout(ct... -> TestAllowPersistsWhenPageAborts, TestDenyPersistsWhenPageAborts
bridge.go: case DecisionAllowOnce: return true, "" -> case DecisionAllowOnce: b.persistGrant(ctx, cap.N... -> TestAllowOnceStillNeverPersists, TestChokepointPermissionGate
bridge.go: decision, found, err = b.grants.Get(ctx, cap.Name, m.Permission) if err != nil { -> decision... -> TestConcurrentFirstCallsShowOneAlert
bridge.go: if decision == grantAllow { return "" } return CodeDenied -> return "" -> TestUnknownStoredDecisionReadsAsDeny
bridge.go: if strings.IndexByte(k, 0) >= 0 || valueHasNUL(t[k]) { -> if valueHasNUL(t[k]) { -> TestBridgeRefusesNULInNestedKey
bridge.go: if !found { writeNotFound(w) -> if !found { writeBridgeError(w, CodeNotFound, "no such method") -> TestChokepointUnknownCapabilityAndMethodIdenticalBodies
bridge.go: return CodeInternal, InternalErrorMsg } } -> return CodeInternal, err.Error() } } -> TestChokepointErrorMapping
bridge.go: http.MaxBytesReader(w, r.Body, maxCallBody) -> r.Body -> TestChokepointOversizeBody
bridge.go: err != nil || mt != "application/json" -> err != nil || mt == "" -> TestChokepointMethodAndContentTypeChecks
bridge.go: `^[a-z][a-z0-9]{0,15}$` -> `^[a-z0-9]{0,40}$` -> TestWindowsSelfAnswersCallersWindow
bridge.go+cap_appwindows_msg.go: reWindowID.MatchString(id) -> id != "" (both sites) -> TestWindowsSelfAnswersCallersWindow
bridge.go: io.WriteString(w, bridgeInactiveJS) return -> retryShortly(w) return -> TestBridgeScriptAndManifestBeforeFreeze503
bridge.go: if err != nil { retryShortly(w) return } body, merr -> if err != nil { writeNotFound(w) retu... -> TestBridgeScriptAndManifestBeforeFreeze503
bridgejs.go: D.manifest = JSON.parse(" + jsQuote(string(manifestJSON)) + "); -> D.manifest = " + string(m... -> TestBridgeJSBracketStringAccessOnly
bridgejs.go: return "if (window === window.top) { window.__gofastr_desktop -> return "if (true) { window.... -> TestBridgeScriptsIgnoreChildFrames
capability.go: "call": true, ->  -> TestCapabilityRefusesRuntimeNames
capability.go: if cap.Version < 1 { -> if cap.Version < 0 { -> TestRegistryValidationRules
capability.go: if seen[m.Name] { -> if false { -> TestRegistryValidationRules
capability.go: m.Permission != "" && !rePermission.MatchString(m.Permission) -> false -> TestRegistryValidationRules
bridgejs.go: fmt.Fprintf(&b, " D[%s] = {\ ", jsQuote(cap.Name)) -> fmt.Fprintf(&b, " D.%s = {\ ", cap.Name) -> TestBridgeJSBracketStringAccessOnly
bridgejs.go: q, err := json.Marshal(s) -> q, err := fmt.Sprintf("%q", s), error(nil) -> TestJSStringsNeverUseGoUnicodeEscapes
bridgejs.go: b.WriteString(" if (window !== window.top) return;\ ") ->  -> TestBridgeScriptsIgnoreChildFrames
capability.go: if r.frozen { return fmt.Errorf -> if false { return fmt.Errorf -> TestRegisterAfterFreezeFails
capability.go: if a.Name < b.Name { return -1 } return 1 -> return 0 -> TestManifestShape
capability.go: this registration from %s must use a different name", cap.Name, prev.origin, origin) -> this... -> TestDuplicateNamesFirstRegistrantSite
capability.go: if err := jsonexact.Unmarshal(in, &v); err != nil { -> var _ = jsonexact.Unmarshal if err :=... -> TestManifestKeepsBigIntegers
grants.go: WHERE capability = $1 AND permission = $2`, query.QuoteFolded(grantsTable)), capability, per... -> TestGrantIsKeyedOnItsCapability
grants.go: if decision != grantAllow && decision != grantDeny { return fmt.Errorf("desktop: grant decis... -> TestSQLGrantStoreRoundTrip
grants.go: if decision != grantAllow && decision != grantDeny { -> if decision == "" { -> TestMemGrantStore, TestSQLGrantStoreRoundTrip
grants.go: ON CONFLICT(capability, permission) DO UPDATE SET decision = excluded.decision, granted_at =... -> TestSQLGrantStoreRoundTrip
capability.go: snapshot := make([]registered, 0, len(r.caps)) for _, name := range slices.Sorted(maps.Keys(... -> TestManifestDeterministicAcrossRegistrationOrders
bridge.go: if err := handler.UnmarshalStrict(body, &probe); err != nil || -> if err := json.Unmarshal(b... -> TestChokepointNonObjectBody
desktop.go: if strings.Trim(id, ".-") == "" { -> if false { -> TestAppIDRejectsDotSegments, TestDataDirStaysUnderItsBase
desktop.go: !reAppID.MatchString(id) || !strings.Contains(id, ".") -> !reAppID.MatchString(id) -> TestAppOptionsRejectsBadID, TestDataDirRejectsBadID, TestNewValidatesConfig
desktop.go: in != nil && (in.X < 0 || in.Y < 0) -> in != nil && in.X < 0 -> TestNewRefusesBadChrome
desktop.go: case MaterialNone, MaterialSidebar, MaterialWindow, MaterialGlass: default: -> default: -> TestNewRefusesBadChrome, TestOpenWindowRefusesBadStyle
desktop.go: cfg.OpenPrefixes = append([]string(nil), cfg.OpenPrefixes...) ->  -> TestDesktopNewCopiesOpenPrefixes
desktop.go: merged.Items = append(merged.Items, cfg.Tray.Menu.Items...) } -> } -> TestPlanTrayMenuSharesActionIDTable
desktop.go: if cfg.SidebarWidth < 0 || cfg.SidebarWidth > MaxSidebarWidth { -> if cfg.SidebarWidth < 0 { -> TestNewRefusesBadChrome
desktop.go: if len(id) > 253 { -> if len(id) > 2530 { -> TestNewRefusesConfigEdges
desktop.go: } else if cfg.DeepLink.Scheme != builtDeepLinkScheme { -> } else if false { -> TestNewRefusesConfigEdges
desktop.go: if cfg.Settings != nil && !validNavigatePath(cfg.Settings.Path) { -> if false { -> TestNewRefusesConfigEdges
desktop.go: if len(b.winOrder) >= maxSecondaryWindows { -> if false { -> TestCappedOpenRefusesTyped, TestSecondaryWindowsAreCapped
desktop.go: if err := w.Focus(); err != nil { return nil, err } ->  -> TestOpenWindowDedupesByPathAndFocuses
desktop.go: if s := b.cfg.Settings; s != nil && s.Path == path { return "settings" } ->  -> TestOpenSettings, TestSettingsWindowFromEveryEntryPoint
desktop.go: if b.nextWinNum < 2 { b.nextWinNum = 2 } ->  -> TestOpenWindowDedupesByPathAndFocuses, TestWindowsCapabilityEdges
desktop.go: if s := b.winStore.Load(); s != nil && spec.Frame == nil { spec.Frame = s.frameFor(id) } ->  -> TestRelaunchDropsTransientFrames, TestRememberedFramesRestoreAcrossRuns
desktop.go: if closed, known := windowClosed(w); known && !closed { // A delayed close callback may belo... -> TestHandleWindowClosedKeepsRegistrationForOpenWindow
desktop.go: if s := b.winStore.Load(); s != nil { s.forget(id) } ->  -> TestClosedSecondaryWindowFrameIsForgotten
desktop.go: if closed, known := windowClosed(w); w != nil && known && closed { b.removeWindowLocked(id) ... -> TestOpenWindowReplacesAClosedRegistration
desktop.go: if closing.Load() { return } ->  -> TestRunQuitLogsNoServerStop
desktop.go: b.flushDeepLinks(w) // Config.Widgets -> // Config.Widgets -> TestColdLinkMapsOnce, TestDeepLinkKeepsEscapedPath, TestDeepLinkNavigatesAndEmits, TestDeepLinkOverride, TestDeepLinkRefusesEncodedDots, TestDeepLinkRefusesOffSchemeUserinfoLength, TestDeepLinkRefusesTraversal, TestDeepLinkSchemeAloneMapsToRoot, TestDeepLinksQueueUntilWindow
desktop.go: if s := b.state.Load(); s != nil { _ = s.Flush() // a failed write was already Warned inside... -> TestRememberedPathBecomesBootRedirect, TestStateSurvivesSecondBattery
desktop.go: if firstErr == nil { firstErr = err } -> return err -> TestEmitKeepsGoingPastAFailedWindow
desktop.go: if len(b.Windows()) == 0 { return } if err := b.Emit(name -> if err := b.Emit(name -> TestFocusBeforeAnyWindowIsDroppedQuietly
desktop.go: if len(b.Windows()) == 0 { return } if err := b.Emit("reduce_transparency" -> if err := b.Em... -> TestFocusBeforeAnyWindowIsDroppedQuietly
desktop.go: if got, err := framework.GetAs[*Battery](app.Batteries, "desktop"); err != nil || got != b {... -> TestRunRefusesAnUnregisteredBattery
desktop.go: if !b.cfg.KeepIsolation { os.Setenv("GOFASTR_ISOLATION", "off") } ->  -> TestRunTurnsWorktreeIsolationOff
desktop.go: b.window = nil b.windows = make(map[string]Window) -> b.windows = make(map[string]Window) -> TestEmitAfterStopFails
desktop.go: if err := app.InitPlugins(); err != nil { return fmt.Errorf("desktop: manifest mode: init pl... -> TestManifestModeIncludesPluginCapabilities
desktop.go: if b.initialized { return errors.New("desktop: Init called twice") } ->  -> TestInitTwiceIsRefused
desktop.go: if launch, ok := b.shell.(interface{ ReleaseLaunch() }); ok { defer launch.ReleaseLaunch() }... -> TestLaunchCleanupAfterShellReturn, TestLaunchCleanupOnStartupFailure, TestLaunchCleanupPrecedesColdLinkMapping
desktop.go: if !reCapabilityName.MatchString(name) { return fmt.Errorf("desktop: event name %q must matc... -> TestEmitInvalidNameRejected
desktop.go: b.shell.Quit() }() shellErr -> }() shellErr -> TestRunQuitsWhenServerStops
desktop.go: b.setHostPin(addr) b.reg.freeze() -> b.setHostPin(addr) -> TestRunEndToEndWithFakeShell
desktop.go: if handled { return nil } -> if handled && false { return nil } -> TestLaunchSecondaryOwnsItsCleanup
desktop.go: b.sessionCookieKey = sessionCookieName + "_" + b.sessionValue b.armGate() -> b.sessionCookie... -> TestRunEndToEndWithFakeShell
dialog_input.go: strings.ContainsAny(ext, "/\\\\*?[];") -> strings.ContainsAny(ext, "/\\\\*?[]") -> TestFilterPatternsForms
dialog_input.go: ext == "" || !NativeText(ext) || -> !NativeText(ext) || -> TestFilterPatternsForms
dialog_input.go: if len(extensions) == 0 { return []string{"*"}, nil } ->  -> TestFilterPatternsForms
dialog_input.go: strings.ContainsAny(defaultName, `/\\`) -> strings.ContainsAny(defaultName, `/`) -> TestCheckFileDialogRefuses
dialog_input.go: if !NativeText(filter.Name) { -> if false { -> TestCheckFileDialogRefuses
dialog_input.go: func NativeText(s string) bool { return textsafe.Storable(s) } -> func NativeText(s string) ... -> TestCheckFileDialogRefuses, TestFilterPatternsForms
shell_default.go: func (s *unsupportedShell) Run(context.Context, WindowConfig, func(Window)) error { return u... -> TestDefaultShellUnsupportedNamesPlatform
shell_text.go: loc, ok := i18n.Match(tr, preferred...) -> loc, ok := i18n.EnvLocale(tr), false _ = preferred -> TestShellContextPicksPreferredLanguage
menu.go: if it.Navigate != "" && it.Handler != nil { -> if false { -> TestMenuValidationCases
menu.go: if it.Role == RoleSeparator && (it.Navigate != "" || it.Handler != nil) { -> if false { -> TestMenuValidationCases
menu.go: if seen[it.ID] { -> if false { -> TestMenuValidationCases
menu.go: if dec == "." || dec == ".." || strings.IndexFunc(dec, refusedDecodedRune) >= 0 { -> if dec ... -> TestDeepLinkRefusesEncodedSeparators, TestNavigatePathRefusesDotSpellings
menu.go: if dec == "." || dec == ".." || strings.IndexFunc(dec, refusedDecodedRune) >= 0 { -> if stri... -> TestDeepLinkRefusesEncodedDots, TestNavigatePathRefusesDotSpellings
menu.go: if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") { -> if !strings.HasPrefix(p, "/... -> TestMenuValidationCases, TestOpenWindowValidatesPath
menu.go: b.logger.Error("desktop: menu handler panicked", "id", id, "panic", textsafe.Recovered(v)) -... -> TestTrayMenuHandlerPanicIsContained
menu.go: ctx, cancel := context.WithTimeout(context.Background(), menuHandlerTimeout) -> ctx, cancel ... -> TestTrayMenuHandlerCompletionIsSynchronous
menu.go: if async { go run() } else { run() } -> go run() -> TestTrayMenuHandlerCompletionIsSynchronous
menu.go: if async { go run() } else { run() } -> run() -> TestMainMenuDispatchDoesNotWaitForTheHandler
menu.go: return command } -> return true } -> TestMenuKeyNeedsCommandModifier
menu.go: if !isMod { return false } ->  -> TestMenuKeyNeedsCommandModifier
menu_plan.go: plans = append(plans, editMenu(ctx)) ->  -> TestPlanMenuBarSpeaksLocale, TestPlanMenuBarSynthesizesAppAndEditMenus, TestPlanMenuBarUserMenusAndTags
menu_plan.go: if hasSettings { appItems -> if true { appItems -> TestPlanMenuBarSettingsRowOnlyWhenConfigured, TestPlanMenuBarSynthesizesAppAndEditMenus
menu_plan.go: var englishContext = i18n.WithTranslator(context.Background(), i18n.NewTranslator(i18n.NewMa... -> TestPlanEnglishIgnoresDefault
menu_plan.go: mp.Items = planRows(ctx, []MenuItem{it}, appTitle, &actionIDs) ->  -> TestPlanMenuBarChildlessTopLevelActionStaysReachable
menu_plan.go: if it.Role == RoleSeparator { // A separator -> if it.Role == "never" { // A separator -> TestPlanMenuBarDropsTopLevelSeparator
deeplink.go: if len(rawURL) > maxDeepLinkBytes { -> if len(rawURL) > 100*maxDeepLinkBytes { -> TestDeepLinkRefusesOffSchemeUserinfoLength
deeplink.go: if u.User != nil { -> if false { -> TestDeepLinkRefusesOffSchemeUserinfoLength
deeplink.go: if !strings.EqualFold(u.Scheme, cfg.Scheme) { -> if false { -> TestDeepLinkRefusesOffSchemeUserinfoLength
deeplink.go: path = "/" + u.Host + u.EscapedPath() -> path = "/" + u.Host + u.Path -> TestDeepLinkKeepsEscapedPath
deeplink.go: if !validNavigatePath(path) { b.logger.Warn("desktop: dropping a deep link whose path -> if ... -> TestDeepLinkRefusesEncodedDots, TestDeepLinkRefusesEncodedSeparators, TestDeepLinkRefusesTraversal
deeplink.go: if r < 0x20 || r == 0x7f { -> if r == 0x7f { -> TestWarmDeepLinkOverflowLogIsScrubbed
deeplink.go: if up && q.flushed { -> if up { -> TestDeepLinkBeforeFlushKeepsArrivalOrder
deeplink.go: if len(q.pending) > maxQueuedDeepLinks { -> if len(q.pending) > 1000*maxQueuedDeepLinks { -> TestWarmDeepLinkOverflowLogIsScrubbed, TestWarmDeepLinkQueueBoundAndReplay
deeplink.go: over := len(q.queued) > maxQueuedDeepLinks -> over := len(q.queued) > 1000*maxQueuedDeepLinks -> TestColdDeepLinkQueueIsBounded
deeplink.go: if len(c.Scheme) > maxDeepLinkSchemeLen { -> if false { -> TestDeepLinkSchemeBounds
deeplink.go: b.logger.Error("desktop: deep-link delivery panicked", "path", path, "panic", textsafe.Recov... -> (test binary crashed or failed to start)
deeplink.go: return "", false } path = p -> return "", false } path = p _, _ = cfg.OnDeepLink(u) -> TestColdLinkMapsOnce
deeplink.go: b.awaitPageRuntime(w) pathJSON -> pathJSON -> TestDeepLinkWaitsForThePageRuntime
launchargs.go: if len(rest) == 1 && strings.HasPrefix(rest[0], "-psn_") { -> if false { -> TestLaunchArgs
datadir.go: if !filepath.IsAbs(v) { -> if false { -> TestDataDirEnvMustBeAbsolute
launchargs.go: for _, s := range []string{builtDeepLinkScheme, builtLaunchScheme} { -> for _, s := range []... -> TestLaunchArgsKnowTheLaunchOnlyScheme
profile.go: return "", nil, fmt.Errorf("desktop: PersistentProfile is unsupported by this shell: %w", Er... -> TestProfileUnsupportedExplicit
desktop.go: if profile != nil { defer profile.ReleaseProfile() } ->  -> TestProfileStartupCleanup
desktop.go: if profile != nil { if err := profile.CommitProfile(addr); err != nil { -> if false { if err... -> TestProfileStartupCleanup
profile.go: b.dataDir = dir return addr, p, nil -> return addr, p, nil -> TestProfileResolvedIdentity
cap_appwindows_msg.go: JSON.parse(" + jsQuote(data) + "))" -> " + data + ")" -> TestEmitPayloadRoundTripsAsJSONString, TestEmitReachesEveryOpenWindow, TestEmitToDeliversToOneWindow, TestWindowsBroadcastExcludesCaller, TestWindowsPostCapsPayloadSize, TestWindowsPostDeliversToTarget, TestWindowsPostRejectsBadEventNames
cap_appwindows_msg.go: return "if (location.origin === " + jsQuote(origin) + ") { " + js + " }" -> _ = origin retur... -> TestEventsReachOnlyTheAppOrigin
cap_appwindows_msg.go: if b.cfg.DangerouslyAllowForeignNavigation || raw == "about:blank" { -> if b.cfg.Dangerously... -> TestWindowsStayOnTheAppOrigin
cap_appwindows_msg.go: return addr != "" && u.Scheme == "http" && u.Host == addr -> return u.Scheme == "http" && u.... -> TestStoppedBatteryRefusesEmptyHost
cap_appwindows_msg.go: return textsafe.ScrubControlBytes(u.Host) -> return textsafe.ScrubControlBytes(u.Host + u.Path) -> TestNavigationHostDropsPathAndQuery
cap_appwindows_msg.go: if _, ok := b.windowByID(windowID); !ok { return &Error{Code: CodeNotFound, Message: "no win... -> TestPostToWindowClosingMidEvalIsNotFound
cap_appwindows_msg.go: if len(p) == 0 { return "null", nil } ->  -> TestWindowsBroadcastExcludesCaller, TestWindowsPostRejectsBadEventNames
cap_appwindows_msg.go: if buf.Len() > maxEventPayload { -> if p == nil { -> TestWindowsPostCapsPayloadSize
cap_appwindows.go: if (s.X == nil) != (s.Y == nil) { -> if false { -> TestWindowsOpenRefusesBadStyle
windowstate.go: if id == MainWindowID || id == "settings" { continue } delete(windows, id) -> if true { cont... -> TestRelaunchDropsTransientFrames
windowstate.go: if id == "" || id == MainWindowID || id == "settings" { return } s.mu.Lock() -> if id == "" ... -> TestForgetKeepsTheStableWindowFrames
cap_appwindows.go: if v != nil && (*v > 100000 || *v < -100000) { -> if v != nil && false { -> TestWindowsOpenRefusesBadStyle
cap_window.go: s != nil && callerWindowID(ctx) == MainWindowID { -> s != nil { -> TestRememberedPathBecomesBootRedirect
cap_window.go: if id == MainWindowID { return nil, &Error{Code: CodeUnsupported, Message: "window is not op... -> TestWindowCapActsOnTheCaller, TestWindowSnapshotBeforeWindowUnsupported
cap_window.go: if !validNavigatePath(req.Path) { return nil, &Error{Code: CodeInvalidInput, Message: "path ... -> TestRememberedPathBecomesBootRedirect
cap_appwindows.go: if id == "main" { return nil, &Error{Code: CodeInvalidInput, Message: "the main window close... -> TestWindowsCapabilityEdges
cap_appwindows.go: b.handleWindowClosedLocked(id) return nil, nil -> return nil, nil -> TestWindowsCapabilityEdges
cap_fs.go: _, cleanOK := b.allowPaths[p] -> _, cleanOK := b.allowPaths[filepath.Clean(p)] -> TestFSWriteStaysInsideTheGrant
cap_fs.go: if resolved := resolveExistingAncestor(p); resolved != p { b.allowMu.Lock() -> if resolved :... -> TestFSSymlinkSwappedAfterAllowPathRefused, TestFSWriteStaysInsideTheGrant
cap_fs.go: if !filepath.IsAbs(p) { return errors.New("desktop: AllowPath requires an absolute path") ->... -> TestFSAllowPathRequiresAbsolute
cap_fs_folder.go: if !filepath.IsAbs(p) || filepath.Clean(p) != p { -> if !filepath.IsAbs(p) { -> TestFolderPickGrantsReadUnderIt
cap_fs_folder.go: if live, err := filepath.EvalSymlinks(folder); err != nil || live != resolved { -> if false { -> TestFolderPickGrantsReadUnderIt
cap_fs_folder.go: return errPathNotAllowed } -> return &Error{Code: CodeInternal, Message: err.Error()} } -> TestFolderPickGrantsReadUnderIt
cap_fs_folder.go: root, err := os.OpenRoot(resolved) -> root, err := os.OpenRoot("/") rel = filepath.Join(reso... -> TestFolderPickRefusesARelativeLinkOut
cap_fs.go: if resolved := resolveExistingAncestor(p); resolved != "" && resolved != clean { -> if resol... -> TestSaveTargetUnderALinkedDirIsWritable
cap_fs.go: if err := fileperm.RestrictOpenFile(f); err != nil { -> if err := fileperm.RestrictOpenFile(... -> TestFSWriteCreates0600AndRoundTrips
cap_fs.go: if len(data) > fsReadLimit { -> if len(data) > 2*fsReadLimit { -> TestFSOversizeReadRefused
cap_dialogs.go: if err := b.AllowFolder(path); err != nil { -> if err := b.AllowPath(path); err != nil { -> TestFolderPickGrantsReadUnderIt
cap_dialogs.go: paths[i] = filepath.Clean(p) -> paths[i] = p -> TestDialogsAnswerTheCleanPath
cap_fs_folder.go: if len(entries) > fsListLimit { -> if len(entries) > 2*fsListLimit { -> TestFolderListIsCapped
cap_state.go: if err := b.Emit("state_changed", map[string]any{"key": key}); err != nil { -> if err := err... -> TestStateChangedReachesSecondWindow
cap_clipboard.go: Permission: "clipboard:read", ->  -> TestClipboardCapability, TestClipboardRoundTripThroughThePage
cap_clipboard.go: if len(text) > clipboardTextLimit { -> if false { -> TestClipboardReadByteLimit
cap_notifications.go: if len(req.Body) > 2000 { -> if false { -> TestNotificationsCapabilityValidation
cap_notifications.go: Permission: "notifications:show", ->  -> TestNotificationGateAsThePageSeesIt, TestNotificationsCapabilityValidation, _allow_persists
cap_state.go: if !strings.HasPrefix(key, pageKeyPrefix) { -> if !strings.HasPrefix(key, pageKeyPrefix) && ... -> TestStateKeysConfinedToPagePrefix
preferences.go: if _, dup := seen[p.Key]; dup { -> if _, dup := seen[p.Key]; dup && false { -> TestNewRejectsBadPreferenceDeclarations
preferences.go: if p.Min != nil && p.Max != nil && *p.Min > *p.Max { -> if false { -> TestNewRejectsBadPreferenceDeclarations
preferences.go: if def, ok := p.Default.(int); ok && def < *p.Min { -> if def, ok := p.Default.(int); ok && ... -> TestNewRejectsBadPreferenceDeclarations
preferences.go: if !slices.Contains(p.Choices, def) { -> if false { -> TestNewRejectsBadPreferenceDeclarations
preferences.go: if p.Kind != PreferenceChoice && len(p.Choices) > 0 { -> if false { -> TestNewRejectsBadPreferenceDeclarations
preferences.go: if len(raw) == 0 || string(raw) == "null" { -> if len(raw) == 0 { -> TestPrefNullRefused
preferences.go: if isText && (text == "true" || text == "false") { -> if false { -> TestPreferencesFormBlankIntKeepsTheStoredValue, TestPreferencesFormSavesThroughTheRoute
preferences.go: if !slices.Contains(d.Choices, text) { -> if false { -> TestPreferencesSetCoercesAndRefuses, TestSetRawIsAllOrNothing
preferences.go: if d.Max != nil && n > *d.Max { -> if false { -> TestPreferencesFormBadIntAnswersTheEnvelope, TestPreferencesSetCoercesAndRefuses, TestPreferencesSetRefusesWholeCall
preferences.go: if strings.ContainsAny(text, "\\r\ ") { return fmt.Errorf -> if strings.ContainsAny(text, "\... -> TestStringDefaultsRefuseLineBreaks
preferences.go: if strings.ContainsAny(text, "\\r\ ") { return nil, &preferenceError{key: key, reason: reaso... -> TestStringRoutesRefuseLineBreaks, TestStringWritesRefuseLineBreaks
preferences.go: if text == strings.NewReplacer("\\r", "", "\ ", "").Replace(stored) { continue } ->  -> TestLegacyStringFormSave
preferences.go: if len(encoded) > appstate.MaxValueBytes { -> if len(encoded) > 2*appstate.MaxValueBytes { -> TestPreferenceEncodedSizeLimit, TestPreferenceSizeBatchIsAtomic, TestPreferenceSizeRouteErrors
preferences.go: for key := range coerced { delete(cur, key) } ->  -> TestPreferenceSizeBatchIsAtomic
preferences.go: if err := p.b.Emit("preferences_changed", map[string]any{"keys": applied}); err != nil { -> ... -> TestPreferencesSetAppliesAndEmits
preferences.go: return nil, &preferenceError{key: key, reason: reasonPrefUndeclared} -> continue -> TestPreferencesSetCoercesAndRefuses, TestPreferencesSetRefusesWholeCall
preferences.go: c.Choices = slices.Clone(p.Choices) ->  -> TestNewTakesACopyOfDeclarations
preferences.go: if v, err := coercePreference(d, raw); err == nil { return v } else { -> if v, err := coerce... -> TestStoredValueFailingItsKindFallsBackWithDefault
preferences.go: if d.Kind != PreferenceBool { panic( -> if false { panic( -> TestPreferencesReadersPanicOnWrongUse
preferences.go: s := p.b.stateStore() if s == nil { return nil, &Error{Code: CodeUnsupported -> s := p.b.sta... -> TestPreferencesBeforeRunAnswerDeclaredDefaults
preferences_screen.go: if name == csrfFormSkip { continue } ->  -> TestPreferencesFormSavesThroughTheRoute
preferences_screen.go: fieldErrs[name] = &preferenceError{key: name, reason: reasonPrefUndeclared} continue -> cont... -> TestPreferencesFormBadIntAnswersTheEnvelope
preferences_screen.go: if !validNavigatePath(path) { panic( -> if false { panic( -> TestPreferencesScreenPathRefusesOffGrammar
preferences_screen.go: err != nil || mt != "application/json" { writePreferencesEnvelope -> err != nil || mt == "" ... -> TestPreferencesFormRouteRefusals
preferences_screen.go: if p.decls[d].Kind == PreferenceInt && string(form[name]) == `""` { -> if p.decls[d].Kind ==... -> TestPreferencesFormBlankIntKeepsTheStoredValue
preferences_screen.go (both 503 guards): stateStore()==nil and CodeUnsupported branches disabled -> TestPreferencesFormRouteRefusals
cap_preferences.go: return &Error{Code: CodeInvalidInput, Message: pe.Error()} -> return pe -> TestPreferencesSetRefusesWholeCall
preferences_screen.go: html.Input(html.InputConfig{Type: "hidden", Name: d.Key, Value: "false"}), ui.Checkbox(ui.To... -> TestPreferencesScreenRendersEveryDeclaredField
update.go: if cfg.MacOSAllowAdHoc { return errors.New -> if false { return errors.New -> TestUpdateConfigValidation
update.go: "if (c < A || c > Z) && (c < 0 || c > 9) {" -> "if c ==   {" -> TestUpdateConfigValidation
update.go: if !u.installMu.TryLock() { return errUpdateInProgress } defer u.installMu.Unlock() -> u.ins... -> (test binary crashed or failed to start)
update.go: if u.installed { return errUpdateInstalled } ->  -> TestInstallRefusesConcurrentInstall
update.go: if info.ShortVersion != r.release.Version { -> if false { -> TestInstallRefusesMislabelledBundle
update.go: if id := platform.Identifier(); id == "" || info.Identifier != id { -> if id := platform.Ide... -> TestInstallRefusesMislabelledBundle
update.go: if _, err := os.Lstat(unit.Current); err != nil { -> if _, err := os.Lstat(unit.Current); er... -> TestInstallRetryAfterFailedRollbackKeepsOldBundle
update.go: u.rollback(units[:i]) return false } if err := u.renameFile(beside, unit.Current); err != ni... -> TestFileLayoutInstallRollsBackEarlierFiles
update.go: if u.notified == res.Version || u.notifying { -> if u.notified == res.Version { -> TestUpdateNotifySharesInflight
update.go: u.mu.Lock() u.notified = res.Version u.mu.Unlock() u.b.logger.Info -> u.b.logger.Info -> TestScheduledCheckEmitsEventOncePerVersion, TestUpdateNotifyRetriesDelivery
update.go: if cfg.Channel[i] < 0x20 || cfg.Channel[i] == 0x7f { -> if cfg.Channel[i] < 0x20 && false { -> TestUpdateConfigValidation
update.go: if n := len(cfg.Channel); n > 64 { -> if n := len(cfg.Channel); n > 6400 { -> TestUpdateConfigValidation
internal/update/feed.go: if len(manifest) > MaxManifestSize { -> if false { -> TestParseFeedRejectsOversizeManifest
internal/update/feed.go: if err := VerifyFeedSignature(manifest, sigFile, pub); err != nil { return nil, err } var f ... -> TestParseFeedRejectsBadSignatureBeforeParse
internal/update/feed.go: if _, err := ParseVersion(ch.Version); err != nil { -> if _, err := ParseVersion(ch.Version)... -> TestFeedRefusesInvalidChannelVersion
internal/update/fetch.go: return u.Hostname() == "127.0.0.1" -> return true -> TestAllowedURLSchemeRules, TestFetchRefusesDisallowedURLBeforeDialing, TestFetchRefusesRedirectOffRule
internal/update/fetch.go: if !AllowedURL(req.URL.String()) { return ErrBadURL -> if false { return ErrBadURL -> TestFetchRefusesRedirectOffRule
internal/update/fetch.go: if int64(len(body)) > max { -> if false { -> TestFetchRefusesBodyOverCap
internal/update/fetch.go: if !AllowedURL(raw) { return nil, ErrBadURL } if max -> if max -> TestFetchRefusesDisallowedURLBeforeDialing
internal/update/archive.go: if strings.Contains(raw, "\\\\") { -> if false { -> TestCleanEntryNameRefusesEscapes
internal/update/archive.go: case mode.Type() != 0: -> case false: -> TestExtractZipRefusesSymlinkEntry
internal/update/archive.go: return (mode.Perm() | 0o700) &^ 0o022 -> return mode.Perm() | 0o700 -> TestExtractDirectoriesRefusePublicWrites
internal/update/archive.go: return mode.Perm() &^ 0o022 -> return mode.Perm() -> TestExtractDropsGroupOtherWrite
internal/update/trust.go: if exe.State == Untrusted { -> if false { -> TestCheckWindowsTrustPolicy, _update_broken
internal/update/trust.go: if len(running.Subject) == 0 || !bytes.Equal(running.Subject, exe.Subject) { -> if len(runni... -> TestCheckWindowsTrustPolicy
internal/update/trust.go: if runningDLL.State == Trusted && (len(runningDLL.Subject) == 0 || !bytes.Equal(runningDLL.S... -> TestCheckWindowsTrustPolicy, _unsigned_app
internal/update/trust.go: if dll.State != Trusted { -> if false { -> TestCheckWindowsTrustPolicy, _new_one_unsigned
internal/update/feed.go: if !now.Before(expires) { -> if expires.IsZero() { -> TestParseFeedRefusesExpired
internal/update/fetch.go: if prev != nil { return prev(req, via) } -> _ = prev -> TestFetchKeepsRedirectPolicy
internal/update/archive.go: if !sawRoot { -> if !sawRoot && false { -> TestExtractZipNeedsTheNamedRootDirectory
internal/update/archive.go: if !found || top != root || name == root { -> if !found || (top != root && false) || name ==... -> TestExtractZipNeedsTheNamedRootDirectory
internal/update/semver.go: case len(a.Pre) == 0: return 1 default: return -1 -> case len(a.Pre) == 0: return -1 default... -> TestCompareOrdersVersions, TestIsNewerRules
internal/update/semver.go: return cmpInt(len(a.Pre), len(b.Pre)) -> return 0 -> TestCompareOrdersVersions
internal/update/apply.go: return pe.Op + ": " + pe.Err.Error() -> return pe.Error() -> TestLogDetailDropsPaths
internal/update/apply.go: return "not a file-system error" -> return err.Error() -> TestLogDetailDropsPaths
internal/update/rename_linux.go: unix.RENAME_NOREPLACE -> 0 -> TestRenameRefusesAppearedDestination
internal/update/feedwrite.go: if !constantTimeEqualString(want, pubHex) { -> if false && !constantTimeEqualString(want, pu... -> TestLoadPrivateKeyFormats
internal/update/feedwrite.go: if validFor < 0 || validFor > MaxFeedLifetime { -> if validFor < 0 { -> TestWriteFeedRefusesBadLifetime
internal/update/feedwrite.go: return atomicfile.WriteFile(path, data, atomicfile.Mode(0o644)) -> return atomicfile.WriteFi... -> TestFeedFilesArePublic
internal/update/bundleinfo.go: if seen[key] { -> if seen[key] && false { -> TestParseInfoPlistRefusesAmbiguity
internal/update/bundleinfo.go: if !seen["CFBundleIdentifier"] || !seen["CFBundleShortVersionString"] { -> if false { -> TestParseInfoPlistRefusesAmbiguity
internal/update/predecessor.go: _ = os.Unsetenv(PredecessorEnv) ->  -> TestAwaitPredecessorConsumesTheVariable
internal/update/predecessor.go: if err != nil || pid <= 0 || pid == os.Getpid() { -> if err != nil { -> TestAwaitPredecessorConsumesTheVariable
internal/update/feedwrite.go: if sigErr != nil || VerifyFeedSignature(existing, existingSig, priv.Public().(ed25519.Public... -> TestFeedRefusesForeignManifest
internal/update/apply.go: if _, err := os.Lstat(dst); err == nil { return errMoveDestExists -> if _, err := os.Lstat(d... -> TestMoveTreeRefusesExistingDestination
cap_updates.go: Permission: "updates:install", ->  -> TestUpdatesCapabilityShape, TestUpdatesCheckAndInstallThroughThePage
appstate/appstate.go: if len(data) > MaxValueBytes { -> if false { -> TestOversizeValueRefused
appstate/appstate.go: if err := jsonexact.Unmarshal(data, out); err != nil { -> var _ = jsonexact.Unmarshal if err... -> TestGetKeepsBigIntegers
appstate/appstate.go: if _, ok := s.entries[key]; ok { delete(s.entries, key) s.scheduleLocked() } -> delete(s.ent... -> TestDeleteAbsentWritesNothing
appstate/appstate.go: s.writeMu.Lock() defer s.writeMu.Unlock() s.mu.Lock() if s.timer != nil { -> s.mu.Lock() if ... -> TestWriteSnapshotsUnderWriteLock
appstate/appstate.go: logger.Error("appstate: watcher panicked", "key", key, "panic", textsafe.Recovered(v)) -> pa... -> (test binary crashed or failed to start)
appstate/appstate.go: s.mu.Lock() s.dirty = true s.mu.Unlock() return err -> return err -> TestFailedFlushKeepsTheChangeDirty
appstate/appstate.go: if err := fileperm.RestrictOpenFile(tmp); err != nil { -> var _ = fileperm.RestrictOpenFile ... -> TestFileModeIs0600
appstate/appstate.go: if err := atomicfile.Replace(name, s.path); err != nil { -> var _ = atomicfile.Replace if er... -> TestPersistIsAtomicReplace, TestRacingWritesNeverPartial, TestSymlinkedStateFileNotFollowed
appstate/appstate.go: if err := json.Unmarshal(data, &f); err != nil || f.Entries == nil { logger.Warn( -> if err ... -> TestCorruptFileIsWarnAndEmpty
appstate/appstate.go: if !reKey.MatchString(key) { return fmt.Errorf("%w: %q", ErrKey, key) } data, err := json.Ma... -> TestBadKeyRefused
native/doc.go: if cfg.Shell == nil { cfg.Shell = Shell() } -> cfg.Shell = Shell() -> TestNewFillsShellAndKeepsExplicit
native/native_linux.go: func Shell() desktop.Shell { return linux.New() } -> func Shell() desktop.Shell { _ = linux.... -> TestShellMatchesPlatform
native/native_other.go: //go:build (!darwin && !windows && !linux) || gofastr_nodesktop -> //go:build (!darwin && !w... -> TestNoDesktopTagKeepsLinuxBuildStatic
native/native_linux.go+native_other.go: gofastr_nodesktop dropped from both build tags -> TestNoDesktopTagKeepsLinuxBuildStatic
ui/inspector.go: if cfg.Label == "" { -> if false { -> TestInspectorRequiresLabel
ui/overlay.go: if cfg.Title == "" { panic("desktopui: Sheet requires Title") -> if false { panic("desktopui... -> TestSheetRequiresTitle
ui/sourcelist.go: class += " active" ->  -> TestSourceListStampsRuntimeActiveClass
ui/layout.go: const SidebarTopInset = 52 -> const SidebarTopInset = 40 -> TestLayoutCSS, TestSidebarTopInsetMeasured
ui/theme.go: t.Colors.Primary = style.Color{Name: "primary", Value: "#0071E3"} -> t.Colors.Primary = styl... -> TestPrimaryStaysAAUnderWhite, TestThemeValidates
ui/theme.go: t.Layout.TouchTarget = style.Spacing{Name: "touch-target", Value: 24} -> t.Layout.TouchTarge... -> TestThemeTouchTargetIsPointerSized
ui/glass.go: html.desktop-reduce-transparency [data-cui-comp="desktopui-glass"], html.desktop-reduce-tran... -> TestGlassAccessibilityAndInactiveRules
ui/glass.go: html.desktop-inactive [data-cui-comp="desktopui-glass"], html.desktop-inactive [data-cui-com... -> TestGlassAccessibilityAndInactiveRules
ui/layout.go: html:has(.desktopui-frame), body:has(.desktopui-frame) { background-color: transparent; } ->... -> TestLayoutCSS
desktoptest/shell.go: if len(f.promptDecisions) == 0 { return desktop.DecisionDeny, nil } -> if len(f.promptDecisi... -> TestNotificationGateAsThePageSeesIt
desktoptest/window.go: w.focuses++ w.hidden = false -> w.focuses++ -> TestFocusShowsAHiddenMainWindow
desktoptest/harness.go: if os.Getenv(dataDirEnv) == "" { t.Setenv(dataDirEnv, t.TempDir()) } -> _ = os.Getenv -> TestRunPointsTheDataDirAtATempDir
internal/ffi/call_linux_amd64.go: if count > intRegs+len(callArgs{}.stks) { -> if count > intRegs+len(callArgs{}.stks)+1 { -> TestBounds, TestLinuxCallBoundsBeforeFFI
internal/ffi/call_linux_amd64.go: if len(floats) > len(callArgs{}.flts) { -> if len(floats) > len(callArgs{}.flts)+1 { -> TestBounds, TestLinuxCallBoundsBeforeFFI
internal/ffi/callback_linux.go: if callbackLen >= MaxCallbacks { -> if callbackLen > MaxCallbacks { -> TestNewCallbackSlotExhaustion
internal/ffi/callback_linux_amd64.go: const entrySize = 11 -> const entrySize = 10 -> (test binary crashed or failed to start)
internal/ffi/callback_linux.go: callbackWrapABI = *(*uintptr)(fv) -> callbackWrapABI = uintptr(fv) -> (test binary crashed or failed to start)
internal/ffi/callback_linux.go: if !free && ThreadCheck != nil { -> if _ = free; ThreadCheck != nil { -> (test binary crashed or failed to start)
internal/fakecgo/callbacks_linux.go: var noopCrosscall2 = func() {} -> var noopCrosscall2 func() -> (test binary crashed or failed to start)
internal/fakecgo/callbacks_linux.go: //go:cgo_import_dynamic libc_setenv setenv "libc.so.6" -> //go:cgo_import_dynamic libc_seten... -> (test binary crashed or failed to start)
internal/ffi/runtime_nocgo.go: import _ "github.com/DonaldMurillo/gofastr/battery/desktop/internal/fakecgo" ->  -> (test binary crashed or failed to start)
macos/shell_darwin_caps.go: if err := s.onMainWithTimeout(modalTimeout, func() { panel := objc.ID(objc.Send(objc.ID(objc... -> TestModalsWaitForThePerson
internal/gtk/gtk_linux.go: lib.soname, installLine, lib.packages, err) -> lib.soname, "", lib.packages, err) -> TestMissingLibraryNamesThePackages
internal/gtk/dl_linux.go: return 0, fmt.Errorf("dlopen %s: %s: %w", path, DLError(), ErrUnavailable) -> return 0, fmt.... -> TestMissingLibraryNamesThePackages
internal/gtk/gtk_linux.go: if lib.optional { continue } -> if lib.optional && false { continue } -> TestMissingLibraryNamesThePackages
internal/gtk/gtk_linux.go: {&Sym.GtkInitCheck, libGtk, "gtk_init_check"}, ->  -> TestTableCoversEverySymbolsField
internal/placement/restore.go: if overlapWidth >= 100 && overlapHeight >= 50 { -> if overlapWidth >= 1 && overlapHeight >= 1 { -> TestRestoreDropsInvisibleFrames
internal/placement/restore.go: width := min(max(defaultWidth, 1), screen.Width, 32767) -> width := min(max(defaultWidth, 1)... -> TestRestoreFitsSmallWorkArea
linux/navpolicy.go: return mainResource && !allow(uri) -> return !allow(uri) -> TestRefuseNavigationChecksEveryAction
linux/navpolicy.go: return uri != "about:srcdoc" && !allow(uri) -> return !allow(uri) -> TestRefuseNavigationChecksEveryAction
internal/win32/monitors.go: if overlapWidth >= math.Round(100*scale) && overlapHeight >= math.Round(50*scale) { -> if ov... -> TestRestoreMonitorFramePhysicalOverlapThreshold
## auth, before the merge with main

- `cl-apitok-r` battery/auth/account_claim.go [tokens, _, err := m.revokeUserAPITokens(ctx, userID) -> tokens, err := 0, error(nil)] -> TestMagicLinkClaimRevokesSquatterAPITokens
- `cl-clear` battery/auth/account_claim.go [if err := clearOrPlaceholder(ctx, clearer, setter, userID) -> if err := error(nil); err != nil {] -> TestMagicLinkClaimKillsSquatter
- `cl-mark-last` battery/auth/account_claim.go [if len(failed) > 0 { return errors.Join(failed...) -> if false { return errors.Join(failed...)] -> TestClaimPurgeFailStillRevokes
- `cl-passkeys2` battery/auth/account_claim.go [passkeys, err := m.purgeUserPasskeys(ctx, userID) -> passkeys, err := 0, error(nil)] -> TestPasskeyClaimRemovesPasskeys
- `cl-provider-tokens` battery/auth/account_claim.go [tokenErr := m.dropProviderTokens(ctx, userID, removed) -> tokenErr := error(nil)] -> TestAccountClaimDropsProviderTokens
- `cl-runs-all` battery/auth/account_claim.go [failed = append(failed, fmt.Errorf("revoke sessions: %w", -> return err } removed] -> TestClaimSessionFailStillPurges
- `cl-sessions-both3` battery/auth/account_claim.go [both purger.DeleteByUser calls -> 0, nil] -> TestMagicLinkClaimKillsSquatter
- `cl-sessions-early` battery/auth/account_claim.go [early, err := purger.DeleteByUser(ctx, userID) -> early, err := 0, error(nil)] -> TestLinkLandsMidClaim
- `cl-token-fail-r` battery/auth/account_claim.go [if tokenErr != nil { -> if tokenErr != nil && false {] -> TestAccountClaimFailsWhenTokensSurvive
- `cl-unlink-r` battery/auth/account_claim.go [removed, err := m.unlinkAllOAuth(ctx, userID, lister, unlin -> removed, err := []string(nil), error(nil) _, _ = lister, u] -> TestClaimDropsSquatterOAuthLink
- `ac-atomic` battery/auth/accounts.go [if atomicUnlinker, ok := p.mgr.UserStore().(AtomicUnlinker) -> if atomicUnlinker, ok := p.mgr.UserStore().(AtomicUnlinker)] -> TestUnlinkAtomicMethodCount
- `ac-pending-r` battery/auth/accounts.go [if sess.PendingTwoFactor { -> if sess.PendingTwoFactor && false {] -> TestAccountsRefusePendingSession
- `ac-principal` battery/auth/accounts.go [sess, err := p.mgr.requestSession(r, false) -> sess, err := func() (*Session, error) { ss := p.mgr.request] -> TestAccountsBoundToPrincipal
- `ac-storable-r` battery/auth/accounts.go [if !textsafe.Storable(provider) { -> if !textsafe.Storable(provider) && false {] -> TestUnlinkRefusesUnstorableProvider
- `ac-tokens` battery/auth/accounts.go [if err := p.revokeProviderTokens(r.Context(), userID, prov -> if err := error(nil); err != nil {] -> TestUnlinkDropsStoredProviderTokens
- `at-display-r` battery/auth/apitoken.go [if i := strings.IndexByte(plaintext, '_'); i >= 0 && i < ma -> if i := strings.IndexByte(plaintext, '_'); (i >= 0 && i < m] -> TestTokenDisplayPrefixKeepsRandomChars
- `at-embed-scopes` battery/auth/apitoken.go [if g, ok := embed.GrantFromContext(ctx); ok { -> if g, ok := embed.GrantFromContext(ctx); ok && false {] -> TestGrantScopesGateLikeTokenScopes
- `at-hash-only` battery/auth/apitoken.go [return hex.EncodeToString(sum[:]) } -> _ = sum return s }] -> TestAPIToken_PlaintextNeverStored
- `at-name-r` battery/auth/apitoken.go [if !textsafe.Storable(spec.Name) \|\| len(spec.Name) > maxTok -> if (!textsafe.Storable(spec.Name) \|\| len(spec.Name) > maxTo] -> TestIssueTokenRefusesBadNameAndNegativeTTL
- `at-neg-ttl-r` battery/auth/apitoken.go [if spec.TTL < 0 { -> if spec.TTL < 0 && false {] -> TestIssueTokenRefusesBadNameAndNegativeTTL
- `at-ownerkind` battery/auth/apitoken.go [if spec.OwnerKind != OwnerKindUser && spec.OwnerKind != Own -> if false {] -> TestIssueToken_ValidatesSpec
- `at-prefix` battery/auth/apitoken.go [if spec.Prefix != "" && !tokenPrefixPattern.MatchString(spe -> if false {] -> TestTokenPrefixBranding
- `at-scope-shape` battery/auth/apitoken.go [if !scopePattern.MatchString(sc) { -> if false {] -> TestIssueToken_ValidatesSpec
- `at-scopes-cap` battery/auth/apitoken.go [if len(spec.Scopes) > maxTokenScopes { -> if false {] -> TestIssueToken_ValidatesSpec
- `at-session-unscoped` battery/auth/apitoken.go [return true // session/JWT: unscoped by design -> return false] -> TestRequireScope_SessionPasses
- `tm-tokenid-r` battery/auth/apitoken.go [return context.WithValue(ctx, tokenIDKey{}, id) -> return ctx] -> TestTokenMiddleware_ExposesTokenID
- `au-token-deliver` battery/auth/apitoken_middleware.go [deliverAudit(ctx, sink, SecurityEvent{ -> if sink == nil { return } sink.SecurityEvent(ctx, Secur] -> TestTokenAuditGoesThroughDeliverAudit
- `tm-api-every-r` battery/auth/apitoken_middleware.go [if !scopeMatches(held, resource+":"+verb) { -> if (!scopeMatches(held, resource+":"+verb)) && false {] -> TestAPIScopes_UnroutedNeedsEveryCandidate
- `tm-api-verb` battery/auth/apitoken_middleware.go [if r.Method == http.MethodGet \|\| r.Method == http.MethodH -> if true {] -> TestAPIScopes_DerivedFromRoute
- `tm-disabled` battery/auth/apitoken_middleware.go [if sa.Disabled { -> if false {] -> TestAPIToken_DisabledServiceAccountAnonymous
- `tm-expired-r` battery/auth/apitoken_middleware.go [if t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()) { -> if t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()) &] -> TestAPIToken_ExpiredTokenClearsUser
- `tm-failclosed` battery/auth/apitoken_middleware.go [next.ServeHTTP(w, r.WithContext(handler.SetUser(ctx, nil -> next.ServeHTTP(w, r) }] -> TestAPIToken_UnknownTokenClearsUser, TestAPIToken_RevokedTokenClearsUser
- `tm-garbage-kind` battery/auth/apitoken_middleware.go [default: return nil, "owner_missing" -> default: return &BasicUser{ID: t.OwnerID}, ""] -> TestGarbageOwnerKindFailsClosed
- `tm-passthrough` battery/auth/apitoken_middleware.go [if !strings.HasPrefix(cred, o.prefix) { -> if false {] -> TestAPIToken_NonGfskBearerPassesThrough
- `tm-prefix-opt-r` battery/auth/apitoken_middleware.go [if !tokenPrefixPattern.MatchString(prefix) { -> if !tokenPrefixPattern.MatchString(prefix) && false {] -> TestWithTokenPrefixPanicsOnBadBrand
- `tm-recheck` battery/auth/apitoken_middleware.go [if err != nil \|\| t == nil \|\| t.RevokedAt != nil { retur -> if err != nil \|\| t == nil { return ctx, false] -> TestEventStreamClosesOnRevocation
- `tm-revoked` battery/auth/apitoken_middleware.go [if t.RevokedAt != nil { -> if false {] -> TestAPIToken_RevokedTokenClearsUser
- `tm-touch` battery/auth/apitoken_middleware.go [if shouldTouchLastUsed(t.LastUsedAt) { -> if true {] -> TestTokenMiddleware_LastUsedThrottle
- `ar-embed-caller-r` battery/auth/apitoken_routes.go [if _, embedded := embed.GrantFromContext(r.Context()); embe -> if _, embedded := embed.GrantFromContext(r.Context()); embe] -> TestEmbedGrantCannotMintAPITokens
- `ar-embed-prefix` battery/auth/apitoken_routes.go [return p.mgr.ReservedEmbedPrefixes() -> return []string{p.prefix}] -> TestAuthReservesItsBasePathFromEmbeds
- `ar-fence-r` battery/auth/apitoken_routes.go [if snap.moved(r.Context(), p.mgr) \|\| !sessionStillHolds(r. -> if (snap.moved(r.Context(), p.mgr) \|\| !sessionStillHolds(r] -> TestTokenCreateOvertakenByReset, TestTokenCreateAfterResetBeforeSnapshot
- `ar-owner-session` battery/auth/apitoken_routes.go [OwnerID: userID, -> OwnerID: body.OwnerID,] -> TestAPIToken_CreateIgnoresBodyOwner
- `ar-revoke-scope` battery/auth/apitoken_routes.go [if err := p.tokens.Revoke(r.Context(), id, OwnerKindUser, -> if err := p.tokens.(*SQLAPITokenStore).RevokeAny(r.Context] -> TestAPIToken_RevokeOwnerScopedHTTP
- `ar-stepup-r` battery/auth/apitoken_routes.go [if !verified { -> if (!verified) && false {] -> TestTokenMintNeedsSteppedUpSessionOn2FAAccount
- `ar-store-err` battery/auth/apitoken_routes.go [if errors.Is(err, ErrInvalidTokenSpec) { -> if true {] -> TestTokenCreate_StoreErrNotLeaked
- `ar-token-caller` battery/auth/apitoken_routes.go [if _, tokenAuth := TokenScopes(r.Context()); tokenAuth { -> if _, tokenAuth := TokenScopes(r.Context()); tokenAuth && f] -> TestTokensPlugin_RejectsTokenAuthCaller
- `ar-ttl-wrap` battery/auth/apitoken_routes.go [if body.TTLSeconds < 0 \|\| body.TTLSeconds > maxTokenTTLSec -> if body.TTLSeconds < 0 {] -> TestTokenMintRefusesWrappingTTL
- `as-erase-user-only` battery/auth/apitoken_sql.go [Where: []datexport.Equals{{Column: "owner_kind", Value: Ow ->] -> TestEraseRemovesUserAPITokens
- `as-revoke-idem2` battery/auth/apitoken_sql.go [return nil // idempotent: already revoked -> return ErrTokenNotFound] -> TestRevoke_IdempotentAndOwnerScoped
- `as-revoke-owner` battery/auth/apitoken_sql.go [`SELECT revoked_at FROM %s WHERE id = $1 AND owner_kind = $2 -> `SELECT revoked_at FROM %s WHERE id = $1 AND owner_kind = $2] -> TestRevoke_IdempotentAndOwnerScoped
- `as-revoked-failclosed` battery/auth/apitoken_sql.go [epoch := time.Unix(0, 0).UTC() return &epoch -> return nil] -> TestTokenUnreadableRevokedAtFailsClosed
- `au-bounded` battery/auth/audit.go [sctx, cancel := context.WithTimeout(context.WithoutCancel(c -> sctx, cancel := context.WithCancel(context.WithoutCancel(ct] -> TestAuditCustomSinkContextBounded
- `au-dash` battery/auth/audit.go [if recordID == "" { -> if false {] -> TestSQLAuditSink_EmptyUserIDRecordDash
- `au-detach` battery/auth/audit.go [sctx, cancel := context.WithTimeout(context.WithoutCancel(c -> sctx, cancel := context.WithTimeout(ctx, auditWriteTimeout)] -> TestAuditRowSurvivesCancelledRequest
- `au-hash-canon` battery/auth/audit.go [if c, err := CanonicalEmail(addr); err == nil { -> if c, err := CanonicalEmail(addr); err == nil && false {] -> TestAuditEmailHashIsCanonical
- `au-noaddr` battery/auth/audit.go [diff["email_hash"] = hashedIdentifier(addr) -> diff["email"] = addr] -> TestAuditActorlessRowHoldsNoAddress
- `au-recover` battery/auth/audit.go [if r := recover(); r != nil { -> if r := recover(); r != nil { panic(r)] -> TestAudit_PanickingSinkRecovered
- `au-scrub-r` battery/auth/audit.go [textsafe.Recovered(r) -> r, "x", textsafe.Recovered] -> TestAuthAuditRedPanicScrubbed
- `ui-csrf-fallback-r` battery/auth/authui/authui.go [if middleware.TokenFromContext(ctx) == "" && s.d.CSRFField -> if (middleware.TokenFromContext(ctx) == "" && s.d.CSRFField] -> TestConfirmScreenCSRFFallback
- `ui-csrf-once-r` battery/auth/authui/authui.go [if middleware.TokenFromContext(ctx) == "" && s.d.CSRFField -> if (middleware.TokenFromContext(ctx) == "" \|\| true) && s.d.] -> TestConfirmScreenSignsIn
- `ui-hop-link` battery/auth/authui/authui.go [Body: ui.Link(ui.LinkConfig{Href: s.d.Target, Text: s.d.C -> Body: ui.Link(ui.LinkConfig{Href: "/", Text: s.d.Continue] -> TestHopScreenLinksBack
- `ui-post` battery/auth/authui/authui.go [Method: http.MethodPost, -> Method: http.MethodGet,] -> TestConfirmScreenSignsIn
- `bf-bearer` battery/auth/bff.go [if len(parts) != 2 \|\| !strings.EqualFold(parts[0], "bearer" -> if len(parts) != 2 \|\| parts[0] != "Bearer" {] -> TestBFFBearerRejectedEverySpelling
- `bf-exact-origin` battery/auth/bff.go [if _, ok := allowed[origin]; !ok { -> if false {] -> TestBFFPostureRejectsUntrustedOriginsAndJWTs
- `bf-formpost` battery/auth/bff.go [if r.Method == http.MethodPost && isFormPostCallback(auth -> if false {] -> TestBFFPostureLetsFormPostCallbackThrough
- `bf-prefix` battery/auth/bff.go [if !tokenPrefixPattern.MatchString(cfg.TokenPrefix) { -> if false {] -> TestBFFRejectsInvalidTokenBrand
- `bf-segment` battery/auth/bff.go [if apiPrefix != "/" && -> if false &&] -> TestBFFGuardPrefixIsSegmentScoped
- `core-2fa-failclosed` battery/auth/core.go [if err != nil && !errors.Is(err, ErrTwoFactorEnforcement) { -> if err != nil && !errors.Is(err, ErrTwoFactorEnforcement) &&] -> TestLoginSessionCreateFailureIsNotATwoFactorFailure
- `core-2fa-redirect` battery/auth/core.go [http.Redirect(w, r, safeRedirectURL(c.twoFactorURL), http.St -> http.Redirect(w, r, successRedirect(w, r, "/"), http.StatusS] -> TestCoreFormLoginOf2FAAccountGoesToTwoFactorURL
- `core-2fa-url` battery/auth/core.go [if _, ok := mgr.plugins[name].(TwoFactorChecker); ok { -> if _, ok := mgr.plugins[name].(TwoFactorChecker); ok && fals] -> TestCoreInitRequiresTwoFactorURLWith2FA
- `core-acct-limit` battery/auth/core.go [allowed, retry := c.loginLimitAccount.AllowContext(r.Context -> allowed, retry := true, time.Duration(0); _ = key] -> TestRateLimit_PerAccount_BlocksIPRotationAttack
- `core-baseurl-host` battery/auth/core.go [if u.Host == "" { -> if false {] -> TestLinkBaseURLNeedsAHost
- `core-baseurl-scheme` battery/auth/core.go [if u.Scheme != "http" && u.Scheme != "https" { -> if false {] -> TestEmailBaseURLRedSchemeValidated
- `core-cookie-clear` battery/auth/core.go [c.MaxAge = -1 ->] -> TestSessionCookieAttributes
- `core-cookie-httponly` battery/auth/core.go [HttpOnly: true, Secure: cfg.SessionSecure, -> HttpOnly: false, Secure: cfg.SessionSecure,] -> TestSessionCookieAttributes
- `core-cookie-secure` battery/auth/core.go [Secure: cfg.SessionSecure, SameSite: http.SameSiteStri -> Secure: false, SameSite: http.SameSiteStrictMode,] -> TestSessionCookieAttributes
- `core-cookie-strict` battery/auth/core.go [SameSite: http.SameSiteStrictMode, -> SameSite: http.SameSiteLaxMode,] -> TestCorePlugin_LoginCookieUsesStrictSameSite, TestCorePlugin_LogoutCookieUsesStrictSameSite
- `core-cookieonly` battery/auth/core.go [if c.mgr.JWT() != nil && !pendingTwoFA && !cfg.CookieOnly { -> if c.mgr.JWT() != nil && !pendingTwoFA {] -> TestBFFPostureSuppressesLoginJWT
- `core-cred-ct` battery/auth/core.go [if err != nil \|\| row == nil \|\| row.GetID() != userID { -> if err != nil \|\| row == nil {] -> TestLoginRefusedWhenEmailNowNamesAnotherUser
- `core-cred-recheck` battery/auth/core.go [if !credentialUnchanged(r.Context(), store, email, user.GetI -> if false {] -> TestLoginLosesRaceToReset
- `core-csd-panic` battery/auth/core.go [if !clearSiteDataDirectives[d] { -> if false {] -> TestLogoutClearSiteDataConfigurable
- `core-dev-ip` battery/auth/core.go [ipCfg.DevMode = cfg.DevMode -> ipCfg.DevMode = false] -> TestRateLimit_DevModeRelaxesPerIPLogin
- `core-dummy-hash` battery/auth/core.go [_ = verifyPassword(password, dummyHashFor(DefaultHasher)) ->] -> TestLogin_TimingSafe_NoEnumerationByResponseTime
- `core-encode-500` battery/auth/core.go [writeAuthError(w, r, http.StatusInternalServerError, i18n. -> w.WriteHeader(status) return] -> TestMeAnswersEncodeFailure
- `core-jwt-bound` battery/auth/core.go [token, err := c.mgr.JWT().generateToken(user, hash) -> token, err := c.mgr.JWT().generateToken(user, "")] -> TestJWTDiesWithCredentialChange
- `core-limit-form303` battery/auth/core.go [if isFormRequest(r) { writeFormAuthError(w, r, http.Statu -> if false { writeFormAuthError(w, r, http.StatusTooManyReq] -> TestRegisterFormRateLimit303
- `core-logout-all` battery/auth/core.go [range sessionCookieCandidates(r, cfg.SessionCookie) { -> range sessionCookieCandidates(r, cfg.SessionCookie)[:1] {] -> TestSessionReadsTryEveryCookieCandidate
- `core-logout-audit-after` battery/auth/core.go [revokeFailed = true continue -> revokeFailed = true] -> TestLogoutFailsClosedOnRevokeFailure
- `core-logout-failclosed` battery/auth/core.go [if revokeFailed { -> if revokeFailed && false {] -> TestLogoutFailsClosedOnRevokeFailure
- `core-me-gone` battery/auth/core.go [if errors.Is(err, ErrUserNotFound) { writeAuthError(w -> if false { writeAuthError(w, r, http.StatusUnauthoriz] -> TestSoftDeletedUserCannotSignIn
- `core-me-pending` battery/auth/core.go [if s.PendingTwoFactor { pending = true -> if false { pending = true] -> TestPendingTwoFA_Me_Rejected
- `core-nojwt-pending` battery/auth/core.go [if c.mgr.JWT() != nil && !pendingTwoFA && !cfg.CookieOnly { -> if c.mgr.JWT() != nil && !cfg.CookieOnly {] -> TestPendingTwoFA_NoJWTIssued
- `core-nostore` battery/auth/core.go [writeCredentialHeaders(w) w.Header().Set("Content-Type", " -> w.Header().Set("Content-Type", "application/json")] -> TestCredentialResponsesSetNoStore
- `core-notice-queue` battery/auth/core.go [c.notices.enqueue(r.Context(), holderEmail, func(ctx context -> func(f func(context.Context)) { f(r.Context()) }(func(ctx co] -> TestDuplicateNoticeBoundedAndStopped
- `core-reg-500` battery/auth/core.go [if err != nil && !taken { -> if err != nil && !taken && false {] -> TestRegisterStoreFailureIs500
- `core-reg-errpath` battery/auth/core.go [r = asRegisterForm(r) ->] -> TestRegisterErrorsLandOnRegisterPath
- `core-reg-notice` battery/auth/core.go [c.deliverRegisterDuplicateNotice(r, holder.GetEmail()) ->] -> TestRegisterDuplicateNotifiesHolder
- `core-reg-roles` battery/auth/core.go [roles := c.mgr.DefaultRoles() -> roles := []string{"admin"}] -> TestRegisterUsesConfiguredRoles
- `core-reg-uniform` battery/auth/core.go [w.WriteHeader(http.StatusAccepted) -> w.WriteHeader(http.StatusCreated)] -> TestRegisterNoEmailTakenOracle
- `core-reg-xsite-first-r` battery/auth/core.go [r = asRegisterForm(r) if rejectCrossSiteForm(w, r) { -> r = asRegisterForm(r) if !guardAuthLimit(c.registerLimit] -> TestCrossSitePostsDontBurnRateBudget
- `core-unphrased` battery/auth/core.go [writeAuthError(w, r, status, frame) } -> writeAuthErrorFields(w, r, status, frame, map[string]string] -> TestCanonicalizerErrorTextNotEchoed
- `core-xsite` battery/auth/core.go [if handler.IsCrossSiteRequest(r) { writeFormAuthError(w, r -> if false { writeFormAuthError(w, r, http.StatusForbidden,] -> TestLoginFormCrossOriginRejected, TestLoginFormCrossSiteFetchMetadataRejected, TestRegisterFormCrossOriginRejected
- `core-xsite-before-limit` battery/auth/core.go [if rejectCrossSiteForm(w, r) { return } if !guardAu -> if !guardAuthLimit(c.loginLimit, w, r) { return } i] -> TestLoginCrossSitePostsDontBurnLoginBudget
- `core-xsite-forgeable` battery/auth/core.go [if !handler.IsForgeableRequest(r) { -> if !isFormRequest(r) {] -> TestVerifyRejectsCrossSiteTextPlain, TestVerifyRejectsCrossSiteNoBody
- `cred-moved` battery/auth/credential_revoke.go [return !credentialUnchanged(ctx, m.UserStore(), s.email, s. -> return false] -> TestTokenCreateOvertakenByReset
- `cred-undo-ctx` battery/auth/credential_revoke.go [return context.WithTimeout(context.WithoutCancel(ctx), undo -> return context.WithTimeout(ctx, undoTimeout)] -> TestPasskeyUndoSurvivesClientHangup, TestTokenUndoSurvivesClientHangup
- `revoke-tokens` battery/auth/credential_revoke.go [if err := tp.tokens.Revoke(ctx, t.ID, "user", userID); err -> if err := error(nil); err != nil {] -> TestPasswordResetRevokesAPITokens
- `cs-ctx-first` battery/auth/csrf.go [if tok := middleware.TokenFromContext(r.Context()); tok != -> if tok := middleware.TokenFromContext(r.Context()); false &] -> TestCSRFInputFromCtx_MatchesRequestVariant
- `cs-host-first` battery/auth/csrf.go [for _, name := range []string{"__Host-" + CSRFCookieName, C -> for _, name := range []string{CSRFCookieName, "__Host-" + C] -> TestCSRFTokenPrefersHostPrefixedCookie
- `cs-skip-bearer` battery/auth/csrf.go [Skip: middleware.SkipAny(middleware.SkipBearerAuth(), embed. -> Skip: middleware.SkipAny(embed.CSRFExempt, framework.IsInPro] -> TestCSRF_SkipsBearerAuth
- `cs-skip-embed2` battery/auth/csrf.go [middleware.SkipAny(middleware.SkipBearerAuth(), embed.CSRFEx -> middleware.SkipAny(middleware.SkipBearerAuth(), func(r *http] -> TestCSRFExemptsEmbedEndpoints
- `cs-skip-prior` battery/auth/csrf.go [c.Skip = middleware.SkipAny(prior, skipper.Skip) -> c.Skip = skipper.Skip] -> TestCSRF_WithCSRFSkipPaths_PinsPerRouteExemption
- `dialect-fail-r` battery/auth/dialect.go [return migrate.DialectSQLite -> c.resolved.Store(new(migrate.Dialect)) return migrate.Di] -> TestFailedFirstProbeIsNotKept
- `dialect-keep-r` battery/auth/dialect.go [c.resolved.Store(&d) ->] -> TestStoreProbesDialectOnce
- `ev-devlog` battery/auth/email_verification.go ["token_hash", hashedIdentifier(tok)) -> "token_hash", hashedIdentifier(tok), "url", verifyURL)] -> TestEmailVerification_DevModeDoesNotLogLiveURL
- `ev-hop` battery/auth/email_verification.go [if handler.NeedsSameSiteHop(r) { -> if false {] -> TestVerifyLinkFromWebmailHops, TestVerifyLinkWithoutFetchMetaHops
- `ev-limit` battery/auth/email_verification.go [if cfg.RateLimit == nil { -> if false {] -> TestVerifyEmailSendDefaultThrottle
- `ev-memstore` battery/auth/email_verification.go [if err := refuseInMemoryTokenStore(mgr, "email-verificatio -> if err := error(nil); err != nil {] -> TestLinkPluginsRefuseMemoryStore
- `ev-own-session-r` battery/auth/email_verification.go [if userID != sess.UserID { -> if userID != sess.UserID && false {] -> TestVerifyEmailNeedsOwnSession
- `ev-pending-send` battery/auth/email_verification.go [if sess.PendingTwoFactor { -> if false {] -> TestSendVerificationNeedsStepUp
- `ev-xsite` battery/auth/email_verification.go [if rejectCrossSiteForm(w, r) { -> if false {] -> TestSendVerificationRejectsCrossSite
- `lk-guard-r` battery/auth/entity_oauth_links.go ["DELETE FROM %s WHERE user_id = $1 AND provider = $2 AND (EX -> "DELETE FROM %s WHERE user_id = $1 AND provider = $2 AND (1=] -> TestEntityStoreGuardedUnlinkRefusesLastMethod
- `lk-order` battery/auth/entity_oauth_links.go ["WHERE user_id = $1 ORDER BY provider", -> "WHERE user_id = $1 ORDER BY provider DESC",] -> TestEntityOAuthLinks_MultipleProvidersForOneUser
- `lk-owner-immutable` battery/auth/entity_oauth_links.go ["email = EXCLUDED.email, name = EXCLUDED.name, avatar_url = -> "email = EXCLUDED.email, name = EXCLUDED.name, avatar_url =] -> TestEntityOAuthLinks_LinkDoesNotReassignOwner
- `lk-refresh-owner` battery/auth/entity_oauth_links.go ["WHERE provider = $4 AND provider_id = $5 AND user_id = $6", -> "WHERE provider = $4 AND provider_id = $5 AND $6 = $6",] -> TestRefreshOAuthProfileUpdatesOnly
- `es-clear-fresh` battery/auth/entity_store.go ["UPDATE %s SET %s = $1, %s = FALSE WHERE %s = $2", -> "UPDATE %s SET %s = %s, %s = FALSE WHERE %s = $2 AND $1 = $1] -> TestJWTDiesWithCredentialChange
- `es-crud-off-r` battery/auth/entity_store.go [return entity.EntityConfig{Fields: UserEntityFields(), Expo -> _ = crudOff return entity.EntityConfig{Fields: UserEntityF] -> TestVerifyAuthEntitiesPrivate_SilentOnSafeConfig
- `es-sess-digest` battery/auth/entity_store.go [_, err = s.db.ExecContext(ctx, q, sessionID, sha256hex(tok) -> _, err = s.db.ExecContext(ctx, q, sessionID, tok, userID, n] -> TestSessionRedStoredOpaque
- `es-sess-expired-r` battery/auth/entity_store.go [if sess.Expired() { -> if sess.Expired() && false {] -> TestEntitySessionStoreExpiredIsNotFound
- `es-sess-pending-only` battery/auth/entity_store.go ["DELETE FROM %s WHERE user_id = $1 AND pending_two_factor = -> "DELETE FROM %s WHERE user_id = $1"] -> TestEntityStoreDeletesOnlyPending
- `es-sess-sweep` battery/auth/entity_store.go [s.sweepExpiredOnCreate(ctx, now) ->] -> TestEntitySessionStoreCreateSweepsExpiredSessions
- `es-sess-ttl` battery/auth/entity_store.go [if ttl <= 0 { -> if ttl < 0 {] -> TestEntitySessionStore_RejectsNonPositiveTTL
- `es-setpw-unknown-r` battery/auth/entity_store.go [if n == 0 { -> if (n == 0) && false {] -> TestPasswordSettersUnknownUserFailClosed
- `es-softdelete` battery/auth/entity_store.go [if !s.hidesDeleted.Load() { return "" -> if true { return ""] -> TestSoftDeletedUserCannotSignIn
- `es-swap` battery/auth/entity_store.go ["UPDATE %s SET %s = $1, %s = TRUE WHERE %s = $2 AND %s = $3" -> "UPDATE %s SET %s = $1, %s = TRUE WHERE %s = $2 AND (%s = $3] -> TestPasswordChangeLosesToReset
- `es-sweep-retry` battery/auth/entity_store.go [if s.lastSweep.Equal(now) { -> if false {] -> TestEntitySessionSweepFailureRetriesLater
- `es-unique` battery/auth/entity_store.go [if strings.Contains(msg, "UNIQUE constraint failed") { -> if false {] -> TestEntityUserStore_DuplicateEmail_ReturnsErrEmailTaken
- `es-verified-false` battery/auth/entity_store.go [q := fmt.Sprintf("UPDATE %s SET %s = TRUE WHERE %s = $1", -> q := fmt.Sprintf("UPDATE %s SET %s = FALSE WHERE %s = $1",] -> TestNewMagicLinkUserIsVerified
- `ts-corrupt` battery/auth/entity_twofa_store.go [return nil, fmt.Errorf("auth: EntityTwoFAStore: corrupt ba -> return nil, nil] -> TestEntityTwoFA_CorruptCodesRowErrors
- `ts-fresh-version-r` battery/auth/entity_twofa_store.go [return int64(binary.BigEndian.Uint64(b[:])>>2) \| 1, nil -> return int64(binary.BigEndian.Uint64(b[:])>>2)*0 + 1, nil] -> TestTwoFACASRefusesReenrolledRow
- `ts-legacy` battery/auth/entity_twofa_store.go [if isLegacyPlaintextSeed(stored) { return stored, false, -> if false { return stored, false, nil] -> TestTwoFAStoreReadsLegacyPlaintext
- `ts-nil-deletes` battery/auth/entity_twofa_store.go [if state == nil { return s.DeleteTwoFA(ctx, userID) -> if state == nil { return nil] -> TestSetTwoFANilDeletesEnrollment
- `ts-rekey-r` battery/auth/entity_twofa_store.go [if !((r >= 'A' && r <= 'Z') \|\| (r >= '2' && r <= '7')) { -> if (!((r >= 'A' && r <= 'Z') \|\| (r >= '2' && r <= '7'))) &] -> TestTwoFARekeyNotServedAsSeed
- `ts-reseal-r` battery/auth/entity_twofa_store.go [if viaPrevious { -> if (viaPrevious) && false {] -> TestTwoFADrainResealsUnderCurrentKey
- `ts-sealed` battery/auth/entity_twofa_store.go [return s.sealer.seal(secret) -> return secret, nil] -> TestTwoFAStoreSealsSecretAtRest
- `ts-version-col` battery/auth/entity_twofa_store.go [if err := s.ensureBigIntColumn(ctx, "version"); err != nil -> if err := error(nil); err != nil {] -> TestEntityTwoFA_EnsureSchemaAddsVersionToOldTable
- `er-canon` battery/auth/erase.go [Canonicalize: canonicalEmailOrEmpty(mgr.config.Canonicali ->] -> TestEraseReachesEmailKeyedTokensFromLegacyRow
- `er-email-identity` battery/auth/erase.go [if p == purposeMagicLink \|\| p == purposeEmailCode { -> if p == purposeMagicLink {] -> TestEraseReachesEmailKeyedTokensFromLegacyRow
- `er-host-resolver-r` battery/auth/erase.go [(r.Canonicalize == nil \|\| (r.Table == "auth_users" && r.ID -> (r.Canonicalize == nil \|\| true) {] -> TestHostResolverKeepsItsCanonicalizer
- `er-login-limit-r` battery/auth/erase.go [prefix := ratelimit.NamespaceScope(scope) + "\|account:" -> prefix := ratelimit.NamespaceScope("")[:0] + "account:"] -> TestEraseRemovesLoginLimitRows
- `er-memory` battery/auth/erase.go [registerMemoryStoreEraser(mgr) ->] -> TestMemoryStoresReachedByErasure
- `er-sessions-r` battery/auth/erase.go [Name: "auth_sessions", Source: "auth", Table: ss.table, -> Name: "auth_sessions", Source: "auth", Table: "auth_sessi] -> TestEraseCoversConfiguredStoreTables
- `er-tagged` battery/auth/erase.go [ValuePrefix: string(p) + ":", -> ValuePrefix: "",] -> TestEraseReachesTaggedTokens
- `ex-parity-r` battery/auth/export.go [registerPasskeyExporter(ps.table) -> _ = ps] -> TestExportCoversConfiguredStoreTables
- `ex-sessions-r` battery/auth/export.go [registerSessionExporter(ss.table) -> registerSessionExporter("auth_sessions" + ss.table[:0])] -> TestExportCoversConfiguredStoreTables
- `fd-dup-fields-r` battery/auth/form_decode.go [if len(r.PostForm[field]) > 1 { -> if len(r.PostForm[field]) > 1 && false {] -> TestLoginFormDuplicateFieldsRejected
- `fd-fallback` battery/auth/form_decode.go [if fallback != "" && handler.IsSafeRelativePath(fallback) -> if false {] -> TestFormAuthError_FallsBackToConfiguredPath
- `fd-file-part` battery/auth/form_decode.go [if r.MultipartForm != nil && len(r.MultipartForm.File) > 0 -> if false {] -> TestMultipartLoginRefusesDuplicateAndFileFields
- `fd-invisible` battery/auth/form_decode.go [canonical := norm.NFC.String(strings.ToLower(strings.TrimSp -> canonical := norm.NFC.String(strings.ToLower(strings.TrimSp] -> TestInvisibleTwinEmailsOneAccount
- `fd-lower` battery/auth/form_decode.go [canonical := norm.NFC.String(strings.ToLower(strings.TrimSp -> canonical := norm.NFC.String(strings.TrimSpace(stripInvisib] -> TestLoginEmailCaseInsensitive, TestCanonicalEmail
- `fd-maxlen-r` battery/auth/form_decode.go [if len(email) > maxEmailLen { -> if len(email) > maxEmailLen && false {] -> TestOverlongShapedEmailRefused
- `fd-multipart` battery/auth/form_decode.go [if bareContentType(r) != "multipart/form-data" { -> if true {] -> TestMultipartRegisterAndLogin
- `fd-next` battery/auth/form_decode.go [if handler.IsSafeRelativePath(next) { -> if next != "" {] -> TestLoginFormRejectsExternalRedirect, TestSuccessRedirect_RejectsBackslashBypass
- `fd-nfc-r` battery/auth/form_decode.go [canonical := norm.NFC.String(strings.ToLower(strings.TrimSp -> canonical := norm.NFC.String("") + strings.ToLower(strings.] -> TestRegisterFoldsUnicodeTwinEmails
- `fd-nostore` battery/auth/form_decode.go [w.Header().Set("Cache-Control", "no-store, no-cache, must-r ->] -> TestCredentialResponsesSetNoStore
- `fd-override-storable-r` battery/auth/form_decode.go [if !textsafe.Storable(canonical) { -> if !textsafe.Storable(canonical) && false {] -> TestCanonicalizerOverrideOutputMustBeStorable
- `fd-ref-host-r` battery/auth/form_decode.go [out := u.EscapedPath() -> out := ref] -> TestWriteFormAuthError_RejectsCrossOriginReferer
- `fd-ref-query` battery/auth/form_decode.go [out := u.EscapedPath() -> out := u.EscapedPath() + "?" + u.RawQuery] -> TestWriteFormAuthError_StripsRefererQuery
- `fd-register-path` battery/auth/form_decode.go [if on, _ := r.Context().Value(registerFormKey{}).(bool); on -> if false {] -> TestRegisterErrorsLandOnRegisterPath
- `fd-sanitise` battery/auth/form_decode.go [if c == ' ' \|\| c < 0x20 \|\| c > 0x7e { -> if false {] -> TestSanitiseErrMakesURLAndHTMLSafe
- `fd-shape` battery/auth/form_decode.go [if canonical != "" && !addressShaped(canonical) { -> if false {] -> TestCanonicalEmailRefusesNonAddress, TestRegisterRefusesNonAddress
- `fd-storable` battery/auth/form_decode.go [if !textsafe.Storable(email) { return "", i18n.NewError(r -> if false { return "", i18n.NewError(reasonInvalidEmail)] -> TestEmailRefusesUnstorableText
- `jl-cap` battery/auth/json_limit.go [r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes) ->] -> TestHandlerBodyLimit_MagicLinkSend, TestHandlerBodyLimit_TwoFAVerify
- `jl-ct` battery/auth/json_limit.go [if !handler.IsJSONContentType(r.Header.Get("Content-Type")) -> if false {] -> TestLogin_RejectsTextPlainJSON, TestMagicLinkSend_RejectsMissingContentType
- `jl-strict2` battery/auth/json_limit.go [if err := handler.UnmarshalStrict(body, dst); err != nil { -> if err := json.Unmarshal(body, dst); err != nil {] -> TestMagicLinkSendRejectsDuplicateKeys, TestTwoFAChallengeRejectsCaseFoldedKeys
- `jwt-cred-check` battery/auth/jwt.go [if !j.credentialCurrent(ctx, c, u) { -> if false {] -> TestJWTDiesWithCredentialChange
- `jwt-cred-empty` battery/auth/jwt.go [if c.cred == "" { return false -> if c.cred == "" { return true] -> TestJWTWithoutCredentialTagRefused
- `jwt-cred-rotation` battery/auth/jwt.go [for _, secret := range append([]string{j.Secret}, j.Previou -> for _, secret := range []string{j.Secret} {] -> TestJWTCredentialTagSurvivesRotation
- `jwt-empty-sub` battery/auth/jwt.go [if claims.UserID == "" { -> if false {] -> TestJWTRejectsEmptySubject
- `jwt-expired` battery/auth/jwt.go [if now.After(claims.ExpiresAt) { -> if false {] -> TestJWTExpiredTokenRejected, TestAuthBypass_ExpiredJWTRejected
- `jwt-future` battery/auth/jwt.go [if !claims.IssuedAt.IsZero() && claims.IssuedAt.After(now.Ad -> if false {] -> TestJWTRejectsFutureIssuedAt
- `jwt-gen-nouser` battery/auth/jwt.go [h, err := j.storedHash(context.Background(), user) if er -> h, err := j.storedHash(context.Background(), user) if er] -> TestGenerateTokenRefusesUnknownUser
- `jwt-owner-fresh` battery/auth/jwt.go [return u, true } -> return claimsToUser(c), true }] -> TestJWTFailsClosedAfterUserDelete
- `jwt-owner-missing` battery/auth/jwt.go [if err != nil \|\| u == nil { return nil, false -> if err != nil \|\| u == nil { return claimsToUser(c), true] -> TestJWTFailsClosedAfterUserDelete
- `jwt-prev-verify` battery/auth/jwt.go [candidates = append(candidates, j.PreviousSecrets...) ->] -> TestJWT_KeyRotationAcceptsPreviousSecret, TestJWT_KeyRotationMultiplePreviousSecrets
- `jwt-sign-current` battery/auth/jwt.go [return encodeToken(j.Secret, j.Issuer, claims) -> s := j.Secret if len(j.PreviousSecrets) > 0 { s = j.Prev] -> TestJWTSignsOnlyWithCurrentSecret
- `jwt-skew` battery/auth/jwt.go [claims.IssuedAt.After(now.Add(2*time.Minute)) -> claims.IssuedAt.After(now.Add(-time.Minute))] -> TestJWTGenerateAndValidate
- `jwt-stored-other` battery/auth/jwt.go [if row == nil \|\| row.GetID() != user.GetID() { -> if row == nil {] -> TestJWTCredentialLookupOtherUserRefused
- `core-single-mint` battery/auth/magiclink.go [mintSessionCookie(w, cfg, sess.Token, sess.ExpiresAt) -> http.SetCookie(w, &http.Cookie{Name: cfg.SessionCookie, Val] -> TestMagicLinkRedCookieStrict
- `ml-2fa-url` battery/auth/magiclink.go [if _, ok := mgr.plugins[name].(TwoFactorChecker); ok { -> if _, ok := mgr.plugins[name].(TwoFactorChecker); ok && f] -> TestMagicLinkInitRequiresTwoFactorURLWith2FA
- `ml-2fa-url-safe` battery/auth/magiclink.go [if value == "" \|\| handler.IsSafeRelativePath(value) { -> if true {] -> TestTwoFactorURLThatCannotBeRedirectedToIsRefusedAtInit
- `ml-addr` battery/auth/magiclink.go [if !p.mgr.allowMailAddress(r.Context(), body.Email) { req -> if false { requested.Meta] -> TestMagicLinkSendPerAddressLimit
- `ml-addr-digest` battery/auth/magiclink.go [allowed, _, err := rl.Admit(ctx, "address:"+sha256hex(email -> allowed, _, err := rl.Admit(ctx, "address:"+email)] -> TestMailAddressLimitKeyIsADigest
- `ml-baseurl` battery/auth/magiclink.go [if why := invalidLinkBaseURL(p.config.BaseURL); why != "" { -> if why := invalidLinkBaseURL(p.config.BaseURL); false && wh] -> TestMagicLinkInitRefusesBadBaseURL
- `ml-csrf-ctx` battery/auth/magiclink.go [csrfField := CSRFInputFromCtx(r.Context()) -> csrfField := template.HTML("")] -> TestConfirmPageFormWorksBehindCSRF
- `ml-devlog` battery/auth/magiclink.go ["token_hash", hashedIdentifier(token)) -> "token_hash", hashedIdentifier(token), "url", magicLinkUR] -> TestMagicLink_DevModeDoesNotLogLiveURL
- `ml-event-user` battery/auth/magiclink.go [requested.UserID = u.GetID() ->] -> TestMagicLinkRequestedEventCarriesUserID
- `ml-get-no-redeem` battery/auth/magiclink.go [got, err := peeker.PeekToken(r.Context(), token) -> got, err := p.tokenStore.RedeemToken(r.Context(), token)] -> TestMagicLinkVerifyNeedsConfirmation
- `ml-get-purpose` battery/auth/magiclink.go [addr, ok := peekPurposePayload(got, purposeMagicLink) if -> addr, ok := got, true if !ok {] -> TestConfirmPageRefusesOtherFlowToken
- `ml-memstore` battery/auth/magiclink.go [if cfg.DevMode \|\| cfg.AllowInMemoryStores { -> if cfg.DevMode \|\| cfg.AllowInMemoryStores \|\| true {] -> TestLinkPluginsRefuseMemoryStore
- `ml-mint-sweep` battery/auth/magiclink.go [purgeMatching(m.tokens, func(e *magicLinkEntry) bool { ret -> m.lastSweep = now] -> TestMemoryMagicLinkStoreReapsExpiredOnMint
- `ml-nosender` battery/auth/magiclink.go [writeAuthError(w, r, http.StatusServiceUnavailable, reason ->] -> TestMagicLink_NilSenderWithoutDevMode_FailsClosed
- `ml-nostore` battery/auth/magiclink.go [w.Header().Set("Cache-Control", "no-store") w.Header().Set -> w.Header().Set("Content-Type", "application/json") w.Write] -> TestMagicLinkSendIsNotCached
- `ml-peek-expired` battery/auth/magiclink.go [if !ok \|\| !time.Now().Before(entry.expiresAt) { -> if !ok {] -> TestMemoryMagicLinkPeekRefusesExpired
- `ml-pending-target` battery/auth/magiclink.go [target = p.config.TwoFactorURL ->] -> TestMagicLinkPendingSignInGoesToTwoFactorURL
- `ml-reaper-recover` battery/auth/magiclink.go [if rec := recover(); rec != nil { slog.Error("magic-lin -> if rec := recover(); rec != nil { panic(rec) slog.Er] -> TestMagicLinkReaperPanicScrubbed
- `ml-reaper-stop` battery/auth/magiclink.go [close(p.stopCh) p.reaperCancel() ->] -> TestAuthManagerStopsMagicLinkReaper
- `ml-safe-redirect` battery/auth/magiclink.go [if handler.IsSafeRelativePath(u) { return u -> if u != "" { return u] -> TestSafeRedirectRedFullGrammar
- `ml-send-limit` battery/auth/magiclink.go [if config.RateLimit == nil { -> if false {] -> TestMagicLinkSendDefaultThrottle
- `ml-single-use2` battery/auth/magiclink.go [delete(m.tokens, token) if !time.Now().Before(entry.expir -> if !time.Now().Before(entry.expiresAt) {] -> TestMemoryMagicLinkTokenStore_DoubleRedeemFails
- `ml-start-after-stop` battery/auth/magiclink.go [if p.stopped { p.lifecycleMu.Unlock() return errors.New -> if false { p.lifecycleMu.Unlock() return errors.New(] -> TestMagicLinkPluginRefusesStartAfterStop
- `ml-ttl` battery/auth/magiclink.go [c.TokenTTL = 15 * time.Minute -> c.TokenTTL = time.Hour] -> TestMagicLinkPlugin_Defaults
- `ml-verify-cap` battery/auth/magiclink.go [r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes) ->] -> TestMagicLinkVerifyCapsBody
- `ml-verify-limit` battery/auth/magiclink.go [if config.VerifyRateLimit == nil { -> if false {] -> TestMagicLinkVerifyDefaultThrottle
- `ml-verify-xsite` battery/auth/magiclink.go [if rejectCrossSiteForm(w, r) { return } r.Body = http.M -> r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)] -> TestVerifyRejectsCrossSiteTextPlain, TestMagicLinkConfirmRejectsCrossSite
- `mlsql-cleanup` battery/auth/magiclink_sql.go [q := fmt.Sprintf(`DELETE FROM %s WHERE expires_at < $1`, qu -> q := fmt.Sprintf(`DELETE FROM %s WHERE expires_at < $1 AND] -> TestSQLMagicLink_Cleanup
- `mlsql-digest` battery/auth/magiclink_sql.go [if _, err := s.db.ExecContext(ctx, q, sha256hex(token), ema -> if _, err := s.db.ExecContext(ctx, q, token, email, time.No] -> TestMagicLinkRedStoredOpaque
- `mlsql-expired2` battery/auth/magiclink_sql.go [if time.Now().Unix() > exp { -> if time.Now().Unix() > exp && false {] -> TestSQLMagicLink_Expired
- `mlsql-purge` battery/auth/magiclink_sql.go [WHERE email = $1 -> WHERE email = $1 AND 0 = 1] -> TestMagicLinkStoresPurgeByPayload
- `mlsql-single` battery/auth/magiclink_sql.go [`DELETE FROM %s WHERE token = $1 RETURNING email, expires_at -> `SELECT email, expires_at FROM %s WHERE token = $1`] -> TestSQLMagicLink_SingleUse
- `mlsql-sweep` battery/auth/magiclink_sql.go [s.sweepExpiredOnCreate(ctx) return token, nil -> return token, nil] -> TestMagicLinkStoreSweepsExpired
- `mlsql-sweep-fail-r` battery/auth/magiclink_sql.go [slog.Warn("auth: expired magic-link token sweep failed", " -> slog.Warn("x") panic(err)] -> TestSQLMagicLinkCreateTokenSurvivesSweepFailure
- `mlsql-sweep-retry` battery/auth/magiclink_sql.go [s.lastSweep = now.Add(magicLinkSweepRetry - magicLinkSwee ->] -> TestSQLMagicLinkCreateTokenSurvivesSweepFailure
- `mq-deadline` battery/auth/mailqueue.go [ctx, cancel := context.WithTimeout(job.ctx, q.timeout) -> ctx, cancel := context.WithCancel(job.ctx)] -> TestDuplicateNoticeBoundedAndStopped
- `mq-detach2` battery/auth/mailqueue.go [job := mailJob{ctx: context.WithoutCancel(requestCtx), -> job := mailJob{ctx: requestCtx,] -> TestMailQueueDetachesFromRequestCancel
- `mq-nonblock` battery/auth/mailqueue.go [case q.queue <- job: queued = true default: } -> case q.queue <- job: queued = true }] -> TestPasswordResetDeliveryDroppedWhenQueueFull
- `mq-recover` battery/auth/mailqueue.go [if recover() != nil { -> if r := recover(); r != nil { panic(r)] -> TestMailQueueRecoversSenderPanic
- `mq-stop-cancels` battery/auth/mailqueue.go [stopCancel := context.AfterFunc(q.ctx, cancel) -> stopCancel := func() bool { return true }] -> TestDuplicateNoticeBoundedAndStopped
- `mq-stopped` battery/auth/mailqueue.go [if q.stopped { q.mu.Unlock() slog.Warn("email delivery -> if false { q.mu.Unlock() slog.Warn("email delivery stop] -> TestPasswordResetDeliveryRefusedAfterStop
- `acct-limit` battery/auth/manager.go [if c.LoginRateLimitPerAccount == nil { -> if false {] -> TestDefaultThrottlesInstalledInDevModeToo, TestAuthBypass_DefaultBruteForceLimiter
- `dev-cookie` battery/auth/manager.go [c.SessionCookie = "session_id" -> c.SessionCookie = "sid"] -> TestAuthConfig_DevMode_RelaxedDefaults
- `dup-panic` battery/auth/manager.go [if _, exists := m.plugins[plugin.Name()]; exists { -> if false {] -> TestAuthManager_UseDuplicatePanics
- `embed-prefix` battery/auth/manager.go [return []string{m.Config().BasePath} -> return []string{"/auth"}] -> TestAuthReservesItsBasePathFromEmbeds
- `jwt-dev-mint` battery/auth/manager.go [secret, err := randomDevJWTSecret() -> secret, err := "", fmt.Errorf("x")] -> TestAuthManager_DevModeMintsJWT
- `jwt-in-new` battery/auth/manager.go [if config.JWTSecret != "" { -> if false {] -> TestRequireAuthWiredBeforeInit
- `jwt-prod` battery/auth/manager.go [if !m.config.DevMode && m.config.JWTSecret == "" { -> if false {] -> TestInit_ProdEmptySecretFailsClosed
- `jwt-store-post` battery/auth/manager.go [m.jwtAuth.SetUserStore(m.userStore) } -> }] -> TestJWTResolvesAgainstStoreAPluginInstalled
- `login-limit` battery/auth/manager.go [MaxAttempts: 30, -> MaxAttempts: 31,] -> TestDefaultThrottlesInstalledInDevModeToo
- `mail-limit` battery/auth/manager.go [MaxAttempts: 1, -> MaxAttempts: 5,] -> TestDefaultThrottlesInstalledInDevModeToo
- `mem-optin` battery/auth/manager.go [if m.config.DevMode \|\| m.config.AllowInMemoryStores { -> if m.config.DevMode {] -> TestNoMemoryStoreWarnWhenOptedIn
- `mem-sess-post` battery/auth/manager.go [if err := m.refuseInMemorySessionStore(); err != nil { -> if err := m.refuseInMemorySessionStore(); err != nil && fals] -> TestProdRefusesMemorySessionStoreInstalledByPlugin
- `mw-order` battery/auth/manager.go [for _, v := range slices.Backward(m.order) { -> for _, v := range m.order {] -> TestPluginMiddlewareFirstRegisteredOutermostAndStopReversed
- `neg-jwt` battery/auth/manager.go [if m.config.JWTExpiry < 0 { -> if false {] -> TestNegativeJWTExpiryRefused
- `neg-ttl` battery/auth/manager.go [if m.config.SessionTTL < 0 { -> if false {] -> TestNegativeSessionTTLRefused
- `noted` battery/auth/manager.go [if app != nil && ok { -> if app != nil && (ok \|\| true) {] -> TestAuthNotedOnlyWithMiddleware
- `noted-r` battery/auth/manager.go [if app != nil && ok { -> if app != nil && (ok \|\| true) {] -> TestAuthNotedOnlyWithMiddleware
- `onstop-eraser` battery/auth/manager.go [unregisterMemoryStoreEraser(m) ->] -> TestMemoryStoresReachedByErasure
- `prod-cookie` battery/auth/manager.go [c.SessionCookie = "__Host-session" -> c.SessionCookie = "session"] -> TestAuthConfig_DefaultsAreSecure
- `prod-secure` battery/auth/manager.go [c.SessionSecure = true -> c.SessionSecure = false] -> TestAuthConfig_DefaultsAreSecure
- `reg-limit` battery/auth/manager.go [if c.RegisterRateLimit == nil { -> if false {] -> TestDefaultThrottlesInstalledInDevModeToo, TestRegisterRateLimitedByDefault
- `roles-audit` battery/auth/manager.go [Kind: "roles.updated", -> Kind: "roles.x",] -> TestSetUserRolesAuditsMutation
- `roles-copy` battery/auth/manager.go [copy(out, m.config.DefaultRoles) return out -> _ = out return m.config.DefaultRoles] -> TestDefaultRolesReturnsACopy
- `roles-default` battery/auth/manager.go [return []string{"user"} -> return []string{"admin"}] -> TestDefaultRolesReturnsACopy
- `roles-default2` battery/auth/manager.go [return []string{"user"} -> return []string{"admin"}] -> TestDefaultRolesReturnsACopy
- `roles-storable` battery/auth/manager.go [if !textsafe.Storable(userID) { -> if !textsafe.Storable(userID) && false {] -> TestSetUserRolesRefusesUnstorableID
- `stop-firsterr` battery/auth/manager.go [err != nil && firstErr == nil { -> err != nil {] -> TestPluginMiddlewareFirstRegisteredOutermostAndStopReversed
- `stop-order` battery/auth/manager.go [for _, name := range slices.Backward(m.order) { -> for _, name := range m.order {] -> TestPluginMiddlewareFirstRegisteredOutermostAndStopReversed
- `ttl-default` battery/auth/manager.go [c.SessionTTL = 7 * 24 * time.Hour -> c.SessionTTL = 24 * time.Hour] -> TestZeroSessionTTLDefaultsToSevenDays
- `mg-decider` battery/auth/mcp_gate.go [if roleGateDenied(ctx, user) { -> if false {] -> TestMCPGatesHonorDecider
- `mg-embed` battery/auth/mcp_gate.go [if _, embedded := embed.GrantFromContext(ctx); embedded { -> if _, embedded := embed.GrantFromContext(ctx); embedded &&] -> TestEmbedGrantCannotPassMCPGates
- `mg-role` battery/auth/mcp_gate.go [if !hasAnyRole(ctx, user, roles) { -> if false {] -> TestMCPRoleRefusesWrongRole
- `bearer-scheme` battery/auth/middleware.go [if len(parts) != 2 \|\| !strings.EqualFold(parts[0], "bearer") -> if len(parts) != 2 {] -> TestExtractBearerTokenNeedsBearerScheme
- `ra-embed-pass` battery/auth/middleware.go [if _, embedded := embed.GrantFromContext(r.Context()); embed -> if _, embedded := embed.GrantFromContext(r.Context()); false] -> TestRequireAuthAcceptsAVerifiedGrant, TestInProcessRequireAuth
- `ra-recheck` battery/auth/middleware.go [ctx = handler.WithPrincipalCheck(ctx, jwtRecheck(jwt, tok ->] -> TestEventStreamClosesOnRevocation
- `ra-resolve` battery/auth/middleware.go [user, ok := jwt.resolveOwner(r.Context(), claims) if ! -> user, ok := claimsToUser(claims), true if !ok {] -> TestJWTFailsClosedAfterUserDelete
- `rr-decider` battery/auth/middleware.go [if roleGateDenied(r.Context(), user) { -> if false {] -> TestRequireRoleHonorsDecider
- `rr-embed` battery/auth/middleware.go [if _, embedded := embed.GrantFromContext(r.Context()); em -> if _, embedded := embed.GrantFromContext(r.Context()); em] -> TestRequireRoleRefusesAnEmbedGrant
- `rr-scope` battery/auth/middleware.go [if slices.Contains(userRoles, target) && access.ScopeAllows( -> if slices.Contains(userRoles, target) {] -> TestRoleGateTokenNeedsRoleScope
- `mint-checker-err` battery/auth/mint.go [return false, fmt.Errorf("two-factor state lookup (%s): % -> continue] -> TestTwoFAFailClosed_CheckerError
- `mint-mark-err` battery/auth/mint.go [return false, fmt.Errorf("mark pending two-factor: %w", e -> return false, nil] -> TestMintSessionRollbackOn2FAFailure
- `mint-no-marker` battery/auth/mint.go [return false, fmt.Errorf("session store %T cannot mark a -> return false, nil] -> TestTwoFAFailClosed_NoPendingMarker
- `mint-rollback` battery/auth/mint.go [_ = m.SessionStore().Delete(ctx, sess.Token) ->] -> TestMintSessionRollbackOn2FAFailure
- `cleanname-r` battery/auth/name.go [\|\| textsafe.ContainsUnsafe(name) { -> \|\| (textsafe.ContainsUnsafe(name) && false) {] -> TestCleanNameRefusesUnsafeNames
- `oa-2fa-url` battery/auth/oauth2.go [if _, ok := mgr.plugins[name].(TwoFactorChecker); ok { -> if _, ok := mgr.plugins[name].(TwoFactorChecker); ok && f] -> TestOAuthInitRequiresTwoFactorURLWith2FA
- `oa-bound-browser2` battery/auth/oauth2.go [return subtle.ConstantTimeCompare([]byte(c.Value), []byte(s -> return len(c.Value) > 0] -> TestOAuthCallbackRefusesAnotherFlowsCookie
- `oa-bound-first` battery/auth/oauth2.go [if !p.requireBoundBrowser(w, r, stateParam) { -> if false {] -> TestOAuthCallbackNeedsBrowserBinding
- `oa-email-limit` battery/auth/oauth2.go [if !emailWithinLimit(w, r, info.Email) { -> if false {] -> TestOAuthOverlongEmailRefused
- `oa-evict` battery/auth/oauth2.go [p.evictEarliestNoncesLocked(nonceEvictBatch) -> return "", false] -> TestOAuthNonceFloodDoesNotLockOutCallbacks
- `oa-gh-refusal` battery/auth/oauth2.go [func githubTokenRefusal(what, code, accessToken string) erro -> func githubTokenRefusal(what, code, accessToken string) erro] -> TestGitHubRefusedExchangeIsAnError
- `oa-gh-verified` battery/auth/oauth2.go [} else if email != "" { emailVerified = false } -> } else if email != "" { emailVerified = true }] -> TestGitHubPublicEmailUnconfirmedIsUnverified
- `oa-hardcap` battery/auth/oauth2.go [if len(p.usedNonces) >= nonceMaxEntries { -> if false {] -> TestOAuth2NonceMapHasHardCap
- `oa-hop` battery/auth/oauth2.go [if handler.NeedsSameSiteHop(r) { -> if false {] -> TestOAuthCrossSiteCallbackHopsToLanding
- `oa-key-shape` battery/auth/oauth2.go [if !validProviderKey(key) { -> if false {] -> TestOAuthProviderKeyShapeIsValidated
- `oa-link-hop` battery/auth/oauth2.go [if p.sameSiteHop(w, r, stateParam, providerName) { -> if false {] -> TestOAuthLinkSurvivesStrictCookie
- `oa-link-race-r` battery/auth/oauth2.go [if bound.GetID() != linkUserID { -> if (bound.GetID() != linkUserID) && false {] -> TestLinkFlowLosingBindingRaceIsRefused
- `oa-link-recheck-r` battery/auth/oauth2.go [if !p.linkSessionHolds(r, linkUserID) \|\| snap.moved(r.Con -> if (!p.linkSessionHolds(r, linkUserID) \|\| snap.moved(r.Co] -> TestLinkCallbackOvertakenByClaim
- `oa-link-session-r` battery/auth/oauth2.go [if currentID != linkUserID { -> if (currentID != linkUserID) && false {] -> TestOAuth_AuthenticatedLinkRefusesSessionMismatch
- `oa-link-steal` battery/auth/oauth2.go [owner, ferr := linker.FindByOAuth(r.Context(), info.Provi -> owner, ferr := User(nil), ErrUserNotFound] -> TestOAuthLinkRefusesBoundIdentity
- `oa-link-tokens` battery/auth/oauth2.go [p.saveProviderTokens(r.Context(), linkUserID, providerNam ->] -> TestLinkFlowStoresProviderTokens
- `oa-linker-required-r` battery/auth/oauth2.go [if !cfg.DevMode && !cfg.AllowInMemoryStores { -> if (!cfg.DevMode && !cfg.AllowInMemoryStores) && false {] -> TestOAuth2Plugin_Init_FailsClosedWithoutLinker
- `oa-login-recheck` battery/auth/oauth2.go [if !p.linkStillNames(r.Context(), userStore, info, user.Ge -> if false {] -> TestOAuthLoginOvertakenByClaim
- `oa-mint-2fa-r` battery/auth/oauth2.go [target = safeRedirectURL(p.twoFactorURL) ->] -> TestOAuthPendingSignInGoesToTwoFactorURL
- `oa-nocode-nonce` battery/auth/oauth2.go [code := r.URL.Query().Get("code") if code == "" { -> p.validateAndConsumeState(stateParam, providerName) code] -> TestOAuthCallbackWithoutCodeRecordsNoNonce
- `oa-norule-redirect` battery/auth/oauth2.go [var defaultOAuthHTTPClient = oidcNoRedirect(&http.Client{Tim -> var defaultOAuthHTTPClient = &http.Client{Timeout: 10 * time] -> TestProviderFetchRefusesRedirect
- `oa-pkce-exchange` battery/auth/oauth2.go [return pp.ExchangeCodeWithVerifier(ctx, code, p.pkceVerifi -> return pp.ExchangeCodeWithVerifier(ctx, code, p.pkceVerifi] -> TestOAuthStolenCodeDoesNotRedeemInAnotherFlow
- `oa-pkce-url2` battery/auth/oauth2.go [return pp.AuthURLWithChallenge(state, pkceChallenge(p.pkce -> _ = pp return provider.AuthURL(state)] -> TestOAuthRedirectSendsPKCEChallenge
- `oa-r-haspw-err` battery/auth/oauth2.go [return nil, false, fmt.Errorf("%w: HasPassword: %v", er -> hp = false] -> TestResolveOAuth_HasPasswordError_FailsClosed
- `oa-r-noemail` battery/auth/oauth2.go [if info.Email == "" { return nil, false, errOAuthNoEmail -> if false { return nil, false, errOAuthNoEmail] -> TestOAuthEmptyEmailNeverMergesAccounts
- `oa-r-nolinker` battery/auth/oauth2.go [return nil, false, errOAuthNoLinker -> u, _, e := store.FindByEmail(ctx, info.Email) return u,] -> TestResolveOAuth_NoLinker_FailsClosed
- `oa-r-proven-r` battery/auth/oauth2.go [if !proven { -> if (!proven) && false {] -> TestOAuthUnverifiedPrecreateNoMerge
- `oa-r-refresh-update-only` battery/auth/oauth2.go [if info.EmailVerified && info.Email != "" && found.GetEmai -> if info.EmailVerified {] -> TestVerifiedLoginMarksAccount
- `oa-r-verified` battery/auth/oauth2.go [if info.EmailVerified { hasPw := true -> if true { hasPw := true] -> TestResolveOAuth_UnverifiedEmail_DoesNotMatchPasswordAccount
- `oa-registry-key` battery/auth/oauth2.go [info.Provider = providerName ->] -> TestOAuthLinksKeyedToRegistryName
- `oa-replay` battery/auth/oauth2.go [if _, replayed := p.usedNonces[nonceB64]; replayed { -> if false {] -> TestOAuth2Plugin_StateReplayRejected
- `oa-samorigin` battery/auth/oauth2.go [if !strings.HasPrefix(path, "/") \|\| strings.HasPrefix(path, -> if !strings.HasPrefix(path, "/") {] -> TestCrossSiteArrivalHopIsNoOpenRedirect
- `oa-state-cookie-host` battery/auth/oauth2.go [return oauthStateCookie -> return oauthStateCookieDev] -> TestOAuthStateCookieHostPrefixed
- `oa-state-expiry2` battery/auth/oauth2.go [if time.Now().After(expiry) { return "", "", time.Time{}, -> if time.Now().After(expiry) && false { return "", "", tim] -> TestExpiredStateRefused
- `oa-state-hmac-r` battery/auth/oauth2.go [if !hmac.Equal([]byte(sig), []byte(expectedSig)) { -> if (!hmac.Equal([]byte(sig), []byte(expectedSig))) && false] -> TestOAuth2Plugin_StateTampered
- `oa-state-provider` battery/auth/oauth2.go [if provider != expectedProvider { -> if false {] -> TestOAuth2Plugin_StateWrongProvider
- `oa-state-secret-len` battery/auth/oauth2.go [if len(cfg.StateSecret) < minStateSecretLen { -> if false {] -> TestStateSecretTooShortRefused
- `oa-sweep-rate` battery/auth/oauth2.go [&& now.Sub(p.lastNonceSweep) >= nonceGCSweepInterval { -> {] -> TestOAuth2NonceSweepIsRateLimitedAndReclaimsExpired
- `oa-timeout` battery/auth/oauth2.go [var defaultOAuthHTTPClient = oidcNoRedirect(&http.Client{Tim -> var defaultOAuthHTTPClient = oidcNoRedirect(&http.Client{})] -> TestOAuthProviders_DefaultClientHasTimeout
- `oa-token-expiry` battery/auth/oauth2.go [func tokenExpiry(expiresIn int64) time.Time { -> func tokenExpiry(expiresIn int64) time.Time { if expiresIn] -> TestNonExpiringProviderTokenIsUsable
- `ots-cas` battery/auth/oauth_token_store.go [WHERE user_id = $1 AND provider = $2 AND refresh_sha = $7 -> WHERE user_id = $1 AND provider = $2 AND ($7 = $7)`,] -> TestRefreshDoesNotClobberNewerToken
- `ots-erase-r` battery/auth/oauth_token_store.go [Column: "user_id", Mode: datexport.EraseDelete, -> Column: "provider", Mode: datexport.EraseDelete,] -> TestEraseRemovesOAuthTokens
- `ots-fail-newer` battery/auth/oauth_token_store.go [if cur, gerr := store.Get(ctx, userID, key); gerr == nil & -> if false { return OAuthTokenRecord{}, nil] -> TestRefreshFailureReturnsNewerRecord
- `ots-keep-refresh` battery/auth/oauth_token_store.go [if tok.RefreshToken != "" { rec.RefreshToken = tok.Refres -> rec.RefreshToken = tok.RefreshToken] -> TestRefresh_KeepsRefreshTokenWhenOmitted
- `ots-key-required` battery/auth/oauth_token_store.go [if len(key) == 0 { return nil, fmt.Errorf("auth: at-rest -> if false { return nil, fmt.Errorf("auth: at-rest sealing] -> TestOAuthStore_RequiresKey
- `ots-lease-cas` battery/auth/oauth_token_store.go [AND refresh_sha = $3 AND refresh_lease_ms < $5`, -> AND refresh_sha = $3 AND ($5 > 0 OR refresh_lease_ms < $5)`,] -> TestRefreshWaitsForAnotherReplicasLease
- `ots-lease-wait` battery/auth/oauth_token_store.go [return awaitOtherRefresh(ctx, store, userID, key, rec) -> _ = got] -> TestRefreshWaitsForAnotherReplicasLease
- `ots-movedon` battery/auth/oauth_token_store.go [if tokenMovedOn(seen, rec) { -> if false {] -> TestConcurrentRefreshSpendsRefreshTokenOnce
- `ots-noaccess` battery/auth/oauth_token_store.go [if err == nil && (tok == nil \|\| tok.AccessToken == "") { -> if err == nil && tok == nil {] -> TestRefreshWithoutAnAccessTokenStoresNothing
- `ots-open-auth` battery/auth/oauth_token_store.go [if !ok { return OAuthTokenRecord{}, errors.New("auth: oau -> if false { return OAuthTokenRecord{}, errors.New("auth: o] -> TestOAuthTokenWrongKeyFailsToOpen
- `ots-registry-key` battery/auth/oauth_token_store.go [return validOAuthToken(ctx, p.tokenStore, provider, provide -> return validOAuthToken(ctx, p.tokenStore, provider, provide] -> TestPluginTokenMethodsUseRegistryKey
- `ots-sealed` battery/auth/oauth_token_store.go [return base64.RawStdEncoding.EncodeToString(ct), nil -> _ = ct return plaintext, nil] -> TestOAuthTokenStore_StoredOpaque
- `ots-settle-detached` battery/auth/oauth_token_store.go [settle, cancel := undoContext(ctx) defer cancel() saved, -> settle, cancel := ctx, func() {} defer cancel() saved, er] -> TestRefreshStoresRotatedGrantWhenCallerHangsUp
- `ots-skew` battery/auth/oauth_token_store.go [return rec.AccessToken != "" && (rec.Expiry.IsZero() \|\| tim -> return rec.AccessToken != "" && (rec.Expiry.IsZero() \|\| tim] -> TestValidToken_RefreshesNearExpiry
- `ots-zero-expiry` battery/auth/oauth_token_store.go [return rec.AccessToken != "" && (rec.Expiry.IsZero() \|\| tim -> return rec.AccessToken != "" && time.Until(rec.Expiry) > re] -> TestNonExpiringProviderTokenIsUsable
- `oi-aud-r` battery/auth/oidc.go [if !found { return errors.New("oidc: id_token aud does no -> if (!found) && false { return errors.New("oidc: id_token] -> TestOIDCSec_AudMissingClient
- `oi-auth-method` battery/auth/oidc.go [if slices.Contains(d.TokenAuthMethods, OIDCAuthClientSecret -> if false {] -> TestOIDCTokenAuthMethod
- `oi-azp` battery/auth/oidc.go [if azp != p.cfg.ClientID { -> if false {] -> TestOIDCSec_SingleAudBadAzp
- `oi-disc-issuer` battery/auth/oidc.go [if normalizeIssuer(d.Issuer) != normalizeIssuer(p.cfg.Issue -> if false {] -> TestOIDCSec_DiscoveryIssuerMismatch
- `oi-endpoint-https` battery/auth/oidc.go [if plainHTTPIssuer && isLocalhostHost(u.Hostname()) { -> if true {] -> TestOIDCSec_EndpointDowngradeRefused, TestValidateOIDCEndpoint
- `oi-endpoint-userinfo` battery/auth/oidc.go [if u.User != nil { -> if false {] -> TestValidateOIDCEndpoint
- `oi-exp-r` battery/auth/oidc.go [if now >= exp+leeway { -> if (now >= exp+leeway) && false {] -> TestOIDCSec_Expired
- `oi-flag-covers` battery/auth/oidc.go [return email != "" && claimString(src, "email") == email -> return true] -> TestOIDCRemappedEmailClaimDoesNotInheritVerified, TestOIDCUserinfoRemappedEmailClaimDoesNotInheritVerified
- `oi-https` battery/auth/oidc.go [if !isLocalhostHost(u.Hostname()) { -> if false {] -> TestOIDCHttpIssuerOnlyLiteralLoopback
- `oi-iat-r` battery/auth/oidc.go [if iat > now+leeway { -> if (iat > now+leeway) && false {] -> TestOIDCSec_FutureIat
- `oi-iss` battery/auth/oidc.go [if normalizeIssuer(claimString(claims, "iss")) != normalize -> if false {] -> TestOIDCSec_WrongIssuer
- `oi-multiaud` battery/auth/oidc.go [} else if len(auds) > 1 { -> } else if false {] -> TestOIDCSec_AudArrayNoAzp
- `oi-nbf-r` battery/auth/oidc.go [if now+leeway < nbf { -> if (now+leeway < nbf) && false {] -> TestOIDCSec_FutureNbf
- `oi-secret-required` battery/auth/oidc.go [if cfg.ClientSecret == "" { -> if false {] -> TestOIDC_NewProviderValidation
- `oi-ui-sub` battery/auth/oidc.go [if tokenSub != "" && tokenSub != uiSub { -> if tokenSub != "" && uiSub != "" && tokenSub != uiSub {] -> TestOIDCSec_UserinfoMissingSub
- `oi-ui-sub-mismatch-r` battery/auth/oidc.go [if tokenSub != "" && tokenSub != uiSub { -> if (tokenSub != "" && tokenSub != uiSub) && false {] -> TestOIDCSec_UserinfoSubMismatch
- `oi-ui-verified2` battery/auth/oidc.go [if !verifiedPresent && claimString(ui, p.cfg.Claims.Emai -> if true {] -> TestOIDCVerifiedFlagNeedsSameEmail
- `jw-alg` battery/auth/oidc_jwks.go [if k.Alg != "" && k.Alg != "RS256" { -> if false {] -> TestOIDCSec_JWKAlgMismatchRejected
- `jw-algallow2` battery/auth/oidc_jwks.go [if hdr.Alg != "RS256" && hdr.Alg != "ES256" { -> if hdr.Alg == "none" {] -> TestOIDCSec_AlgHS256, TestOIDCSec_AlgLowercase
- `jw-backoff` battery/auth/oidc_jwks.go [const jwksRetryBackoff = time.Minute -> const jwksRetryBackoff = 0] -> TestJWKSRefreshFailureBacksOff
- `jw-big` battery/auth/oidc_jwks.go [if n.BitLen() > maxCOSERSABits { -> if false {] -> TestOIDCSec_OversizedRSAModulus
- `jw-curve` battery/auth/oidc_jwks.go [if !curve.IsOnCurve(x, y) { -> if false {] -> TestOIDCEcPointOffCurveRejected
- `jw-exp` battery/auth/oidc_jwks.go [if e.Int64() < 3 \|\| e.Bit(0) == 0 { -> if false {] -> TestOIDCSec_DegenerateRSAExponent
- `jw-kty` battery/auth/oidc_jwks.go [if hdr.Alg == "RS256" && key.rsa == nil { -> if false {] -> TestOIDCSec_ECKeyForRS256
- `jw-noKid` battery/auth/oidc_jwks.go [if len(s.list) == 1 { -> if len(s.list) >= 1 {] -> TestOIDCNoKidAmbiguousKeyset
- `jw-use` battery/auth/oidc_jwks.go [if k.Use != "" && k.Use != "sig" { -> if false {] -> TestOIDCSec_EncUseKeyRejected
- `jw-weak2` battery/auth/oidc_jwks.go [if n.BitLen() < 2048 { -> if n.BitLen() < 2048 && false {] -> TestOIDCWeakRSAJWKSkipped
- `owner-ex` battery/auth/owner.go [if id == "" { -> if false {] -> TestOwnerExtractorReadsCurrentUser
- `pg-action-clean` battery/auth/pages.go [action := urlsafe.CleanAnchor(d.Action) -> action := d.Action] -> TestDefaultConfirmPageRefusesUnsafeAction
- `pg-nostore` battery/auth/pages.go [w.Header().Set("Cache-Control", "no-store") w.Header().Set ->] -> TestConfirmPageIsNotCachedOrReferred
- `pg-render` battery/auth/pages.go [if pages != nil { pages.MagicLinkConfirm(w, r, d) -> if false { pages.MagicLinkConfirm(w, r, d)] -> TestPagesRenderMagicLinkConfirm
- `pk-2fa-down` battery/auth/passkey.go [if errors.Is(err, ErrTwoFactorEnforcement) { -> if false {] -> TestPasskeyTwoFADownFailsClosed
- `pk-2fa-url` battery/auth/passkey.go [if _, ok := mgr.plugins[name].(TwoFactorChecker); ok { -> if _, ok := mgr.plugins[name].(TwoFactorChecker); ok && f] -> TestPasskeyTwoFactorNeedsURL
- `pk-be` battery/auth/passkey.go [if res.backupEligible != cred.BackupEligible { -> if false {] -> TestPasskeyBackupEligibleIsFixed
- `pk-cap-insert` battery/auth/passkey.go [if err := p.store.AddPasskey(r.Context(), cred, p.config.Ma -> if err := p.store.AddPasskey(r.Context(), cred, 1<<30); err] -> TestPasskeyCapHoldsAtRegister
- `pk-cap-options` battery/auth/passkey.go [if len(existing) >= p.config.MaxPerUser { -> if false {] -> TestPasskeyPerUserCap
- `pk-cas-r` battery/auth/passkey.go [if !wrote { -> if !wrote && false {] -> TestPasskeyLostRaceIs409
- `pk-count` battery/auth/passkey.go [if (res.signCount != 0 \|\| cred.SignCount != 0) && res.signC -> if false {] -> TestPasskeySignCountMustAdvance
- `pk-count-zero` battery/auth/passkey.go [if (res.signCount != 0 \|\| cred.SignCount != 0) && res.signC -> if res.signCount <= cred.SignCount {] -> TestPasskeyZeroCounterSignsIn
- `pk-delete-own-r` battery/auth/passkey.go [if !deleted { -> if (!deleted) && false {] -> TestPasskeyDeleteOnlyOwn
- `pk-full-pending2` battery/auth/passkey.go [if sess.PendingTwoFactor { -> if sess.PendingTwoFactor && false {] -> TestPasskeyRegisterNeedsSecondFactor
- `pk-full-verified` battery/auth/passkey.go [if !sess.TwoFactorVerified { -> if false {] -> TestPasskeyRegisterNeedsSecondFactor
- `pk-handle` battery/auth/passkey.go [if string(f["userHandle"]) != cred.UserID { -> if false {] -> TestPasskeyUserHandleMustMatch
- `pk-handle-len` battery/auth/passkey.go [if len(sess.UserID) > maxUserHandleLen { -> if false {] -> TestPasskeyLongUserIDRefused
- `pk-json-r` battery/auth/passkey.go [if !handler.IsJSONContentType(r.Header.Get("Content-Type")) -> if (!handler.IsJSONContentType(r.Header.Get("Content-Type")] -> TestPasskeyOptionsNeedJSON
- `pk-memstore-r` battery/auth/passkey.go [if !memoryOK { -> if (!memoryOK) && false {] -> TestPasskeyProdRefusesMemoryStores
- `pk-name-r` battery/auth/passkey.go [if unicode.IsControl(c) \|\| unicode.Is(unicode.Cf, c) { -> if (unicode.IsControl(c) \|\| unicode.Is(unicode.Cf, c)) &&] -> TestPasskeyNameRefusesBadInput
- `pk-rate` battery/auth/passkey.go [if !p.limit.guard(w, r) \|\| !requireJSON(w, r) { -> if !requireJSON(w, r) {] -> TestPasskeyRoutesAreRateLimited
- `pk-reg-fence-r` battery/auth/passkey.go [if _, err := p.mgr.SessionStore().Get(r.Context(), sess.Tok -> if _, err := p.mgr.SessionStore().Get(r.Context(), sess.Tok] -> TestPasskeyRegisterOvertakenByReset
- `pk-reg-id` battery/auth/passkey.go [if string(res.credID) != string(f["id"]) { -> if false {] -> TestPasskeyRegisterIDMustMatch
- `pk-reg-owner-r2` battery/auth/passkey.go [if err != nil \|\| owner != sess.UserID { -> if err != nil \|\| (owner != sess.UserID && false) {] -> TestPasskeyRegisterOthersChallenge
- `pk-reset-recheck` battery/auth/passkey.go [if _, err := p.store.GetPasskey(r.Context(), cred.ID); err -> if false {] -> TestPasskeyLoginOvertakenByReset
- `pw-argon-ct-r` battery/auth/password.go [return subtle.ConstantTimeCompare(key, p.key) == 1 -> return subtle.ConstantTimeCompare(key, key) == 1] -> TestArgon2_HashAndVerify
- `pw-argon-detect` battery/auth/password.go [if strings.HasPrefix(hash, "$argon2id$") { -> if false {] -> TestCheckPassword_AutoDetectsArgon2
- `pw-argon-hash-cap` battery/auth/password.go [if memory > maxArgon2MemoryKiB \|\| time > maxArgon2Time \|\| th -> if false {] -> TestArgon2HashRefusesParamsVerifyRefuses
- `pw-argon-salt` battery/auth/password.go [if _, err := rand.Read(salt); err != nil { return "", fmt ->] -> TestArgon2_DifferentSaltPerHash
- `pw-argon-verify-cap2` battery/auth/password.go [if p.memory > maxArgon2MemoryKiB \|\| p.time > maxArgon2Time -> if (p.memory > maxArgon2MemoryKiB \|\| p.time > maxArgon2Time] -> TestArgon2PHCRefusesOversizedParams
- `pw-custom-first` battery/auth/password.go [if isCustomHasher(DefaultHasher) && DefaultHasher.Verify(pas -> if false {] -> TestVerifyPasswordConsultsCustomDefaultHasher
- `pw-custom-key` battery/auth/password.go [return fmt.Sprintf("%T", h) } -> return fmt.Sprintf("%T\|%+v", h, h) }] -> TestCustomHasherDummyCachedOnce
- `pw-dummy-cache` battery/auth/password.go [dummyHashes.Store(key, hash) ->] -> TestCustomHasherDummyCachedOnce
- `pw-dummy-hasher` battery/auth/password.go [hash, err := h.Hash("dummy-password-for-timing") -> hash, err := "", fmt.Errorf("x")] -> TestLoginDummyHashTracksDefaultHasher
- `pw-empty2` battery/auth/password.go [func HashPassword(password string) (string, error) { if pas -> func HashPassword(password string) (string, error) { if pas] -> TestPassword_EmptyPasswordRejected
- `pw-floor2` battery/auth/password.go [const RecommendedMinPasswordBytes = 8 -> const RecommendedMinPasswordBytes = 9] -> TestPasswordFloorCountsCharacters
- `pw-norecurse2` battery/auth/password.go [return BcryptHasher{}.Verify(password, hash) } -> return verifyPassword(password, hash) && BcryptHasher{}.Ver] -> TestCustomVerifyCallingCheckPasswordDoesNotRecurse
- `pw-placeholder-fresh` battery/auth/password.go [return newPlaceholderHashWith(h) } -> return placeholderPasswordHash(), nil }] -> TestJWTDiesWithCredentialChange
- `pw-placeholder-hasher` battery/auth/password.go [hash, err := newPlaceholderHashWith(h) if err != nil { r -> hash, err := "", fmt.Errorf("x") if err != nil { return] -> TestPasswordlessPlaceholderTracksDefaultHasher
- `pw-prehash` battery/auth/password.go [if len(input) > 72 { sum := sha256.Sum256(input) input -> if len(input) > 72 { input = input[:72] } b, err] -> TestPassword_VeryLongPasswordHandled
- `pw-runes-r` battery/auth/password.go [if utf8.RuneCountInString(password) < RecommendedMinPasswor -> if utf8.RuneCountInString(password) < 0 \|\| len(password) <] -> TestPasswordFloorCountsCharacters
- `pc-2fa-err` battery/auth/password_change.go [if err != nil { return false, err } if enabled { -> if err != nil { return false, nil } if enabled {] -> TestPasswordChangeTwoFALookupErrorRefuses
- `pc-acct-limit-r2` battery/auth/password_change.go [if allowed, retry := c.loginLimitAccount.AllowContext(ctx -> if allowed, retry := c.loginLimitAccount.AllowContext(ctx] -> TestPasswordChangeRateLimited
- `pc-confirm` battery/auth/password_change.go [case body.Confirm != nil && *body.Confirm != body.Password -> case false:] -> TestPasswordChangeConfirmMustMatch
- `pc-current` battery/auth/password_change.go [if err != nil \|\| !verifyPassword(body.Current, hash) { -> if err != nil {] -> TestPasswordChangeWrongCurrentRefused
- `pc-drop-ml` battery/auth/password_change.go [if err := c.mgr.dropMagicLinks(ctx, sess.UserID, "password -> if err := error(nil); err != nil {] -> TestChangeDropsMagicLinks
- `pc-drop-reset` battery/auth/password_change.go [reset.dropLinks(ctx, sess.UserID, "password_change", rem -> _ = reset] -> TestPasswordChangeDropsOutstandingResetLinks
- `pc-embed2` battery/auth/password_change.go [if _, embedded := embed.GrantFromContext(ctx); embedded { -> if _, embedded := embed.GrantFromContext(ctx); embedded &&] -> TestPasswordChangeRefusesTokenAndEmbedCallers
- `pc-pending` battery/auth/password_change.go [if sess.PendingTwoFactor { -> if false {] -> TestPasswordChangePendingTwoFARefused
- `pc-purge-err` battery/auth/password_change.go [slog.Warn("session revocation failed", "reason", reason, " -> return nil] -> TestPasswordChangeFailsWhenRevokeFails, TestResetFailsWhenRevokeFails
- `pc-remint-recheck` battery/auth/password_change.go [if !credentialUnchanged(ctx, c.mgr.UserStore(), email, old. -> if false {] -> TestChangeRemintOvertakenByReset
- `pc-revoke` battery/auth/password_change.go [if err := c.mgr.revokeUserSessions(ctx, sess.UserID, "pass -> if err := error(nil); err != nil {] -> TestPasswordChangeRevokesOthers
- `pc-revoke-fail` battery/auth/password_change.go [writeAuthError(w, r, http.StatusInternalServerError, reas ->] -> TestPasswordChangeFailsWhenRevokeFails
- `pc-same` battery/auth/password_change.go [if body.Password == body.Current { -> if false {] -> TestPasswordChangeWeakRefused
- `pc-stepup-r` battery/auth/password_change.go [if twoFA && !sess.TwoFactorVerified { -> if (twoFA && !sess.TwoFactorVerified) && false {] -> TestPasswordChangeNeedsTwoFAProof
- `pc-swap-r` battery/auth/password_change.go [if subtle.ConstantTimeCompare([]byte(current), []byte(oldHa -> if (subtle.ConstantTimeCompare([]byte(current), []byte(oldH] -> TestPasswordChangeLosesToReset
- `pc-swap-sql-r` battery/auth/password_change.go [if !swapped { return errPasswordMoved -> if (!swapped) && false { return errPasswordMoved] -> TestPasswordChangeLosesToReset
- `pc-token` battery/auth/password_change.go [if _, tokenAuth := TokenScopes(ctx); tokenAuth { -> if _, tokenAuth := TokenScopes(ctx); tokenAuth && false {] -> TestPasswordChangeRefusesTokenAndEmbedCallers
- `pc-toolong` battery/auth/password_change.go [case len(body.Password) > 128: -> case len(body.Password) > 1280:] -> TestPasswordChangeRefusesOverlongPassword
- `pc-xsite` battery/auth/password_change.go [if rejectCrossSiteForm(w, r) { return } if !guardAu -> if !guardAuthLimit(c.loginLimit, w, r) {] -> TestPasswordChangeCrossSiteRefused
- `pr-addr` battery/auth/password_reset.go [if !p.mgr.allowMailAddress(r.Context(), body.Email) { -> if false {] -> TestForgotPasswordPerAddressLimit, TestMailAddressRateLimitSharedBySendAndForgot
- `pr-apitok-fail` battery/auth/password_reset.go [slog.Warn("password-reset API token revocation failed", -> _ = err] -> TestPasswordResetFailsIfTokensStay
- `pr-apitok-r` battery/auth/password_reset.go [if n, supported, err := p.mgr.revokeUserAPITokens(r.Context -> if n, supported, err := 0, false, error(nil); err != nil {] -> TestResetOfVerifiedAccountEvictsEverything
- `pr-async` battery/auth/password_reset.go [p.delivery.enqueue(requestCtx, to, func(ctx context.Context -> func(f func(context.Context)) { f(requestCtx) }(func(ctx co] -> TestForgotPasswordReturnsBeforeEmailDelivery
- `pr-baseurl` battery/auth/password_reset.go [if why := invalidLinkBaseURL(p.cfg.BaseURL); why != "" { -> if why := invalidLinkBaseURL(p.cfg.BaseURL); false && why !] -> TestEmailBaseURLRedSchemeValidated
- `pr-claim` battery/auth/password_reset.go [err = p.mgr.claimAccount(r.Context(), userID, "password_r -> err = nil] -> TestResetClaimsUnverifiedAccount
- `pr-decoy` battery/auth/password_reset.go [p.burnUnknownBranchWork(r) ->] -> TestForgotPasswordBranchWorkParity
- `pr-devlog` battery/auth/password_reset.go ["token_hash", hashedIdentifier(tok)) -> "token_hash", hashedIdentifier(tok), "url", resetURL)] -> TestPasswordReset_DevModeDoesNotLogLiveURL
- `pr-droplinks` battery/auth/password_reset.go [p.dropLinks(r.Context(), userID, "password_reset", remoteHo -> incomplete] -> TestPasswordReset_KillsSiblingTokens
- `pr-limit` battery/auth/password_reset.go [if cfg.RateLimit == nil { -> if false {] -> TestResetRequestDefaultThrottle
- `pr-linkpath-r` battery/auth/password_reset.go [if p.cfg.LinkPath != "" && (!handler.IsSafeRelativePath(p.c -> if (p.cfg.LinkPath != "" && (!handler.IsSafeRelativePath(p.] -> TestPasswordResetLinkPathMustBeAPath
- `pr-memstore` battery/auth/password_reset.go [if err := refuseInMemoryTokenStore(mgr, "password-reset"); -> if err := error(nil); err != nil {] -> TestLinkPluginsRefuseMemoryStore
- `pr-ml` battery/auth/password_reset.go [if err := p.mgr.dropMagicLinks(r.Context(), userID, "passwo -> if err := error(nil); err != nil {] -> TestResetDropsMagicLinks
- `pr-notice` battery/auth/password_reset.go [p.queueResetNotice(r.Context(), userID, unlinked) ->] -> TestPasswordResetMailsANotice
- `pr-overlong` battery/auth/password_reset.go [if len(body.Password) > 128 { -> if false {] -> TestPasswordResetRefusesOverlongPasswordKeepingToken
- `pr-passkeys` battery/auth/password_reset.go [if n, err := p.mgr.purgeUserPasskeys(r.Context(), userID); -> if n, err := 0, error(nil); err != nil {] -> TestPasskeyResetPurgesBeforeRevoke
- `pr-revoke-fail` battery/auth/password_reset.go [if err := p.mgr.revokeUserSessions(r.Context(), userID, "pa -> if err := p.mgr.revokeUserSessions(r.Context(), userID, "pa] -> TestResetFailsWhenRevokeFails
- `pr-revoke-r` battery/auth/password_reset.go [if err := p.mgr.revokeUserSessions(r.Context(), userID, "pa -> if err := error(nil); err != nil {] -> TestResetOfVerifiedAccountEvictsEverything
- `pr-ttl` battery/auth/password_reset.go [cfg.TokenTTL = time.Hour -> cfg.TokenTTL = 48 * time.Hour] -> TestPasswordResetDefaultTTLIsOneHour
- `pr-uniform-r` battery/auth/password_reset.go [p.burnUnknownBranchWork(r) return -> p.burnUnknownBranchWork(r) w.Header().Set("X-Known", "no] -> TestPasswordReset_ForgotDoesNotEnumerate
- `pr-unlink` battery/auth/password_reset.go [unlinked, err = p.mgr.unlinkAllOAuth(r.Context(), userID, -> _, _ = lister, unlinker] -> TestResetUnlinksOAuthOnVerified
- `pr-unlink-r` battery/auth/password_reset.go [unlinked, err = p.mgr.unlinkAllOAuth(r.Context(), userID, -> _, _ = lister, unlinker] -> TestResetOfVerifiedAccountEvictsEverything
- `pr-validate-first` battery/auth/password_reset.go [if err := ValidatePasswordStrength(body.Password); err != n -> userID0, _ := redeemPurposeToken(r.Context(), p.store, purp] -> TestPasswordReset_TokenSurvivesBadPassword
- `pr-xsite` battery/auth/password_reset.go [func (p *PasswordResetPlugin) forgotHandler(w http.ResponseW -> func (p *PasswordResetPlugin) forgotHandler(w http.ResponseW] -> TestForgotPasswordCrossSiteRefused
- `po-decider` battery/auth/policy.go [if roleGateDenied(ctx, u) { -> if false {] -> TestRolePolicyRedHonorsDecider
- `po-escaped` battery/auth/policy.go [return r.URL.EscapedPath() } -> return r.URL.Path }] -> TestSessionPolicy_NextPathEdgeCases
- `po-next-amp` battery/auth/policy.go [if containsQuery(dest) { -> if false {] -> TestSessionPolicy_NextPathEdgeCases
- `po-next-r` battery/auth/policy.go [if path := requestPathFromCtx(ctx); path != "" && path != -> if path := requestPathFromCtx(ctx); (path != "" && path !] -> TestSessionPolicy_RedirectsAnonWithNextPath
- `po-note` battery/auth/policy.go [handler.NoteSessionRefusal(ctx) return decide.Redirect(d -> return decide.Redirect(dest)] -> TestCrossSiteArrivalHopsOnlyOnSessionPolicyRedirects
- `po-precedence` battery/auth/policy.go [if o.altFactory != nil { -> if o.altFactory != nil && o.redirect == "" {] -> TestPolicyOptionPrecedenceAltFirst
- `po-role` battery/auth/policy.go [if !hasAnyRole(ctx, u, roles) { -> if false {] -> TestRolePolicy_BlocksMissingRole
- `po-token2` battery/auth/policy.go [if _, tokenAuth := TokenScopes(ctx); tokenAuth { -> if _, tokenAuth := TokenScopes(ctx); tokenAuth && false {] -> TestRequireSessionRedRefusesBearer
- `req-prefer` battery/auth/request_session.go [if s.PendingTwoFactor == preferPending { -> if s.PendingTwoFactor != preferPending {] -> TestRequestSessionPrefersRequestedKind
- `req-principal-r` battery/auth/request_session.go [if s.UserID == id { -> if s.UserID == id \|\| true {] -> TestTwoFADisableBoundToPrincipal
- `sess-copy-get2` battery/auth/session.go [cp := *sess m.mu.RUnlock() if cp.Expired() { -> cp := sess m.mu.RUnlock() if cp.Expired() {] -> TestMemSession_GetVsMarkersNoRace
- `sess-del-pending-only` battery/auth/session.go [return s.UserID == userID && s.PendingTwoFactor -> return s.UserID == userID] -> TestTwoFADisableDropsPendingSessions
- `sess-del-user` battery/auth/session.go [return purgeMatching(m.sessions, func(s *Session) bool { ret -> return 0, nil] -> TestPasswordReset_RevokesExistingSessions
- `sess-expired` battery/auth/session.go [if cp.Expired() { -> if false {] -> TestMemorySessionExpiredIsNotFound
- `sess-mark-clears-pending` battery/auth/session.go [sess.PendingTwoFactor = false ->] -> TestPendingTwoFA_Me_AllowedAfterChallenge
- `sess-neg-ttl-r` battery/auth/session.go [if ttl < 0 { return nil, fmt.Errorf("auth: MemorySessionS -> if ttl < 0 && false { return nil, fmt.Errorf("auth: Memor] -> TestMemorySessionStoreRefusesNegativeTTL
- `sess-sweep` battery/auth/session.go [purgeMatching(m.sessions, func(s *Session) bool { return !no ->] -> TestMemorySessionStoreCreateSweepsExpiredSessions
- `rs-note-refusal` battery/auth/session_middleware.go [handler.NoteSessionRefusal(r.Context()) http.Redirec -> http.Redirect(w, r, o.failHTMLPath] -> TestCrossSiteArrivalAtGuardedPageHops
- `rs-redirect-html` battery/auth/session_middleware.go [if o.failHTMLPath != "" && wantsHTML(r) { -> if o.failHTMLPath != "" {] -> TestRequireSession_RedirectOnlyForHTML
- `rs-token-refused` battery/auth/session_middleware.go [if _, tokenAuth := TokenScopes(r.Context()); tokenAuth { -> if _, tokenAuth := TokenScopes(r.Context()); tokenAuth && fa] -> TestRequireSessionRedRefusesBearer
- `sm-all-cookies` battery/auth/session_middleware.go [for _, token := range tokens { if user != nil { -> for _, token := range tokens[:min(1, len(tokens))] { if u] -> TestSessionReadsTryEveryCookieCandidate
- `sm-anon-clear` battery/auth/session_middleware.go [ctx := handler.SetUser(r.Context(), nil) -> ctx := r.Context()] -> TestSessionMiddleware_ClearsUserOnAnonymousFallthrough, TestSessionMiddleware_ClearsUserOnStoreError
- `sm-assurance-same-user-r` battery/auth/session_middleware.go [if err == nil && session != nil && session.UserID == assuran -> if err == nil && session != nil && !session.PendingTwoFactor] -> TestInProcessTwoFAAssuranceRecheckCandidates
- `sm-cand-cap` battery/auth/session_middleware.go [if len(out) == maxSessionCookieCandidates { -> if false {] -> TestSessionCookieFanoutBounded
- `sm-cand-dedupe-r` battery/auth/session_middleware.go [if c.Value == "" \|\| slices.Contains(out, c.Value) { -> if c.Value == "" \|\| (slices.Contains(out, c.Value) && false)] -> TestSessionCookieFanoutBounded, TestSessionCookieCandidatesKeepOrder
- `sm-embed-keep` battery/auth/session_middleware.go [if _, embedded := embed.GrantFromContext(r.Context()); embed -> if _, embedded := embed.GrantFromContext(r.Context()); frame] -> TestInProcessRequireSessionTrustsContextUser
- `sm-log-notfound` battery/auth/session_middleware.go [log.Debug("session: not found in store", -> log.Warn("session: not found in store",] -> TestSessionMiddleware_NotFoundIsNotLouder
- `sm-log-warn` battery/auth/session_middleware.go [log.Warn("session: store lookup failed: request degraded -> log.Debug("session: store lookup failed: request degrade] -> TestSessionMiddleware_LogsStoreError
- `sm-nil-store` battery/auth/session_middleware.go [if mgr.UserStore() == nil { panic( -> if false { panic(] -> TestSessionMiddleware_PanicsOnNilUserStore
- `sm-pending-anon-r` battery/auth/session_middleware.go [if sess.PendingTwoFactor { if log != nil { -> if sess.PendingTwoFactor && false { if log != nil {] -> TestSessionMiddlewareLoadsNoPendingUser
- `sm-recheck` battery/auth/session_middleware.go [ctx = handler.WithPrincipalCheck(ctx, sessionRecheck(mgr ->] -> TestEventStreamClosesOnRevocation
- `sm-recheck-tenant` battery/auth/session_middleware.go [lookupContext = handler.SetTenantID(lookupContext, princi ->] -> TestSessionRecheckPreservesLookupTenant
- `sm-tenant-bound` battery/auth/session_middleware.go [tenantBound: !mgr.globalSessionLookup()} -> tenantBound: false}] -> TestInProcessTwoFAAssuranceTenantBinding, TestInProcessTwoFAAssuranceCustomStoreDefaultTenantBinding
- `rl-escape-r` battery/auth/sql_ratelimit_store.go [return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`) -> return strings.TrimSpace(s)] -> TestSQLRateLimit_SweepEscapesScopeMetacharacters
- `rl-expired-block-attempts` battery/auth/sql_ratelimit_store.go [if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FRO -> if false {] -> TestSQLRateLimit_BlockExpiryResetsBudget
- `rl-insert-first` battery/auth/sql_ratelimit_store.go [if n > cfg.MaxAttempts { -> if n > cfg.MaxAttempts+1 {] -> TestSQLRateLimit_BlocksAtMax
- `rl-schema-retry` battery/auth/sql_ratelimit_store.go [if err := s.EnsureSchema(ctx); err != nil { -> s.schemaReady.Store(true) if err := s.EnsureSchema(ctx); e] -> TestSQLRateLimit_RecoversAfterTransientSchemaError
- `rl-scoped-sweep-r` battery/auth/sql_ratelimit_store.go [scopePrefix := escapeLikeLiteral(ratelimit.NamespaceScope( -> scopePrefix := escapeLikeLiteral(ratelimit.NamespaceScope(] -> TestSQLRateLimit_SweepScopedNotCrossWindow
- `rl-sweep-expired-blocks` battery/auth/sql_ratelimit_store.go [if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FRO -> if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`SELECT 1 F] -> TestSQLRateLimitSweepClearsAttemptsUnderExpiredBlock
- `tok-sig-r` battery/auth/token.go [if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) { -> if (!hmac.Equal([]byte(parts[2]), []byte(expectedSig))) && f] -> TestJWTInvalidSignature
- `tp-check` battery/auth/token_purpose.go [payload, ok := strings.CutPrefix(raw, string(p)+":") if !o -> payload, ok := strings.CutPrefix(raw, string(p)+":") if !o] -> TestNonPeekingStoreRefusesVerificationTokenAtReset, TestNonPeekingStoreRefusesResetTokenElsewhere
- `tp-peek` battery/auth/token_purpose.go [if _, ok := peekPurposePayload(raw, p); !ok { -> if _, ok := peekPurposePayload(raw, p); !ok && false {] -> TestMagicLinkTokenCannotResetPassword
- `tp-purge` battery/auth/token_purpose.go [n, err := purger.DeleteTokensForPayload(ctx, string(p)+":"+ -> n, err := purger.DeleteTokensForPayload(ctx, payload)] -> TestResetDropsMagicLinks
- `tp-tag` battery/auth/token_purpose.go [return store.CreateToken(ctx, string(p)+":"+payload, ttl) -> return store.CreateToken(ctx, payload, ttl)] -> TestSharedStoreStillServesOwnFlows
- `tf-backup-hash` battery/auth/twofa.go [if bcrypt.CompareHashAndPassword([]byte(hashed), []byte(co -> if code != "" {] -> TestMemoryTwoFAStore_ConsumeBackupCode
- `tf-backup-post` battery/auth/twofa.go [r.Post(basePath+"/2fa/backup-codes", http.HandlerFunc(p.bac -> r.Post(basePath+"/2fa/backup-codes", http.HandlerFunc(p.bac] -> TestBackupCodesRejectsGetRequests
- `tf-clone` battery/auth/twofa.go [if st.BackupCodes != nil { -> if false {] -> TestMemoryTwoFAStoreReturnsCopies
- `tf-disable-pending` battery/auth/twofa.go [if purger, ok := p.mgr.SessionStore().(SessionPendingPurger -> if purger, ok := p.mgr.SessionStore().(SessionPendingPurger] -> TestTwoFADisableDropsPendingSessions
- `tf-enroll-cas` battery/auth/twofa.go [if !p.swapTwoFAState(w, r, userID, existing, state) { -> if err := p.store.SetTwoFA(r.Context(), userID, state); err] -> TestTwoFAEnrollNoResurrect
- `tf-enroll-existing` battery/auth/twofa.go [if existing != nil && existing.Enabled { -> if false {] -> TestTwoFAEnrollRefusesOverwritingEnabledFactor
- `tf-judged-vanished` battery/auth/twofa.go [if still, err := store.Get(ctx, sess.Token); err != nil \|\| -> if false {] -> TestTwoFAChallengeRefusesWhenJudgedSessionVanished
- `tf-limit` battery/auth/twofa.go [if c.RateLimit == nil { -> if false {] -> TestTwoFADefaultChallengeThrottle, TestTwoFAVerifyDefaultThrottle
- `tf-mark-fails` battery/auth/twofa.go [if err := p.markSessionTwoFA(ctx, fresh.Token); err != nil -> if err := error(nil); err != nil {] -> TestChallengeFailsWhenMarkFails
- `tf-memstore-prod` battery/auth/twofa.go [if !cfg.DevMode && !cfg.AllowInMemoryStores { -> if false {] -> TestProdRefusesMemoryTwoFAStore
- `tf-period-url-r` battery/auth/twofa.go [q.Set("period", strconv.FormatUint(uint64(period), 10)) -> q.Set("period", strconv.FormatUint(uint64(30+period*0), 10)] -> TestEnrollURLCarriesPeriod
- `tf-require-enrolled` battery/auth/twofa.go [if verified { -> if true {] -> TestTwoFA_RequireMiddleware_BlocksUnverifiedSession
- `tf-require-mine` battery/auth/twofa.go [if session.UserID == u.GetID() && !session.PendingTwoFa -> if !session.PendingTwoFactor {] -> TestRequireTwoFAJWTPlusOtherCookie
- `tf-rotate-r` battery/auth/twofa.go [if err := store.Delete(ctx, sess.Token); err != nil { ret -> if err := error(nil); err != nil { return fail(http.Statu] -> TestTwoFAChallengeRotatesTheSession
- `tf-session-cap` battery/auth/twofa.go [if p.sessionGuesses != nil { -> if false {] -> TestChallengeGuessesCappedPerSession
- `tf-session-cap-err` battery/auth/twofa.go [fail(http.StatusServiceUnavailable, reasonTryAgainShortly -> allowed = true] -> TestChallengeStoreErrorKeepsSession
- `tf-step-cas-r2` battery/auth/twofa.go [if !p.swapTwoFAState(w, r, userID, state, consumed) { -> if err := p.store.SetTwoFA(r.Context(), userID, consumed);] -> TestTwoFAChallengeCodeSingleUseConcurrent
- `tf-step-single-r` battery/auth/twofa.go [ok && step > state.LastUsedStep { -> ok {] -> TestTOTPCodeNotReusableWithinWindow
- `tf-stepup-pending-r` battery/auth/twofa.go [if sess.PendingTwoFactor { writeAuthError -> if sess.PendingTwoFactor && false { writeAuthError] -> TestPendingTwoFA_MutationEndpointsRejected
- `tf-stepup-positive-r` battery/auth/twofa.go [if state != nil && state.Enabled && !sess.TwoFactorVerified -> if (state != nil && state.Enabled && !sess.TwoFactorVerifie] -> TestTwoFA_StepUpNeedsVerifiedFlag
- `tf-verify-cas` battery/auth/twofa.go [if !p.swapTwoFAState(w, r, userID, state, enabled) { -> if err := p.store.SetTwoFA(r.Context(), userID, enabled); e] -> TestTwoFAVerifyNoResurrect
- `tf-verify-marks-r` battery/auth/twofa.go [if err := p.markSessionTwoFA(r.Context(), sess.Token); err -> if err := error(nil); err != nil {] -> TestTwoFA_BackupCodesRefresh
- `tf-xsite-r` battery/auth/twofa.go [if rejectCrossSiteForm(w, r) { -> if rejectCrossSiteForm(w, r) && false {] -> TestTwoFARoutesRejectCrossSite
- `ur-empty-r` battery/auth/update_roles.go [b, _ := json.Marshal(roles) rolesJSON := string(b) -> _, _ = json.Marshal(nil) rolesJSON := formatRoles(roles)] -> TestUpdateRolesEmptyStaysEmpty
- `ur-storable-r` battery/auth/update_roles.go [if !textsafe.Storable(userID) { -> if (!textsafe.Storable(userID)) && false {] -> TestRoleIDsRefuseUnstorableText
- `list-cap` battery/auth/users.go [opts.Limit = 500 -> opts.Limit = 501] -> TestListUsersClampsOptions
- `list-def-r` battery/auth/users.go [opts.Limit = 50 -> opts.Limit = 10] -> TestListUsersClampsOptions
- `list-off` battery/auth/users.go [opts.Offset = 0 -> opts.Offset = -1] -> TestListUsersClampsOptions
- `list-unsup` battery/auth/users.go [return nil, 0, ErrListUsersUnsupported -> return nil, 0, nil] -> TestListUsersUnsupportedStore
- `vp-every-version` battery/auth/verify_private.go [if ent != nil && ent.Config.Name == name { -> if ent != nil && ent.Config.Name == name && ent.Version ==] -> TestAuditSeesEveryVersion
- `vp-flags` battery/auth/verify_private.go [if !crudOn && !mcpOn { -> if true {] -> TestVerifyAuthEntitiesPrivate_FlagsDangerousConfig
- `vp-missing` battery/auth/verify_private.go [if len(versions) == 0 { -> if false {] -> TestVerifyAuthEntitiesPrivate_WarnsOnMissingEntity
- `wa-cbor-depth` battery/auth/webauthn.go [const cborMaxDepth = 8 -> const cborMaxDepth = 1 << 20] -> TestCBORRefusesMalformedInput
- `wa-crossorigin3` battery/auth/webauthn.go [if crossOrigin { -> if crossOrigin && false {] -> TestClientDataRefusals
- `wa-ext-trailing` battery/auth/webauthn.go [if hasAT != attested { -> if false {] -> TestAttestationRefusals
- `wa-origin2` battery/auth/webauthn.go [if !slices.Contains(rp.Origins, origin) { -> if false {] -> TestClientDataRefusals
- `wa-port` battery/auth/webauthn.go [if (u.Scheme == "https" && u.Port() == "443") \|\| (u.Scheme -> if false {] -> TestPasskeyDefaultPortFailsInit
- `wa-rphash3` battery/auth/webauthn.go [if subtle.ConstantTimeCompare(ad.rpIDHash, want[:]) != 1 { -> if subtle.ConstantTimeCompare(ad.rpIDHash, want[:]) != 1 &&] -> TestAssertionRefusals
- `wa-rpid` battery/auth/webauthn.go [if host != rp.ID && !strings.HasSuffix(host, "."+rp.ID) { -> if false {] -> TestPasskeyBadOriginFailsInit
- `wa-rsa-size2` battery/auth/webauthn.go [if mod.BitLen() > maxCOSERSABits \|\| mod.Bit(0) == 0 { -> if mod.Bit(0) == 0 {] -> TestCOSEKeyRefusals
- `wa-sig3` battery/auth/webauthn.go [if !key.verify(msg, sig) { -> if !key.verify(msg, sig) && false {] -> TestAssertionRefusals
- `wa-uv2` battery/auth/webauthn.go [if ad.flags&flagUV == 0 { -> if false {] -> TestAssertionRefusals

## auth, reruns after the merge with main

- jwt-prod-m: battery/auth/manager.go [if !m.config.DevMode && m.config.JWTSecret == "" { -> if false {] -> TestInit_ProdEmptySecretFailsClosed
- jwt-dev-mint-m: battery/auth/manager.go [secret, err := randomDevJWTSecret() -> secret, err := "", fmt.Errorf("x")] -> TestAuthManager_DevModeMintsJWT
- neg-ttl-m: battery/auth/manager.go [if m.config.SessionTTL < 0 { -> if false {] -> TestNegativeSessionTTLRefused
- neg-jwt-m: battery/auth/manager.go [if m.config.JWTExpiry < 0 { -> if false {] -> TestNegativeJWTExpiryRefused
- ttl-default-m: battery/auth/manager.go [c.SessionTTL = 7 * 24 * time.Hour -> c.SessionTTL = 24 * time.Hour] -> TestZeroSessionTTLDefaultsToSevenDays
- prod-cookie-m: battery/auth/manager.go [c.SessionCookie = "__Host-session" -> c.SessionCookie = "session"] -> TestAuthConfig_DefaultsAreSecure
- prod-secure-m: battery/auth/manager.go [c.SessionSecure = true -> c.SessionSecure = false] -> TestAuthConfig_DefaultsAreSecure
- dev-cookie-m: battery/auth/manager.go [c.SessionCookie = "session_id" -> c.SessionCookie = "sid"] -> TestAuthConfig_DevMode_RelaxedDefaults
- mem-sess-post-m: battery/auth/manager.go [if err := m.refuseInMemorySessionStore(); err != nil { -> if err := m.refuseInMemorySessionStore(); err != nil && fals] -> TestProdRefusesMemorySessionStoreInstalledByPlugin
- mem-optin-m: battery/auth/manager.go [if m.config.DevMode || m.config.AllowInMemoryStores { -> if m.config.DevMode {] -> TestNoMemoryStoreWarnWhenOptedIn
- login-limit-m: battery/auth/manager.go [MaxAttempts:   30, -> MaxAttempts:   31,] -> TestDefaultThrottlesInstalledInDevModeToo
- acct-limit-m: battery/auth/manager.go [if c.LoginRateLimitPerAccount == nil { -> if false {] -> TestDefaultThrottlesInstalledInDevModeToo, TestAuthBypass_DefaultBruteForceLimiter
- reg-limit-m: battery/auth/manager.go [if c.RegisterRateLimit == nil { -> if false {] -> TestDefaultThrottlesInstalledInDevModeToo, TestRegisterRateLimitedByDefault
- mail-limit-m: battery/auth/manager.go [MaxAttempts:   1, -> MaxAttempts:   5,] -> TestDefaultThrottlesInstalledInDevModeToo
- roles-default-m: battery/auth/manager.go [return []string{"user"} -> return []string{"admin"}] -> TestDefaultRolesReturnsACopy
- roles-copy-m: battery/auth/manager.go [copy(out, m.config.DefaultRoles)
		return out -> _ = out
		return m.config.DefaultRoles] -> TestDefaultRolesReturnsACopy
- roles-audit-m: battery/auth/manager.go [Kind:   "roles.updated", -> Kind:   "roles.x",] -> TestSetUserRolesAuditsMutation
- roles-storable-m: battery/auth/manager.go [if !textsafe.Storable(userID) { -> if !textsafe.Storable(userID) && false {] -> TestSetUserRolesRefusesUnstorableID
- jwt-in-new-m: battery/auth/manager.go [if config.JWTSecret != "" { -> if false {] -> TestRequireAuthWiredBeforeInit
- jwt-store-post-m: battery/auth/manager.go [m.jwtAuth.SetUserStore(m.userStore)
	} -> }] -> TestJWTResolvesAgainstStoreAPluginInstalled
- noted-m: battery/auth/manager.go [if app != nil && ok { -> if app != nil && (ok || true) {] -> TestAuthNotedOnlyWithMiddleware
- onstop-eraser-m: battery/auth/manager.go [unregisterMemoryStoreEraser(m) -> ] -> TestMemoryStoresReachedByErasure
- mw-order-m: battery/auth/manager.go [for _, v := range slices.Backward(m.order) { -> for _, v := range m.order {] -> TestPluginMiddlewareFirstRegisteredOutermostAndStopReversed
- stop-order-m: battery/auth/manager.go [for _, name := range slices.Backward(m.order) { -> for _, name := range m.order {] -> TestPluginMiddlewareFirstRegisteredOutermostAndStopReversed
- stop-firsterr-m: battery/auth/manager.go [err != nil && firstErr == nil { -> err != nil {] -> TestPluginMiddlewareFirstRegisteredOutermostAndStopReversed
- dup-panic-m: battery/auth/manager.go [if _, exists := m.plugins[plugin.Name()]; exists { -> if false {] -> TestAuthManager_UseDuplicatePanics
- embed-prefix-m: battery/auth/manager.go [return []string{m.Config().BasePath} -> return []string{"/auth"}] -> TestAuthReservesItsBasePathFromEmbeds
- core-2fa-url-m: battery/auth/core.go [if _, ok := mgr.plugins[name].(TwoFactorChecker); ok { -> if _, ok := mgr.plugins[name].(TwoFactorChecker); ok && fals] -> TestCoreInitRequiresTwoFactorURLWith2FA
- core-2fa-redirect-m: battery/auth/core.go [http.Redirect(w, r, safeRedirectURL(c.twoFactorURL), http.St -> http.Redirect(w, r, successRedirect(w, r, "/"), http.StatusS] -> TestCoreFormLoginOf2FAAccountGoesToTwoFactorURL
- core-dev-ip-m: battery/auth/core.go [ipCfg.DevMode = cfg.DevMode -> ipCfg.DevMode = false] -> TestRateLimit_DevModeRelaxesPerIPLogin
- core-xsite-forgeable-m: battery/auth/core.go [if !handler.IsForgeableRequest(r) { -> if !isFormRequest(r) {] -> TestVerifyRejectsCrossSiteTextPlain, TestVerifyRejectsCrossSiteNoBody
- core-xsite-m: battery/auth/core.go [if handler.IsCrossSiteRequest(r) {
		writeFormAuthError(w, r -> if false {
		writeFormAuthError(w, r, http.StatusForbidden,] -> TestLoginFormCrossOriginRejected, TestLoginFormCrossSiteFetchMetadataRejected, TestRegisterFormCrossOriginRejected
- core-xsite-before-limit-m: battery/auth/core.go [if rejectCrossSiteForm(w, r) {
			return
		}
		if !guardAu -> if !guardAuthLimit(c.loginLimit, w, r) {
			return
		}
		i] -> TestLoginCrossSitePostsDontBurnLoginBudget
- core-notice-queue-m: battery/auth/core.go [c.notices.enqueue(r.Context(), holderEmail, scrubbedNoticeSe -> func(f func(context.Context)) { f(r.Context()) }(scrubbedNot] -> TestDuplicateNoticeBoundedAndStopped
- tp-peek-m: battery/auth/token_purpose.go [if _, _, ok := match(raw); !ok { -> if _, _, ok := match(raw); !ok && false {] -> TestMagicLinkTokenCannotResetPassword
- tp-check-m: battery/auth/token_purpose.go [p, payload, ok := match(raw)
	if !ok { -> p, payload, ok := match(raw)
	if !ok && false {] -> TestNonPeekingStoreRefusesVerificationTokenAtReset, TestNonPeekingStoreRefusesResetTokenElsewhere
- fd-next-m: battery/auth/form_decode.go [return handler.SafeRelativePathOr(next, fallback) -> return cmp.Or(next, fallback)] -> TestLoginFormRejectsExternalRedirect, TestSuccessRedirect_RejectsBackslashBypass
- oi-disc-issuer-m: battery/auth/oidc.go [} else if normalizeIssuer(d.Issuer) != normalizeIssuer(p.cf -> } else if false {] -> TestOIDCSec_DiscoveryIssuerMismatch
- oi-iss-m: battery/auth/oidc.go [if iss != normalizeIssuer(p.cfg.Issuer) { -> if false {] -> TestOIDCSec_WrongIssuer
- pk-handle-m: battery/auth/passkey.go [given && string(handle) != cred.UserID { -> given && string(handle) != cred.UserID && false {] -> TestPasskeyUserHandleMustMatch
- wa-uv2-m: battery/auth/webauthn.go [if !conditional && ad.flags&flagUV == 0 { -> if !conditional && ad.flags&flagUV == 0 && false {] -> TestAssertionRefusals
- fix-state-exp: battery/auth/oauth2.go [if time.Now().After(expiry) {
		return "", "", time.Time{}, -> if time.Now().After(expiry) && false {
		return "", "", tim] -> TestOAuth2Plugin_StateExpiry
- fix-weak-rsa: battery/auth/oidc_jwks.go [if n.BitLen() < 2048 { -> if n.BitLen() < 1024 {] -> TestOIDCSec_WeakRSAKey
- core-xsite-before-limit-m: battery/auth/core.go [if rejectCrossSiteForm(w, r) {
			return
		}
		if !guardAu -> if !guardAuthLimit(c.loginLimit, w, r) {
			return
		}
		i] -> TestLoginCrossSitePostsDontBurnLoginBudget
- core-limit-form303-m: battery/auth/core.go [if isFormRequest(r) {
		writeFormAuthError(w, r, http.Statu -> if false {
		writeFormAuthError(w, r, http.StatusTooManyReq] -> TestRegisterFormRateLimit303
- core-cookie-strict-m: battery/auth/core.go [SameSite: http.SameSiteStrictMode, -> SameSite: http.SameSiteLaxMode,] -> TestCorePlugin_LoginCookieUsesStrictSameSite, TestCorePlugin_LogoutCookieUsesStrictSameSite
- core-cookie-httponly-m: battery/auth/core.go [HttpOnly: true,
		Secure:   cfg.SessionSecure, -> HttpOnly: false,
		Secure:   cfg.SessionSecure,] -> TestSessionCookieAttributes
- core-cookie-secure-m: battery/auth/core.go [Secure:   cfg.SessionSecure,
		SameSite: http.SameSiteStri -> Secure:   false,
		SameSite: http.SameSiteStrictMode,] -> TestSessionCookieAttributes
- core-cookie-clear-m: battery/auth/core.go [c.MaxAge = -1 -> ] -> TestSessionCookieAttributes
- core-acct-limit-m: battery/auth/core.go [allowed, retry := c.loginLimitAccount.AllowContext(r.Context -> allowed, retry := true, time.Duration(0); _ = key] -> TestRateLimit_PerAccount_BlocksIPRotationAttack
- core-dummy-hash-m: battery/auth/core.go [_ = verifyPassword(password, dummyHashFor(DefaultHasher)) -> ] -> TestLogin_TimingSafe_NoEnumerationByResponseTime
- core-2fa-failclosed-m: battery/auth/core.go [if err != nil && !errors.Is(err, ErrTwoFactorEnforcement) { -> if err != nil && !errors.Is(err, ErrTwoFactorEnforcement) &&] -> TestLoginSessionCreateFailureIsNotATwoFactorFailure
- core-cred-recheck-m: battery/auth/core.go [if !credentialUnchanged(r.Context(), store, email, user.GetI -> if false {] -> TestLoginLosesRaceToReset
- core-cred-ct-m: battery/auth/core.go [if err != nil || row == nil || row.GetID() != userID { -> if err != nil || row == nil {] -> TestLoginRefusedWhenEmailNowNamesAnotherUser
- core-nojwt-pending-m: battery/auth/core.go [if c.mgr.JWT() != nil && !pendingTwoFA && !cfg.CookieOnly { -> if c.mgr.JWT() != nil && !cfg.CookieOnly {] -> TestPendingTwoFA_NoJWTIssued
- core-cookieonly-m: battery/auth/core.go [if c.mgr.JWT() != nil && !pendingTwoFA && !cfg.CookieOnly { -> if c.mgr.JWT() != nil && !pendingTwoFA {] -> TestBFFPostureSuppressesLoginJWT
- core-jwt-bound-m: battery/auth/core.go [token, err := c.mgr.JWT().generateToken(user, hash) -> token, err := c.mgr.JWT().generateToken(user, "")] -> TestJWTDiesWithCredentialChange
- core-logout-failclosed-m: battery/auth/core.go [if revokeFailed { -> if revokeFailed && false {] -> TestLogoutFailsClosedOnRevokeFailure
- core-logout-audit-after-m: battery/auth/core.go [revokeFailed = true
				continue -> revokeFailed = true] -> TestLogoutFailsClosedOnRevokeFailure
- core-reg-500-m: battery/auth/core.go [if err != nil && !taken { -> if err != nil && !taken && false {] -> TestRegisterStoreFailureIs500
- core-reg-notice-m: battery/auth/core.go [c.deliverRegisterDuplicateNotice(r, holder.GetEmail()) -> ] -> TestRegisterDuplicateNotifiesHolder
- core-reg-errpath-m: battery/auth/core.go [r = asRegisterForm(r) -> ] -> TestRegisterErrorsLandOnRegisterPath
- core-encode-500-m: battery/auth/core.go [writeAuthError(w, r, http.StatusInternalServerError, i18n. -> w.WriteHeader(status)
		return] -> TestMeAnswersEncodeFailure
- core-nostore-m: battery/auth/core.go [writeCredentialHeaders(w)
	w.Header().Set("Content-Type", " -> w.Header().Set("Content-Type", "application/json")] -> TestCredentialResponsesSetNoStore
- core-unphrased-m: battery/auth/core.go [writeAuthError(w, r, status, frame)
} -> writeAuthErrorFields(w, r, status, frame, map[string]string] -> TestCanonicalizerErrorTextNotEchoed
- core-baseurl-scheme-m: battery/auth/core.go [if u.Scheme != "http" && u.Scheme != "https" { -> if false {] -> TestEmailBaseURLRedSchemeValidated
- core-baseurl-host-m: battery/auth/core.go [if u.Host == "" { -> if false {] -> TestLinkBaseURLNeedsAHost
- core-csd-panic-m: battery/auth/core.go [if !clearSiteDataDirectives[d] { -> if false {] -> TestLogoutClearSiteDataConfigurable
- tp-tag-m: battery/auth/token_purpose.go [return store.CreateToken(ctx, string(p)+":"+payload, ttl) -> return store.CreateToken(ctx, payload, ttl)] -> TestSharedStoreStillServesOwnFlows
- er-tagged-m: battery/auth/erase.go [ValuePrefix: string(p) + ":", -> ValuePrefix: "",] -> TestEraseReachesTaggedTokens
- er-email-identity-m: battery/auth/erase.go [if p == purposeMagicLink || p == purposeEmailCode { -> if p == purposeMagicLink {] -> TestEraseReachesEmailKeyedTokensFromLegacyRow
- er-canon-m: battery/auth/erase.go [Canonicalize: canonicalEmailOrEmpty(mgr.config.Canonicali -> ] -> TestEraseReachesEmailKeyedTokensFromLegacyRow
- er-memory-m: battery/auth/erase.go [registerMemoryStoreEraser(mgr) -> ] -> TestMemoryStoresReachedByErasure
- es-clear-fresh-m: battery/auth/entity_store.go ["UPDATE %s SET %s = $1, %s = FALSE WHERE %s = $2", -> "UPDATE %s SET %s = %s, %s = FALSE WHERE %s = $2 AND $1 = $1] -> TestJWTDiesWithCredentialChange
- es-sess-digest-m: battery/auth/entity_store.go [_, err = s.db.ExecContext(ctx, q, sessionID, sha256hex(tok) -> _, err = s.db.ExecContext(ctx, q, sessionID, tok, userID, n] -> TestSessionRedStoredOpaque
- es-sess-sweep-m: battery/auth/entity_store.go [s.sweepExpiredOnCreate(ctx, now) -> ] -> TestEntitySessionStoreCreateSweepsExpiredSessions
- es-sess-pending-only-m: battery/auth/entity_store.go ["DELETE FROM %s WHERE user_id = $1 AND pending_two_factor = -> "DELETE FROM %s WHERE user_id = $1"] -> TestEntityStoreDeletesOnlyPending
- es-sweep-retry-m: battery/auth/entity_store.go [if s.lastSweep.Equal(now) { -> if false {] -> TestEntitySessionSweepFailureRetriesLater
- fd-storable-m: battery/auth/form_decode.go [if !textsafe.Storable(email) {
		return "", i18n.NewError(r -> if false {
		return "", i18n.NewError(reasonInvalidEmail)] -> TestEmailRefusesUnstorableText
- fd-invisible-m: battery/auth/form_decode.go [canonical := norm.NFC.String(strings.ToLower(strings.TrimSp -> canonical := norm.NFC.String(strings.ToLower(strings.TrimSp] -> TestInvisibleTwinEmailsOneAccount
- fd-lower-m: battery/auth/form_decode.go [canonical := norm.NFC.String(strings.ToLower(strings.TrimSp -> canonical := norm.NFC.String(strings.TrimSpace(stripInvisib] -> TestLoginEmailCaseInsensitive, TestCanonicalEmail
- fd-shape-m: battery/auth/form_decode.go [if canonical != "" && !addressShaped(canonical) { -> if false {] -> TestCanonicalEmailRefusesNonAddress, TestRegisterRefusesNonAddress
- fd-fallback-m: battery/auth/form_decode.go [if fallback != "" && handler.IsSafeRelativePath(fallback) -> if false {] -> TestFormAuthError_FallsBackToConfiguredPath
- fd-register-path-m: battery/auth/form_decode.go [if on, _ := r.Context().Value(registerFormKey{}).(bool); on -> if false {] -> TestRegisterErrorsLandOnRegisterPath
- fd-sanitise-m: battery/auth/form_decode.go [if c == ' ' || c < 0x20 || c > 0x7e { -> if false {] -> TestSanitiseErrMakesURLAndHTMLSafe
- fd-nostore-m: battery/auth/form_decode.go [w.Header().Set("Cache-Control", "no-store, no-cache, must-r -> ] -> TestCredentialResponsesSetNoStore
- mq-recover-m: battery/auth/mailqueue.go [if recover() != nil { -> if r := recover(); r != nil {
			panic(r)] -> TestMailQueueRecoversSenderPanic
- mq-nonblock-m: battery/auth/mailqueue.go [case q.queue <- job:
		queued = true
	default:
	} -> case q.queue <- job:
		queued = true
	}] -> TestPasswordResetDeliveryDroppedWhenQueueFull
- mq-deadline-m: battery/auth/mailqueue.go [ctx, cancel := context.WithTimeout(job.ctx, q.timeout) -> ctx, cancel := context.WithCancel(job.ctx)] -> TestDuplicateNoticeBoundedAndStopped
- mq-stop-cancels-m: battery/auth/mailqueue.go [stopCancel := context.AfterFunc(q.ctx, cancel) -> stopCancel := func() bool { return true }] -> TestDuplicateNoticeBoundedAndStopped
- oa-state-secret-len-m: battery/auth/oauth2.go [if len(cfg.StateSecret) < minStateSecretLen { -> if false {] -> TestStateSecretTooShortRefused
- oa-sweep-rate-m: battery/auth/oauth2.go [&& now.Sub(p.lastNonceSweep) >= nonceGCSweepInterval { -> {] -> TestOAuth2NonceSweepIsRateLimitedAndReclaimsExpired
- oa-registry-key-m: battery/auth/oauth2.go [info.Provider = providerName -> ] -> TestOAuthLinksKeyedToRegistryName
- oa-link-steal-m: battery/auth/oauth2.go [owner, ferr := linker.FindByOAuth(r.Context(), info.Provi -> owner, ferr := User(nil), ErrUserNotFound] -> TestOAuthLinkRefusesBoundIdentity
- oa-link-hop-m: battery/auth/oauth2.go [if p.sameSiteHop(w, r, stateParam, providerName) { -> if false {] -> TestOAuthLinkSurvivesStrictCookie
- oa-r-nolinker-m: battery/auth/oauth2.go [return nil, false, errOAuthNoLinker -> u, _, e := store.FindByEmail(ctx, info.Email)
		return u,] -> TestResolveOAuth_NoLinker_FailsClosed
- oa-r-noemail-m: battery/auth/oauth2.go [if info.Email == "" {
		return nil, false, errOAuthNoEmail -> if false {
		return nil, false, errOAuthNoEmail] -> TestOAuthEmptyEmailNeverMergesAccounts
- oa-r-verified-m: battery/auth/oauth2.go [if info.EmailVerified {
			hasPw := true -> if true {
			hasPw := true] -> TestResolveOAuth_UnverifiedEmail_DoesNotMatchPasswordAccount
- oa-r-haspw-err-m: battery/auth/oauth2.go [return nil, false, fmt.Errorf("%w: HasPassword: %v", er -> hp = false] -> TestResolveOAuth_HasPasswordError_FailsClosed
- oa-r-refresh-update-only-m: battery/auth/oauth2.go [if info.EmailVerified && info.Email != "" && found.GetEmai -> if info.EmailVerified {] -> TestVerifiedLoginMarksAccount
BUILDFAIL oa-gh-verified-m: battery/auth/oauth2.go [} else if email != "" {
		emailVerified = false
	} -> } else if email != "" {
		emailVerified = true
	}] -> TestGitHubPublicEmailUnconfirmedIsUnverified

- oa-norule-redirect-m: battery/auth/oauth2.go [var defaultOAuthHTTPClient = oidcNoRedirect(&http.Client{Tim -> var defaultOAuthHTTPClient = &http.Client{Timeout: 10 * time] -> TestProviderFetchRefusesRedirect
- oi-secret-required-m: battery/auth/oidc.go [if cfg.ClientSecret == "" { -> if false {] -> TestOIDC_NewProviderValidation
- oi-https-m: battery/auth/oidc.go [if !isLocalhostHost(u.Hostname()) { -> if false {] -> TestOIDCHttpIssuerOnlyLiteralLoopback
- oi-endpoint-https-m: battery/auth/oidc.go [if plainHTTPIssuer && isLocalhostHost(u.Hostname()) { -> if true {] -> TestOIDCSec_EndpointDowngradeRefused, TestValidateOIDCEndpoint
- oi-endpoint-userinfo-m: battery/auth/oidc.go [if u.User != nil { -> if false {] -> TestValidateOIDCEndpoint
- oi-ui-sub-m: battery/auth/oidc.go [if tokenSub != "" && tokenSub != uiSub { -> if tokenSub != "" && uiSub != "" && tokenSub != uiSub {] -> TestOIDCSec_UserinfoMissingSub
- oi-azp-m: battery/auth/oidc.go [if azp != p.cfg.ClientID { -> if false {] -> TestOIDCSec_SingleAudBadAzp
- oi-multiaud-m: battery/auth/oidc.go [} else if len(auds) > 1 { -> } else if false {] -> TestOIDCSec_AudArrayNoAzp
- jw-curve-m: battery/auth/oidc_jwks.go [if !curve.IsOnCurve(x, y) { -> if false {] -> TestOIDCEcPointOffCurveRejected
- jw-kty-m: battery/auth/oidc_jwks.go [if hdr.Alg == "RS256" && key.rsa == nil { -> if false {] -> TestOIDCSec_ECKeyForRS256
- ots-open-auth-m: battery/auth/oauth_token_store.go [if !ok {
		return OAuthTokenRecord{}, errors.New("auth: oau -> if false {
		return OAuthTokenRecord{}, errors.New("auth: o] -> TestOAuthTokenWrongKeyFailsToOpen
- ots-cas-m: battery/auth/oauth_token_store.go [WHERE user_id = $1 AND provider = $2 AND refresh_sha = $7 -> WHERE user_id = $1 AND provider = $2 AND ($7 = $7)`,] -> TestRefreshDoesNotClobberNewerToken
- ots-noaccess-m: battery/auth/oauth_token_store.go [if err == nil && (tok == nil || tok.AccessToken == "") { -> if err == nil && tok == nil {] -> TestRefreshWithoutAnAccessTokenStoresNothing
- ots-registry-key-m: battery/auth/oauth_token_store.go [return validOAuthToken(ctx, p.tokenStore, provider, provide -> return validOAuthToken(ctx, p.tokenStore, provider, provide] -> TestPluginTokenMethodsUseRegistryKey
- pk-2fa-url-m: battery/auth/passkey.go [if _, ok := mgr.plugins[name].(TwoFactorChecker); ok { -> if _, ok := mgr.plugins[name].(TwoFactorChecker); ok && f] -> TestPasskeyTwoFactorNeedsURL
- pk-full-verified-m: battery/auth/passkey.go [if !sess.TwoFactorVerified { -> if false {] -> TestPasskeyRegisterNeedsSecondFactor
- pk-handle-len-m: battery/auth/passkey.go [if len(sess.UserID) > maxUserHandleLen { -> if false {] -> TestPasskeyLongUserIDRefused
- pk-cap-options-m: battery/auth/passkey.go [if len(existing) >= p.config.MaxPerUser { -> if false {] -> TestPasskeyPerUserCap
- pk-reg-id-m: battery/auth/passkey.go [if string(res.credID) != string(f["id"]) { -> if false {] -> TestPasskeyRegisterIDMustMatch
- pk-be-m: battery/auth/passkey.go [if res.backupEligible != cred.BackupEligible { -> if false {] -> TestPasskeyBackupEligibleIsFixed
- pk-count-m: battery/auth/passkey.go [if (res.signCount != 0 || cred.SignCount != 0) && res.signC -> if false {] -> TestPasskeySignCountMustAdvance
- pk-count-zero-m: battery/auth/passkey.go [if (res.signCount != 0 || cred.SignCount != 0) && res.signC -> if res.signCount <= cred.SignCount {] -> TestPasskeyZeroCounterSignsIn
- pk-2fa-down-m: battery/auth/passkey.go [if errors.Is(err, ErrTwoFactorEnforcement) { -> if false {] -> TestPasskeyTwoFADownFailsClosed
- pk-reset-recheck-m: battery/auth/passkey.go [if _, err := p.store.GetPasskey(r.Context(), cred.ID); err -> if false {] -> TestPasskeyLoginOvertakenByReset
- pk-rate-m: battery/auth/passkey.go [if !p.limit.guard(w, r) || !requireJSON(w, r) { -> if !requireJSON(w, r) {] -> TestPasskeyRoutesAreRateLimited
- wa-rpid-m: battery/auth/webauthn.go [if host != rp.ID && !strings.HasSuffix(host, "."+rp.ID) { -> if false {] -> TestPasskeyBadOriginFailsInit
- au-dash-m: battery/auth/audit.go [if recordID == "" { -> if false {] -> TestSQLAuditSink_EmptyUserIDRecordDash
- au-noaddr-m: battery/auth/audit.go [diff["email_hash"] = hashedIdentifier(addr) -> diff["email"] = addr] -> TestAuditActorlessRowHoldsNoAddress
- au-hash-canon-m: battery/auth/audit.go [if c, err := CanonicalEmail(addr); err == nil { -> if c, err := CanonicalEmail(addr); err == nil && false {] -> TestAuditEmailHashIsCanonical
- ts-legacy-m: battery/auth/entity_twofa_store.go [if isLegacyPlaintextSeed(stored) {
		return stored, false, -> if false {
		return stored, false, nil] -> TestTwoFAStoreReadsLegacyPlaintext
- ts-nil-deletes-m: battery/auth/entity_twofa_store.go [if state == nil {
		return s.DeleteTwoFA(ctx, userID) -> if state == nil {
		return nil] -> TestSetTwoFANilDeletesEnrollment
- pk-cap-insert-m: battery/auth/passkey.go [if err := p.store.AddPasskey(r.Context(), cred, p.config.Ma -> if err := p.store.AddPasskey(r.Context(), cred, 1<<30); err] -> TestPasskeyCapHoldsAtRegister
- jw-weak2-m: battery/auth/oidc_jwks.go [if n.BitLen() < 2048 { -> if n.BitLen() < 2048 && false {] -> TestOIDCWeakRSAJWKSkipped
- jw-algallow2-m: battery/auth/oidc_jwks.go [if hdr.Alg != "RS256" && hdr.Alg != "ES256" { -> if hdr.Alg == "none" {] -> TestOIDCSec_AlgHS256, TestOIDCSec_AlgLowercase
- oa-state-expiry2-m: battery/auth/oauth2.go [if time.Now().After(expiry) {
		return "", "", time.Time{}, -> if time.Now().After(expiry) && false {
		return "", "", tim] -> TestExpiredStateRefused
- mq-detach2-m: battery/auth/mailqueue.go [job := mailJob{ctx: context.WithoutCancel(requestCtx), -> job := mailJob{ctx: requestCtx,] -> TestMailQueueDetachesFromRequestCancel
- oa-bound-browser2-m: battery/auth/oauth2.go [return subtle.ConstantTimeCompare([]byte(c.Value), []byte(s -> return len(c.Value) > 0] -> TestOAuthCallbackRefusesAnotherFlowsCookie
- pk-full-pending2-m: battery/auth/passkey.go [if sess.PendingTwoFactor { -> if sess.PendingTwoFactor && false {] -> TestPasskeyRegisterNeedsSecondFactor
- oa-pkce-url2-m: battery/auth/oauth2.go [return pp.AuthURLWithChallenge(state, pkceChallenge(p.pkce -> _ = pp
		return provider.AuthURL(state)] -> TestOAuthRedirectSendsPKCEChallenge
- oi-ui-verified2-m: battery/auth/oidc.go [if !verifiedPresent && claimString(ui, p.cfg.Claims.Emai -> if true {] -> TestOIDCVerifiedFlagNeedsSameEmail
- wa-ext-trailing-m: battery/auth/webauthn.go [if hasAT != attested { -> if false {] -> TestAttestationRefusals
- es-setpw-unknown-r-m: battery/auth/entity_store.go [if n == 0 { -> if (n == 0) && false {] -> TestPasswordSettersUnknownUserFailClosed
- cleanname-r-m: battery/auth/name.go [|| textsafe.ContainsUnsafe(name) { -> || (textsafe.ContainsUnsafe(name) && false) {] -> TestCleanNameRefusesUnsafeNames
- er-host-resolver-r-m: battery/auth/erase.go [(r.Canonicalize == nil || (r.Table == "auth_users" && r.ID -> (r.Canonicalize == nil || true) {] -> TestHostResolverKeepsItsCanonicalizer
- er-login-limit-r-m: battery/auth/erase.go [prefix := ratelimit.NamespaceScope(scope) + "|account:" -> prefix := ratelimit.NamespaceScope("")[:0] + "account:"] -> TestEraseRemovesLoginLimitRows
- er-sessions-r-m: battery/auth/erase.go [Name: "auth_sessions", Source: "auth", Table: ss.table, -> Name: "auth_sessions", Source: "auth", Table: "auth_sessi] -> TestEraseCoversConfiguredStoreTables
- es-sess-expired-r-m: battery/auth/entity_store.go [if sess.Expired() { -> if sess.Expired() && false {] -> TestEntitySessionStoreExpiredIsNotFound
- fd-nfc-r-m: battery/auth/form_decode.go [canonical := norm.NFC.String(strings.ToLower(strings.TrimSp -> canonical := norm.NFC.String("") + strings.ToLower(strings.] -> TestRegisterFoldsUnicodeTwinEmails
- fd-maxlen-r-m: battery/auth/form_decode.go [if len(email) > maxEmailLen { -> if len(email) > maxEmailLen && false {] -> TestOverlongShapedEmailRefused
- fd-override-storable-r-m: battery/auth/form_decode.go [if !textsafe.Storable(canonical) { -> if !textsafe.Storable(canonical) && false {] -> TestCanonicalizerOverrideOutputMustBeStorable
- noted-r-m: battery/auth/manager.go [if app != nil && ok { -> if app != nil && (ok || true) {] -> TestAuthNotedOnlyWithMiddleware
- oa-r-proven-r-m: battery/auth/oauth2.go [if !proven { -> if (!proven) && false {] -> TestOAuthUnverifiedPrecreateNoMerge
- oi-aud-r-m: battery/auth/oidc.go [if !found {
		return errors.New("oidc: id_token aud does no -> if (!found) && false {
		return errors.New("oidc: id_token] -> TestOIDCSec_AudMissingClient
- oi-exp-r-m: battery/auth/oidc.go [if now >= exp+leeway { -> if (now >= exp+leeway) && false {] -> TestOIDCSec_Expired
- oi-iat-r-m: battery/auth/oidc.go [if iat > now+leeway { -> if (iat > now+leeway) && false {] -> TestOIDCSec_FutureIat
- oi-nbf-r-m: battery/auth/oidc.go [if now+leeway < nbf { -> if (now+leeway < nbf) && false {] -> TestOIDCSec_FutureNbf
- oi-ui-sub-mismatch-r-m: battery/auth/oidc.go [if tokenSub != "" && tokenSub != uiSub { -> if (tokenSub != "" && tokenSub != uiSub) && false {] -> TestOIDCSec_UserinfoSubMismatch
SURVIVED pk-delete-own-r-m: battery/auth/passkey.go [if !deleted { -> if (!deleted) && false {] -> TestPasskeyDeleteOnlyOwn
not failing: TestPasskeyDeleteOnlyOwn
ok  	github.com/DonaldMurillo/gofastr/battery/auth	0.029s

- pk-memstore-r-m: battery/auth/passkey.go [if !memoryOK { -> if (!memoryOK) && false {] -> TestPasskeyProdRefusesMemoryStores
- pk-reg-fence-r-m: battery/auth/passkey.go [if _, err := p.mgr.SessionStore().Get(r.Context(), sess.Tok -> if _, err := p.mgr.SessionStore().Get(r.Context(), sess.Tok] -> TestPasskeyRegisterOvertakenByReset
- ts-reseal-r-m: battery/auth/entity_twofa_store.go [if viaPrevious { -> if (viaPrevious) && false {] -> TestTwoFADrainResealsUnderCurrentKey
- au-scrub-r-m: battery/auth/audit.go [textsafe.Recovered(r) -> r, "x", textsafe.Recovered] -> TestAuthAuditRedPanicScrubbed
- ots-erase-r-m: battery/auth/oauth_token_store.go [Column: "user_id", Mode: datexport.EraseDelete, -> Column: "provider", Mode: datexport.EraseDelete,] -> TestEraseRemovesOAuthTokens
- pk-cas-r-m: battery/auth/passkey.go [if !wrote { -> if !wrote && false {] -> TestPasskeyLostRaceIs409
- ts-fresh-version-r-m: battery/auth/entity_twofa_store.go [return int64(binary.BigEndian.Uint64(b[:])>>2) | 1, nil -> return int64(binary.BigEndian.Uint64(b[:])>>2)*0 + 1, nil] -> TestTwoFACASRefusesReenrolledRow
- oa-mint-2fa-r-m: battery/auth/oauth2.go [target = safeRedirectURL(p.twoFactorURL) -> ] -> TestOAuthPendingSignInGoesToTwoFactorURL
BADSPEC pk-reg-owner-r2-m: battery/auth/passkey.go [if err != nil || owner != sess.UserID { -> if err != nil || (owner != sess.UserID && false) {] -> TestPasskeyRegisterOthersChallenge
old occurs 2 times
- wa-sig3-m: battery/auth/webauthn.go [if !key.verify(msg, sig) { -> if !key.verify(msg, sig) && false {] -> TestAssertionRefusals
- wa-rphash3-m: battery/auth/webauthn.go [if subtle.ConstantTimeCompare(ad.rpIDHash, want[:]) != 1 { -> if subtle.ConstantTimeCompare(ad.rpIDHash, want[:]) != 1 &&] -> TestAssertionRefusals
- oa-gh-verified-m2: battery/auth/oauth2.go [} else if email != "" {
		emailVerified = false
	} -> } else if email != "" {
		emailVerified = true
	}] -> TestGitHubPublicEmailUnconfirmedIsUnverified
SURVIVED pk-reg-owner-m3: battery/auth/passkey.go [purposePasskeyRegisterQuiet)
	if err != nil || owner != sess -> purposePasskeyRegisterQuiet)
	if err != nil || (owner != ses] -> TestPasskeyRegisterOthersChallenge
not failing: TestPasskeyRegisterOthersChallenge
ok  	github.com/DonaldMurillo/gofastr/battery/auth	0.021s


## admin, queue, semantic, webhook



```
battery/admin/account.go: invalid name a 500 -> FAIL TestAccountNameNamesTheShell
battery/admin/account.go: name change not audited -> FAIL TestAccountNameChangeIsAudited
battery/admin/account.go: name post keyed by the audit label -> FAIL TestAccountNameNeedsUserID
battery/admin/account.go: name post reads user_id -> FAIL TestAccountNameNamesTheShell
battery/admin/account.go: password lookup keyed by the audit label -> FAIL TestAccountPageNeedsUserID
battery/admin/activity.go: actor email not used -> FAIL TestAuditPageNamesTheActor
battery/admin/activity.go: bulk runs get the generic line -> FAIL TestRecentActivityNamesBulkRuns
battery/admin/activity.go: no actor reads blank -> FAIL TestRecentActivityReadsAsSentences,TestRecentActivityNamesDeletedRecord
battery/admin/admin.go: Authorize lifts a Decider deny -> FAIL TestGateHonorsADeciderDeny
battery/admin/admin.go: Bind after resume -> FAIL TestBulkResumeExpiredEnqueuedJob,TestBulkInitResumesOrphanJob
battery/admin/admin.go: Init without a UI host -> FAIL TestInitNeedsAUIHost
battery/admin/admin.go: POST counted safe -> FAIL TestGateRefusesCrossSitePosts
battery/admin/admin.go: a nil user is admitted -> FAIL TestGateRefusesASeededNilUser
battery/admin/admin.go: admitJob does not elevate -> FAIL TestBulkRunAdminIsElevated
battery/admin/admin.go: admitJob puts no Policy on the job ctx -> FAIL TestAdmitJobPutsThePolicyOnABareContext
battery/admin/admin.go: admitJob replaces a held policy -> FAIL TestAdmitJobPutsThePolicyOnABareContext
battery/admin/admin.go: an unknown ?result= is printed -> FAIL TestResultNoticeIgnoresUnknownNames
battery/admin/admin.go: askPolicy logs the panic value -> FAIL TestPanickingPolicyLogsTypeNotValue
battery/admin/admin.go: askPolicy recover answers true -> FAIL TestPanickingAuthorizeRefuses,TestPanickingPageAccessHidesOnlyThatPage
battery/admin/admin.go: body cap raised -> FAIL TestOpsPostBodyIsCapped
battery/admin/admin.go: bulk jobs with no UI -> FAIL TestFeatureConfigRefusals
battery/admin/admin.go: exposed entity without a UI -> FAIL TestEntitiesNeedTheUI
battery/admin/admin.go: gate skips the cross-site refusal -> FAIL TestGateRefusesCrossSitePosts
battery/admin/admin.go: headers drop no-store -> FAIL TestGateHeadersOnEveryAnswer
battery/admin/admin.go: headers drop the security headers -> FAIL TestGateHeadersOnEveryAnswer
battery/admin/admin.go: jobsUI uses the app write path only -> FAIL TestBulkQueuedRunsOnCRUDOffEntity
battery/admin/admin.go: loginNext drops the query -> FAIL TestGateLoginRedirectKeepsQuery
battery/admin/admin.go: protocol-relative link accepted -> FAIL TestLinksMustBeSameOrigin
battery/admin/admin.go: queue figure not counted against the strip -> FAIL TestQueueTakesOneMetricSlot
battery/admin/admin.go: queued run over an app-only entity has no write path -> FAIL TestBulkQueuedRunsOnAppOnlyEntity
battery/admin/admin.go: readOps decodes loosely -> FAIL TestOpsRPCRefusesAmbiguousKeys
battery/admin/admin.go: readOps takes a non-string JSON value -> FAIL TestGrantRefusesBadInput
battery/admin/admin.go: reserved page path accepted -> FAIL TestPageRefusesTheAdminsOwnPaths
battery/admin/admin.go: reserved prefix ignores PathPrefix -> FAIL TestReservedEmbedPrefixFollowsPathPrefix
battery/admin/admin.go: saved views with no db -> FAIL TestFeatureConfigRefusals
battery/admin/admin.go: token scope check skipped -> FAIL TestScopedTokenRefusedAdmin
battery/admin/attention.go: an empty watched view draws -> FAIL TestAttentionCountsFilterFuncView,TestAttentionClearUnderArabicDigits,TestAttentionPreviewsWatchedViews
battery/admin/attention.go: default watch rows -> FAIL TestAttentionPreviewsWatchedViews
battery/admin/attention.go: undeclared watched view accepted -> FAIL TestAttentionPreviewsWatchedViews
battery/admin/attention.go: watch rows uncapped -> FAIL TestAttentionPreviewsWatchedViews
battery/admin/audit.go: actor length uncapped -> FAIL TestAuditFilterIgnoresBadValues
battery/admin/audit.go: actor with control bytes accepted -> FAIL TestAuditFilterIgnoresBadValues
battery/admin/audit.go: audit table quoted without folding -> FAIL TestMixedCaseAuditTable
battery/admin/audit.go: audit titles read unelevated -> FAIL TestAuditNamesARecordOnlyTheElevationReads
battery/admin/audit.go: bad day not warned -> FAIL TestAuditFilterIgnoresBadValues
battery/admin/audit.go: big integers decoded as float64 -> FAIL TestAuditChangesKeepBigIntegers
battery/admin/audit.go: bulk delete worded as restore -> FAIL TestAuditNamesBulkRuns
battery/admin/audit.go: digest id read as a record -> FAIL TestAuditDigestIDLinksNothing
battery/admin/audit.go: failed audit write not logged -> FAIL TestAuditWriteFailureIsLogged
battery/admin/audit.go: grant worded as revoke -> FAIL TestAuditNamesAccessChanges
battery/admin/audit.go: hook-hidden record keeps its sides -> FAIL TestAuditPurgedRecordFollowsHooks
battery/admin/audit.go: limit uncapped -> FAIL TestParseLimitIsCapped
battery/admin/audit.go: load failure prints driver text -> FAIL TestAuditLoadFailureIsLogged
battery/admin/audit.go: no change cache -> FAIL TestAuditChangesAskHooksOncePerRealRecord
battery/admin/audit.go: no id tie break -> FAIL TestAuditPagesBreakTimeTiesByID
battery/admin/audit.go: no-db audit not logged -> FAIL TestAuditWithNoDatabaseIsLogged
battery/admin/audit.go: owner scope ignored -> FAIL TestAuditHidesOtherOwnersRecords
battery/admin/audit.go: page past the end not clamped -> FAIL TestAuditPagesOlderRows
battery/admin/audit.go: tenant not scoped -> FAIL TestAuditStaysInsideTheTenant
battery/admin/audit.go: the audit page forgets from -> FAIL TestUndecodablePairWarns
battery/admin/audit.go: to is not the whole day -> FAIL TestAuditFilterByDateRange
battery/admin/audit.go: transitions not folded -> FAIL TestAuditOpKindFoldsKeyedOps
battery/admin/audit.go: unexposed entity accepted -> FAIL TestAuditFilterIgnoresBadValues
battery/admin/audit.go: unknown op accepted -> FAIL TestAuditFilterIgnoresBadValues
battery/admin/csrf.go: cross-site read from Sec-Fetch-Site alone -> FAIL TestGateRefusesEveryForgeableCrossSiteShape
battery/admin/csrf.go: cross-site refusal keyed on the urlencoded content type alone -> FAIL TestGateRefusesEveryForgeableCrossSiteShape
battery/admin/dashboard.go: New without read -> FAIL TestPaletteHonorsCreateGate,TestNewLinkNeedsReadAndCreateEverywhere
battery/admin/dashboard.go: late count not probed -> FAIL TestCountFallsBackToBoundedRead
battery/admin/dashboard.go: recent card without change cache -> FAIL TestAuditChangesAskHooksOncePerRealRecord
battery/admin/emails.go: Render runs elevated -> FAIL TestEmailRenderRunsUnelevated
battery/admin/emails.go: a panicking Render crashes -> FAIL TestEmailRenderErrorStaysGeneric
battery/admin/emails.go: duplicate preview name accepted -> FAIL TestEmailPreviewsConfigRefused
battery/admin/emails.go: frame not sandboxed -> FAIL TestEmailFrameIsSandboxed
battery/admin/emails.go: log carries the error text -> FAIL TestEmailRenderFailureLogsTypeNotText
battery/admin/emails.go: render error shown -> FAIL TestEmailRenderErrorStaysGeneric
battery/admin/emails.go: unknown frame name serves the first -> FAIL TestEmailFrameUnknownName404
battery/admin/entities.go: AllEntities exposes CRUD-off -> FAIL TestAllEntitiesSkipsCRUDOff,TestRawTableNotServed
battery/admin/entities.go: count at manyRows exact -> FAIL TestCountTextStopsBeingExactAtManyRows
battery/admin/entities.go: count fragment unelevated -> FAIL TestEntityElevationLiftsAccess
battery/admin/entities.go: create not a drawer over a parent record -> FAIL TestCreateOpensOverRelatedRecord
battery/admin/entities.go: crumb id read from the path -> FAIL TestCompositeCrumbNamesSlashKey
battery/admin/entities.go: every entity exposed by default -> FAIL TestEntitiesNoneExposedByDefault
battery/admin/entities.go: keyless entity exposed -> FAIL TestRawTableNotServed
battery/admin/entities.go: override unelevated -> FAIL TestAdminOverrideNeedsCapability
battery/admin/entities.go: refused count reads 0 -> FAIL TestEntityCountReadsAndFormats,TestCountFallsBackToBoundedRead
battery/admin/entities.go: saved views route unelevated -> FAIL TestAdminSavesAViewOfALockedEntity
battery/admin/entities.go: unknown entity name skipped -> FAIL TestEntitiesUnknownNameFailsBoot
battery/admin/entities.go: unsplittable composite id linked -> FAIL TestCompositeRecordHref
battery/admin/gate.go: a creator the gate refuses still runs elevated -> FAIL TestBulkRunUsesCurrentRoles
battery/admin/gate.go: a signed-in non-admin gets 401 -> FAIL TestGateRefusesNonAdmin
battery/admin/gate.go: elevation lifts nothing -> FAIL TestElevationStopsAtExposedEntities,TestEntityElevationLiftsAccess
battery/admin/gate.go: elevation reaches an unexposed entity -> FAIL TestElevationStopsAtExposedEntities
battery/admin/gate.go: embed grant and token-scope refusals both off -> FAIL TestGateRefusesAnEmbedGrant
battery/admin/gate.go: no login redirect -> FAIL TestGateSendsSignedOutToLogin
battery/admin/jobs.go: Enqueue before Bind enqueues -> FAIL TestBulkJobsUnboundRefusals
battery/admin/jobs.go: NewBulkJobs accepts a nil principal -> FAIL TestBulkJobsUnboundRefusals
battery/admin/jobs.go: Principal skips the gate -> FAIL TestBulkRunAdminIsElevated
battery/admin/jobs.go: lease retry not delayed -> FAIL TestBulkLeaseRetriesAreDelayed
battery/admin/jobs.go: non-member runs -> FAIL TestAuthPrincipalChecksTenantMember
battery/admin/jobs.go: payload decoded loosely -> FAIL TestBulkJobPayloadWithAnUnknownFieldRunsNothing
battery/admin/jobs.go: principal roles not on the access context -> FAIL TestAuthPrincipalBuildsCurrentContext
battery/admin/jobs.go: queued job carries no UserID -> FAIL TestBulkLeaseRetriesAreDelayed
battery/admin/jobs.go: rescheduled job loses its UserID -> FAIL TestBulkLeaseRetriesAreDelayed
battery/admin/jobs.go: resume grace zero -> FAIL TestBulkInitLeavesAYoungUnenqueuedJob
battery/admin/jobs.go: tenant run with nil member -> FAIL TestAuthPrincipalChecksTenantMember
battery/admin/metrics.go: tenant sees failed jobs figure -> FAIL TestTenantAdminNavOmitsPlatformPages,TestTenantAttentionOmitsFailedJobs
battery/admin/metrics.go: top-level tone accepted -> FAIL TestMetricsAreOneStrip
battery/admin/metrics.go: uncomputable stat accepted -> FAIL TestMetricsRefuseWhatCannotCompute
battery/admin/pages.go: Access asked elevated -> FAIL TestPageAccessIsNotElevated
battery/admin/pages.go: Build runs elevated -> FAIL TestPageBuildIsNotElevated
battery/admin/pages.go: Part skips the page Access -> FAIL TestPagePartIsServedAloneBehindTheSameGates
battery/admin/pages.go: a Build error prints its text -> FAIL TestPageBuildFailureIsContained
battery/admin/pages.go: a nil component is drawn -> FAIL TestPageBuildFailureIsContained
battery/admin/pages.go: a panicking Build crashes the page -> FAIL TestPageBuildFailureIsContained
battery/admin/pages.go: a panicking TabTitle gives no title -> FAIL TestPageTabTitleFallsBackInTheCallersLanguage
battery/admin/pages.go: a sub-path of an own path is not reserved -> FAIL TestPageRefusesTheAdminsOwnPaths
battery/admin/pages.go: an empty TabTitle is kept -> FAIL TestPageTabTitleFallsBackInTheCallersLanguage
battery/admin/pages.go: an underscore path is not reserved -> FAIL TestPageRefusesTheAdminsOwnPaths
battery/admin/pages.go: an unpolled card is served -> FAIL TestCardsDrawAndPoll
battery/admin/pages.go: page Access not a screen policy -> FAIL TestPageAccessRefuses
battery/admin/palette.go: palette answer uncapped -> FAIL TestPaletteAnswerIsCapped
battery/admin/palette.go: palette body uncapped -> FAIL TestPaletteCapsTheQueryAndBody
battery/admin/palette.go: palette query uncapped -> FAIL TestPaletteCapsTheQueryAndBody
battery/admin/palette.go: records searched unelevated -> FAIL TestPaletteRecordSearchIsElevated
battery/admin/palette.go: records searched without the gate -> FAIL TestPaletteRecordsNeedTheGate
battery/admin/palette.go: refused page listed -> FAIL TestPaletteHidesRefusedPages
battery/admin/palette.go: the search page drops the warning -> FAIL TestUndecodablePairWarns
battery/admin/platform.go: a tenant caller counts as the platform operator -> FAIL TestTenantAdminRefusedPlatformPages,TestTenantAdminNavOmitsPlatformPages,TestTenantAttentionOmitsFailedJobs
battery/admin/platform.go: platformOnly lets a tenant caller post -> FAIL TestTenantAdminRefusedPlatformPages
battery/admin/platform.go: platformPolicy lets a tenant caller read -> FAIL TestTenantAdminRefusedPlatformPages
battery/admin/process_modules.go: a refused lever is not audited -> FAIL TestModulesLeversNeedManage
battery/admin/process_modules.go: an empty module name runs the lever -> FAIL TestModulesLeverRefusesBadInput
battery/admin/process_modules.go: lever error text shown -> FAIL TestModulesLeverErrorHidesItsText
battery/admin/process_modules.go: levers need no permission -> FAIL TestModulesLeversNeedManage,TestModulesLeversRefusedWithoutPolicy
battery/admin/process_modules.go: revoke with no grant runs -> FAIL TestModulesLeverRefusesBadInput
battery/admin/query_pairs.go: a narrowing param with an undecodable value does not warn -> FAIL TestUndecodablePairWarns
battery/admin/query_pairs.go: an undecodable key does not warn -> FAIL TestUndecodablePairWarns
battery/admin/query_pairs.go: an undecodable non-narrowing pair warns -> FAIL TestUndecodablePairWarns
battery/admin/queue.go: a page that adds no new failed id keeps the read going -> FAIL TestQueueReplayAllStopsOnARepeat (test timed out at -timeout=30s)
battery/admin/queue.go: a plain Replayable reads as not replayed -> FAIL TestQueueReplayAuditsTheActor,TestQueueReplayRPCAnswersAToast,TestQueueReplayAllAuditsEachJob,TestSingleReplayNeverScansTheFailedList
battery/admin/queue.go: held key replay is a 500 -> FAIL TestReplayHeldKeyIsConflict
battery/admin/queue.go: list failure prints driver text -> FAIL TestQueueLoadFailureHidesDriverText
battery/admin/queue.go: page not clamped -> FAIL TestQueuePagesOlderJobs
battery/admin/queue.go: partial stats kept on error -> FAIL TestQueueStatsErrorDropsCounts
battery/admin/queue.go: replay all audits a job someone else replayed -> FAIL TestReplayAllAuditsOnlyWhatItQueued
battery/admin/queue.go: replay all stops on a held key -> FAIL TestReplayAllSkipsHeldKey
battery/admin/queue.go: replay all unbounded -> FAIL TestQueueReplayAllIsBounded
battery/admin/queue.go: the jobs page drops the undecodable warning -> FAIL TestUndecodablePairWarns
battery/admin/queue.go: unknown status not warned -> FAIL TestUnknownFilterValueWarns
battery/admin/queue.go: unknown status passed to ListJobs -> FAIL TestQueueStatusIsOneOfTheKnownSet,TestUnknownFilterValueWarns
battery/admin/queue.go: unstorable replay id accepted -> FAIL TestQueueReplayRefusesUnstorableID
battery/admin/rbac_admin.go: decider deny ignored -> FAIL TestPermissionCheckAsksDecider
battery/admin/rbac_admin.go: grant beyond the caller tier -> FAIL TestGrantRevokeRequireCallerTier
battery/admin/rbac_admin.go: grants not prechecked -> FAIL TestGrantUnderWildcardRevokeIsRefused
battery/admin/rbac_admin.go: grid size unbounded -> FAIL TestPermissionsSaveRefusesBadInput
battery/admin/rbac_admin.go: held roles read from the context -> FAIL TestPermissionCheckAsksDecider,TestAssignRefusesOwnAdminRemoval,TestRevokeRefusesOwnLastPermission,TestGrantUpdatesPolicyAndAudits,TestPermissionsSaveAppliesChangedBoxes,TestGrantStrictTypoIsRefused,TestGrantUnderWildcardRevokeIsRefused,TestGrantRevokeRequireCallerTier,TestAssignSetsRoles,TestAssignRequiresCallerTier,TestAssignRefusesStrippingHigherRoles,TestAssignRefusesPermissionlessRole
battery/admin/rbac_admin.go: id-less admin audited with no actor -> FAIL TestIDlessAdminIsAuditedAsUnknown
battery/admin/rbac_admin.go: own admin role removable -> FAIL TestAssignRefusesOwnAdminRemoval
battery/admin/rbac_admin.go: own last permission revoked -> FAIL TestRevokeRefusesOwnLastPermission
battery/admin/rbac_admin.go: permissionless role assignable -> FAIL TestAssignRefusesPermissionlessRole
battery/admin/rbac_admin.go: refused grant not audited -> FAIL TestGrantRevokeRequireCallerTier
battery/admin/rbac_admin.go: stripped roles not tier-checked -> FAIL TestAssignRefusesStrippingHigherRoles
battery/admin/rbac_admin.go: unknown capability a 500 -> FAIL TestGrantStrictTypoIsRefused
battery/admin/rbac_admin.go: unstorable user id accepted -> FAIL TestAssignRefusesUnstorableID
battery/admin/rbac_admin.go: user id trimmed -> FAIL TestAssignKeepsPaddedUserID
battery/admin/rbac_admin.go: users limit uncapped -> FAIL TestUsersLimitIsBounded
battery/admin/rbac_admin.go: users page unbounded -> FAIL TestUsersHugePageOffsetStays
battery/admin/rbac_admin.go: wildcard does not cover -> FAIL TestAssignRefusesOwnAdminRemoval,TestAssignSetsRoles,TestAssignRefusesStrippingHigherRoles,TestAssignRefusesPermissionlessRole
battery/admin/rbac_admin.go: wildcard holder refused own revoke -> FAIL TestWildcardHolderMayRevokeAnyPermission
battery/admin/rbac_admin.go: wildcard revoke a 500 -> FAIL TestGrantUnderWildcardRevokeIsRefused
battery/admin/rbac_admin.go: wildcard role edited by a save -> FAIL TestPermissionsSaveAppliesChangedBoxes
battery/admin/saved_views.go: Get ignores the owner -> FAIL TestAdminSavedViewsPerAdmin,TestSavedViewsOwnerIsolation
battery/admin/saved_views.go: any saved views table name accepted -> FAIL TestFeatureConfigRefusals
battery/admin/saved_views.go: cap off by one -> FAIL TestSavedViewsCapHoldsUnderConcurrency,TestSavedViewsCapPerOwnerEntity
battery/admin/saved_views.go: empty user id accepted -> FAIL TestSavedViewsRefuseAUserWithNoID
battery/admin/saved_views.go: erasure keyed on tenant -> FAIL TestEraseRemovesSavedViews
battery/admin/saved_views.go: lock key parts not length-prefixed -> FAIL TestSavedViewLockKeysDoNotAlias
battery/admin/saved_views.go: name length uncapped -> FAIL TestSavedViewsContractLimits
battery/admin/saved_views.go: no advisory lock on Postgres -> FAIL TestSavedViewsCapHoldsUnderConcurrency
battery/admin/saved_views.go: tenant ignored -> FAIL TestSavedViewsTenantIsolation
battery/admin/shell.go: account menu drawn with no user -> FAIL TestAccountMenuWithNoUser
battery/admin/shell.go: crumb title unelevated -> FAIL TestNavCountAndCrumbReadElevated
battery/admin/shell.go: drawer draws counts -> FAIL TestNavRowsCountRecords
battery/admin/shell.go: drawer offered off admin pages -> FAIL TestCatalogHidesAdminWidgets
battery/admin/shell.go: nav count drawn without the gate -> FAIL TestNavRowsCountRecords
battery/admin/shell.go: nav count unelevated -> FAIL TestNavCountAndCrumbReadElevated
battery/admin/shell.go: screen elevates without the gate -> FAIL TestScreenRechecksTheGate
battery/admin/shell.go: screen login redirect drops next -> FAIL TestGateLoginRedirectKeepsQuery,TestGateSendsSignedOutToLogin
battery/admin/shell.go: screen policy admits everyone -> FAIL TestGateLoginRedirectKeepsQuery,TestGateRefusesAnonymous,TestGateRefusesNonAdmin,TestGateHonorsAdminRole,TestGateSendsSignedOutToLogin
battery/admin/shell.go: screen renders without the gate -> FAIL TestScreenRechecksTheGate
battery/admin/shell.go: title loaded without the gate -> FAIL TestScreenRechecksTheGate
battery/admin/shell.go: widgets listed to everyone -> FAIL TestCatalogHidesAdminWidgets
battery/queue/db.go: Close of an unstarted queue blocks -> FAIL TestCloseWithoutStartReturns,TestCloseWithoutStartIsIdempotent
battery/queue/db.go: ErrPermanent retried on DBQueue -> FAIL TestPermanentDeadLettersAtOnce,TestPermanentWrapperIsPermanent
battery/queue/db.go: MaxAge ignored on DBQueue -> FAIL TestMaxAgeAppliesWhenLeaseExpires,TestMaxAgeGivesUp,TestMaxAgeBoundsRetryAfter
battery/queue/db.go: RetryAfter ignored on DBQueue -> FAIL TestRetryAfterSetsNextAttempt,TestRetryAfterNegativeIsNow
battery/queue/db.go: deferred BEGIN on the SQLite claim -> FAIL TestSQLiteDequeueConcurrentNoBusy
battery/queue/db.go: gate not rechecked after the claim -> FAIL TestGateReleaseSurvivesShutdown
battery/queue/db.go: gate release spends an attempt -> FAIL TestGateReleaseSurvivesShutdown
battery/queue/db.go: handlerless job acked away -> FAIL TestNoHandlerJobIsDeadLettered
battery/queue/db.go: last error keeps control bytes -> FAIL TestDBListJobsCarriesStatusUpdatedAndLastError
battery/queue/db.go: last error uncapped -> FAIL TestDBListJobsCarriesStatusUpdatedAndLastError
battery/queue/db.go: lease within the handler budget accepted -> FAIL TestHandlerBudgetMustFitInsideLease
battery/queue/db.go: list pages without the id tiebreak -> FAIL TestDBListJobsBreaksTimeTiesByID
battery/queue/db.go: replay resurrects a live job -> FAIL TestReplayJobReportsWhetherItQueued
battery/queue/db.go: shutdown-cut handler spends the attempt -> FAIL TestShutdownMidHandlerRefunds
battery/queue/db.go: stale ack deletes the live claim -> FAIL TestDBStaleClaimCannotSettleTheNewOne
battery/queue/db.go: stale nack fenced neither at the read nor at the write -> FAIL TestDBStaleClaimCannotSettleTheNewOne
battery/queue/db.go: unstarted claim run on a cancelled ctx -> FAIL TestShutdownBeforeStartRefundsClaim
battery/queue/durable_scheduler.go: durable sub-millisecond interval registered -> FAIL TestDurableIntervalFloor
battery/queue/durable_scheduler_catchup.go: Local zone persisted -> FAIL TestTZNamePersistsOnlyLoadableZones
battery/queue/durable_scheduler_catchup.go: unloadable zone persisted -> FAIL TestTZNamePersistsOnlyLoadableZones
battery/queue/invoke.go: handler panic escapes -> FAIL TestDBWorkerSurvivesHandlerPanic
battery/queue/invoke.go: span carries the error message -> FAIL TestMemoryJobSpans
battery/queue/memory.go: memory Nack ignores MaxAge -> FAIL TestMemoryNackMaxAgeGivesUp
battery/queue/memory.go: memory accepts a SequenceKey -> FAIL TestSequenceKeyRefusedUnsupported
battery/queue/memory.go: memory gate panic unrecovered -> FAIL TestMemoryQueueGatePanicIsolated
battery/queue/memory.go: memory ignores MaxAge -> FAIL TestMemoryReplayAfterRetryAfterPastMaxAgeIsDue,TestMemoryMaxAgeGivesUp
battery/queue/memory.go: memory ignores RetryAfter -> FAIL TestMemoryReplayAfterRetryAfterPastMaxAgeIsDue,TestMemoryRetryAfterWaits
battery/queue/memory.go: memory retries ErrPermanent -> FAIL TestMemoryPermanentDeadLettersAtOnce
battery/queue/redis.go: reclaim panic escapes -> FAIL TestRedisReclaimClientPanicContained
battery/queue/redis.go: reclaim takes live claims -> FAIL TestRedisNonPositiveVisibilityKeepsTimeout,TestRedisReclaimRedeliversExpired
battery/queue/redis.go: redis accepts a SequenceKey -> FAIL TestSequenceKeyRefusedUnsupported
battery/queue/redis.go: redis accepts a UniqueKey -> FAIL TestRedisRefusesUniqueKey
battery/queue/redis.go: redis stale claim owns the job -> FAIL TestRedisStaleAckCannotDeleteReclaimant,TestRedisStaleNackCannotTouchReclaimant
battery/queue/scheduler.go: sub-millisecond interval registered -> FAIL TestSchedulerRefusesSubMillisecondInterval
battery/semantic/chunker.go: invalid UTF-8 chunked -> FAIL TestFixedWindowRejectsInvalidUTF8Offsets
battery/semantic/chunker.go: quadratic offsets -> FAIL TestChunkLinearTime
battery/semantic/chunker.go: zero window chunked -> FAIL TestFixedWindowInvalidConfigReturnsError
battery/semantic/chunker_lang.go: defaults written on the shared chunker (-race) -> FAIL TestLangAwareDefaultConfigurationConcurrent
battery/semantic/chunker_lang.go: fallback chunks not capped -> FAIL TestFallbackPathHonorsMaxRunes
battery/semantic/chunker_lang.go: go chunks not capped -> FAIL TestLangAwareReChunksOverlongStructuralChunks
battery/semantic/embedder_ollama.go: empty embedding accepted -> FAIL TestOllamaRejectsEmptyEmbeddings
battery/semantic/embedder_ollama.go: inconsistent dims accepted -> FAIL TestOllamaRejectsInconsistentDimensions
battery/semantic/embedder_ollama.go: oversized response accepted -> FAIL TestOllamaResponseCapDetectsExtraBytes
battery/semantic/hybrid.go: fused ties unordered -> FAIL TestFuseKeepsTheVectorHitAndOrdersTies
battery/semantic/hybrid.go: keyword copy replaces the vector hit -> FAIL TestFuseKeepsTheVectorHitAndOrdersTies
battery/semantic/index.go: MMR lambda above 1 -> FAIL TestQueryRejectsInvalidMMRLambda
battery/semantic/index.go: WAL record names no model -> FAIL TestWALReplayRefusesOtherModel
battery/semantic/index.go: broken WAL keeps refusing -> FAIL TestWALTornAppendKeepsRestart
battery/semantic/index.go: candidate width unclamped -> FAIL TestQueryClampsK
battery/semantic/index.go: duplicate ids both stored -> FAIL TestLastDuplicateWinsFlat,TestAddValidatesOnlyLastDocumentPerID
battery/semantic/index.go: half-applied write not finished first -> FAIL TestHalfAppliedReplaceHealsBeforeNextWrite
battery/semantic/index.go: keyword terms unbounded -> FAIL TestHybridKeywordQueryTermsBounded
battery/semantic/index.go: logged replace applied on a cancelable ctx -> FAIL TestCancelMidReplaceStaysConsistent
battery/semantic/index.go: no dir sync before WAL cut -> FAIL TestSnapshotSyncsDirBeforeWALCut
battery/semantic/index.go: pgvector replace not atomic -> FAIL TestPgVectorRefusesAnotherModel,TestPgReplaceSerializesReplicas
battery/semantic/index.go: query panic escapes -> FAIL TestQueryRecoversBackendPanics
battery/semantic/index.go: read-only registers an eraser -> FAIL TestReadOnlyIndexRegistersNoEraser
battery/semantic/index.go: rerank sees only K -> FAIL TestRerankCanChangeTopOne
battery/semantic/index.go: rerank without reranker -> FAIL TestQueryCallerFaultsAnswer400
battery/semantic/index.go: shared store index registers an eraser -> FAIL TestIndexSkipsEraserItCannotRun
battery/semantic/index.go: snapshot over a half-applied write -> FAIL TestHalfAppliedReplaceHealsOnSnapshot,TestHalfAppliedReplaceKeepsWAL
battery/semantic/index.go: unnormalized query in MMR -> FAIL TestMMRIgnoresQueryVectorLength
battery/semantic/index.go: vectors leak out of Query -> FAIL TestRoundTrip
battery/semantic/mmr.go: keyword-only hits lose relevance -> FAIL TestMMRKeepsAKeywordOnlyHitsScore
battery/semantic/mmr.go: negative similarity floored at zero -> FAIL TestMMRNegativeSimilarityRanks,TestMMRAccountsForNegativeSimilarity
battery/semantic/mmr.go: relevance not weighed by lambda -> FAIL TestMMRNegativeSimilarityRanks,TestMMRIgnoresQueryVectorLength,TestMMRFullDiversityPrefersDistinct,TestMMRAccountsForNegativeSimilarity
battery/semantic/model_pgvector.go: model keyed by the unfolded name -> FAIL TestPgVectorModelFoldsTableName
battery/semantic/model_pgvector.go: table of another model rebound -> FAIL TestPgVectorRefusesAnotherModel,TestPgVectorRefusesAnotherDim,TestPgVectorModelFoldsTableName
battery/semantic/persist.go: WAL perms kept loose -> FAIL TestOpenWALTightensExistingFile
battery/semantic/persist.go: chunk of another dim accepted -> FAIL TestSnapshotRejectsVectorDimensionMismatch
battery/semantic/persist.go: empty vectors accepted -> FAIL TestSnapshotOfEmptyVectorsIsRefused
battery/semantic/persist.go: second writer error untyped -> FAIL TestSecondWriterRefused
battery/semantic/persist.go: snapshot of another dim loads -> FAIL TestLoadSnapshotRefusesModelMismatch
battery/semantic/persist.go: snapshot of another model loads -> FAIL TestLoadSnapshotRefusesAnotherModelOfTheSameDim
battery/semantic/persist.go: torn append left in the file -> FAIL TestWALFailedAppendCutsAndStops
battery/semantic/persist.go: truncated chunk list accepted -> FAIL TestSnapshotRejectsTruncatedChunkList
battery/semantic/routes.go: answers cacheable -> FAIL TestHandlerAnswersAreNoStore
battery/semantic/routes.go: any token accepted -> FAIL TestHandler_WrongBearerTokenRejected
battery/semantic/routes.go: chunked body uncapped -> FAIL TestHTTPIndex_RejectsOversizedChunkedBody
battery/semantic/routes.go: index body decoded loosely -> FAIL TestIndexRejectsDuplicateJSONKeys,TestIndexRejectsFoldedJSONKeys
battery/semantic/routes.go: index body decoded loosely -> FAIL TestRejectsDuplicateAndFoldedJSONKeys
battery/semantic/routes.go: no-token mount open to an empty bearer -> FAIL TestHandler_EmptyBearerWithNoTokenRefused
battery/semantic/routes.go: non-JSON accepted -> FAIL TestHTTPIndex_RejectsTextPlainJSON,TestHTTPIndex_RejectsMissingContentType,TestHTTPQuery_RejectsTextPlainJSON,TestHTTPQuery_RejectsMissingContentType
battery/semantic/routes.go: query body decoded loosely -> FAIL TestQueryRejectsDuplicateJSONKeys,TestQueryRejectsFoldedJSONKeys
battery/semantic/routes.go: query body decoded loosely -> FAIL TestRejectsDuplicateAndFoldedJSONKeys
battery/semantic/routes.go: routes unauthenticated -> FAIL TestHTTPStats_RequiresAuthentication,TestHTTPDelete_RequiresAuthentication
battery/semantic/routes.go: unstorable delete id reaches the index -> FAIL TestHTTPDelete_UnstorableIDNeverReachesIndex
battery/semantic/semantic.go: dim not seeded -> FAIL TestAddDoesNotCallStatsPerWrite
battery/semantic/semantic.go: failed open keeps the lock -> FAIL TestFailedOpenReleasesLock
battery/semantic/semantic.go: keyword without chunk lister -> FAIL TestOpen_FailsClosedOnKeywordWithoutChunkLister
battery/semantic/semantic.go: memory keyword over shared store -> FAIL TestOpenRefusesLocalKeywordOnSharedStore
battery/semantic/semantic.go: path without snapshotter -> FAIL TestOpen_FailsClosedOnPathWithoutSnapshotter
battery/semantic/semantic.go: read-only without path -> FAIL TestReadOnlyNeedsPath
battery/semantic/semantic.go: zero snapshotEvery kept -> FAIL TestZeroSnapshotEveryUsesDefault
battery/semantic/storable.go: huge exponent accepted -> FAIL TestAddRefusesNumbersJSONBCannotHold
battery/semantic/storable.go: lone surrogate accepted -> FAIL TestAddMetadataMatchesStoredJSON
battery/semantic/storable.go: too many fraction digits accepted -> FAIL TestAddRefusesNumbersJSONBCannotHold
battery/semantic/storable.go: too many integer digits accepted -> FAIL TestIndexRouteRefusesOutOfRangeNumber,TestAddRefusesNumbersJSONBCannotHold
battery/semantic/storable.go: unstorable document text -> FAIL TestIndexRouteRefusesUnstorableText,TestAddRefusesUnstorableText
battery/semantic/store_flat.go: empty vector stored -> FAIL TestEmptyVectorIsRefused
battery/semantic/store_flat.go: metadata shared with the caller -> FAIL TestFlatStoreOwnsVectorsAndMetadata,TestFlatStoreDeepCopiesMetadata
battery/semantic/store_flat.go: mixed dims stored -> FAIL TestFlatAddFailureLeavesStoreUnchanged
battery/semantic/store_flat.go: non-finite vector stored -> FAIL TestFlatStoreRejectsNonFiniteVectors
battery/semantic/store_pgvector.go: case-kept table accepted -> FAIL TestPgVectorCaseKeptTableRefused
battery/semantic/store_pgvector.go: replace takes no document lock -> FAIL TestPgReplaceSerializesReplicas
battery/semantic/store_pgvector.go: source filter interpolated -> FAIL TestPgVectorFilterNoInjection,TestPgVectorFilterSource
battery/semantic/store_pgvector.go: write of another model accepted -> FAIL TestPgVectorRefusesAnotherModel
battery/semantic/watcher.go: FIFO open blocks -> FAIL TestReadCappedSkipsSwappedFIFO
battery/semantic/watcher.go: cap overflows at MaxInt64 -> FAIL TestReadLimitedAtMaxInt64ReadsTheFile
battery/semantic/watcher.go: deleted file kept known -> FAIL TestWatcherRetriesFailedRemove
battery/semantic/watcher.go: file past the cap indexed -> FAIL TestReadLimitedAtMaxInt64ReadsTheFile,TestReadLimitedStopsPastTheCap,TestWatcherReadCapsAtMaxFileSize,TestWatcherDropsFileGrownPastMax
battery/semantic/watcher.go: no reconcile on first scan -> FAIL TestWatcherRestartDropsDeletedFiles
battery/semantic/watcher.go: non-regular file read -> FAIL TestReadCappedSkipsSwappedFIFO
battery/semantic/watcher.go: paths outside the root count as under it -> FAIL TestUnderRootRefusesSiblingPrefix,TestWatcherRestartDropsDeletedFiles
battery/webhook/auth.go: api key on a reserved header -> FAIL TestSubscribeRefusesMalformedAuth
battery/webhook/auth.go: basic username with colon -> FAIL TestSubscribeRefusesMalformedAuth
battery/webhook/auth.go: control byte in credential -> FAIL TestSubscribeRefusesMalformedAuth
battery/webhook/auth.go: corrupt auth column reads as none -> FAIL TestSQLStoreCorruptColumnsAreErrors
battery/webhook/auth.go: credential without kind -> FAIL TestSubscribeRefusesMalformedAuth
battery/webhook/bridge.go: owner read after redaction -> FAIL TestBridgeOwnerSurvivesRedactor
battery/webhook/bridge.go: remote event queued again -> FAIL TestBridgeQueuesOncePerEvent
battery/webhook/format.go: body twice -> FAIL TestFormatValidateRefuses
battery/webhook/format.go: format on reserved header -> FAIL TestFormatValidateRefuses
battery/webhook/format.go: format reuses a header -> FAIL TestFormatValidateRefuses
battery/webhook/format.go: signed string without timestamp -> FAIL TestFormatValidateRefuses
battery/webhook/format.go: timestamp nowhere for the receiver -> FAIL TestFormatValidateRefuses
battery/webhook/inbound.go: body uncapped -> FAIL TestIngest_OversizeBody_413
battery/webhook/inbound.go: dedupe key stored before enqueue -> FAIL TestIngest_RedeliveryAfterEnqueueFailure,TestRedeliverySurvivesFailedCleanup
battery/webhook/inbound.go: dedupe registered with a snapshot write -> FAIL TestDedupeKeyRegistersAfterProcessed
battery/webhook/inbound.go: lost failed write not logged -> FAIL TestProcessInbound_FailedStateUpdateLogged
battery/webhook/inbound.go: outcome written on the cancelled ctx -> FAIL TestInboundOutcomeSurvivesCancel
battery/webhook/inbound.go: processed envelope re-run -> FAIL TestProcessedEnvelopeSkipsHandler
battery/webhook/inbound.go: seen key re-persisted -> FAIL TestIngest_Dedupe_SecondGets200,TestIngest_QueueNil_Dedupes
battery/webhook/inbound.go: unadmitted envelope runs -> FAIL TestProcessedEnvelopeSkipsHandler,TestDeletedEnvelopeSkipsHandler
battery/webhook/inbound.go: unlisted headers persisted -> FAIL TestIngest_HappyPath
battery/webhook/inbound.go: unstorable dedupe key -> FAIL TestIngestUnstorableDedupeKeyAndOwnerRefused
battery/webhook/inbound.go: unstorable owner -> FAIL TestIngestUnstorableDedupeKeyAndOwnerRefused
battery/webhook/inbound.go: unverified payload persisted -> FAIL TestIngest_BadSignature_401_NoPersist
battery/webhook/inbound_memory.go: memory processing claim fence dropped -> FAIL TestProcessingClaimSkipsAProcessedEnvelopeOnEveryStore
battery/webhook/inbound_memory.go: memory processing claim on a processed envelope -> FAIL TestProcessedEnvelopeSkipsHandler
battery/webhook/inbound_sql.go: sql processing claim fence dropped -> FAIL TestProcessingClaimSkipsAProcessedEnvelopeOnEveryStore
battery/webhook/inbound_sql.go: sql settle fence dropped -> FAIL TestInboundRedStaleSettleFenced
battery/webhook/memory.go: memory claim fence dropped -> FAIL TestStaleRetryKeepsLiveLease,TestStaleDeadSettleKeepsReplay
battery/webhook/memory.go: memory reap deletes dead rows -> FAIL TestResetAndReapTouchOnlyTheirRowsOnEveryStore
battery/webhook/memory.go: memory release fence dropped -> FAIL TestStaleReleaseKeepsNewClaim
battery/webhook/memory.go: memory reset resurrects a live row -> FAIL TestResetAndReapTouchOnlyTheirRowsOnEveryStore
battery/webhook/memory.go: memory settle fence dropped -> FAIL TestDeadNeverOverwritesSuccess
battery/webhook/memory.go: memory settle writes attempts absolutely -> FAIL TestFallbackTickReachesMaxAttempts
battery/webhook/secret.go: legacy plaintext refused -> FAIL TestAESGCMSecretCodec_DecodeAcceptsLegacyPlaintext
battery/webhook/sign.go: blank secret verifies -> FAIL TestVerifyTimestampedBlankSecretRefuses
battery/webhook/sign.go: duplicate t= accepted -> FAIL TestVerify_RejectsDuplicateFields
battery/webhook/sign.go: duplicate v1= accepted -> FAIL TestVerifyRefusesARepeatedV1EvenWhenBothMatch
battery/webhook/sign.go: newline in event accepted -> FAIL TestVerifyRefusesANewlineInEventOrID
battery/webhook/sign.go: non-positive tolerance accepted -> FAIL TestVerifyTimestamped_RejectsNonPositiveTolerance
battery/webhook/sign.go: stale timestamp accepted -> FAIL TestTimestampedVerifier_RejectsStale,TestVerifyTimestamped_RejectsExtremeTimestamps,TestSignature_ExpiredTimestampRejected,TestSignature_FutureTimestampRejected
battery/webhook/sql.go: corrupt events column reads as no events -> FAIL TestSQLStoreCorruptColumnsAreErrors
battery/webhook/sql.go: deferred BEGIN -> FAIL TestSQLiteClaimConcurrentNoBusy
battery/webhook/sql.go: delivery erasure keyed wrong -> FAIL TestEraseRemovesOwnedDeliveries
battery/webhook/sql.go: negative lease accepted -> FAIL TestClaimLeaseNegativeRejected
battery/webhook/sql.go: old tables not upgraded -> FAIL TestSQLStoreUpgradesOldTables,TestWebhookStoresAddOwnerColumn
battery/webhook/sql.go: plaintext without opt-in -> FAIL TestSQLStore_RefusesPlaintextWithoutOptIn
battery/webhook/sql.go: postgres claim consumes no attempt -> FAIL TestClaimConsumesOneAttemptOnEveryStore
battery/webhook/sql.go: postgres upsert without alias -> FAIL TestSubscribersTableNamedExcluded
battery/webhook/sql.go: sql claim fence dropped -> FAIL TestStaleRetryKeepsLiveLease,TestStaleDeadSettleKeepsReplay
battery/webhook/sql.go: sql reap deletes dead rows -> FAIL TestResetAndReapTouchOnlyTheirRowsOnEveryStore
battery/webhook/sql.go: sql release fence dropped -> FAIL TestStaleReleaseKeepsNewClaim,TestTickReleasesClaimsItCannotRun
battery/webhook/sql.go: sql reset resurrects a live row -> FAIL TestResetAndReapTouchOnlyTheirRowsOnEveryStore
battery/webhook/sql.go: sql settle fence dropped -> FAIL TestDeadNeverOverwritesSuccess
battery/webhook/sql.go: sqlite claim consumes no attempt -> FAIL TestClaimConsumesAttemptCrashLoop
battery/webhook/sql.go: sqlite upsert without alias -> FAIL TestSubscribersTableNamedExcluded
battery/webhook/sql.go: zero lease kept -> FAIL TestClaimLeaseZeroKeepsDefault
battery/webhook/sql.go: zoned due times not rewritten -> FAIL TestSQLiteStoreRewritesZonedDueTimes
battery/webhook/ssrf.go: .internal host accepted -> FAIL TestSSRFRefusesInternalSuffixHosts
battery/webhook/ssrf.go: DNS failure refuses registration -> FAIL TestSSRF_PublicURLAccepted,TestValidateSubscriberURL_AllowsPublicHTTPandHTTPS
battery/webhook/ssrf.go: allowPrivate ignored -> FAIL TestSSRF_AllowPrivateOverrides,TestValidateSubscriberURL_AllowPrivateLetsLoopbackThrough
battery/webhook/ssrf.go: any scheme accepted -> FAIL TestSSRF_UnsupportedSchemesRejected,TestValidateSubscriberURL_RejectsNonHTTPSchemes
battery/webhook/ssrf.go: custom client unguarded -> FAIL TestSSRF_CustomClientStillGuarded,TestSSRF_CustomTransportSettingsSurvive,TestSSRF_CustomRoundTripperGuarded,TestSSRF_PlainTransportDialGuarded,TestSSRF_DefaultCloneDialGuarded
battery/webhook/ssrf.go: subscriber URL not checked -> FAIL TestSSRFRefusesInternalSuffixHosts,TestSSRF_LocalhostRejected,TestSSRF_PrivateIPsRejected,TestSSRF_CloudMetadataRejected,TestSSRF_LinkLocalRejected,TestSSRF_UnsupportedSchemesRejected,TestSSRF_EmptyURLRejected,TestSSRF_MissingHostRejected,TestSSRF_AllowPrivateStillEnforcesScheme,TestValidateSubscriberURL_RejectsLoopbackAndPrivate,TestValidateSubscriberURL_RejectsNonHTTPSchemes,TestValidateSubscriberURL_AllowPrivateLetsLoopbackThrough,TestSubscribe_RejectsSSRFURL,TestSubscribe_RejectsBadInput,TestSSRF_URLWithUserinfoRejected
battery/webhook/ssrf.go: userinfo URL accepted -> FAIL TestSSRF_URLWithUserinfoRejected
battery/webhook/webhook.go: * not a wildcard -> FAIL TestMatch_Patterns
battery/webhook/webhook.go: ** matches one segment only -> FAIL TestMatch_DoubleStarMatchesAnyDepth
battery/webhook/webhook.go: attempt not counted -> FAIL TestWebhookMetrics_CountersAndCollector
battery/webhook/webhook.go: auth on sig header subscribed -> FAIL TestSubscribeRefusesAuthOnSigHeader
battery/webhook/webhook.go: bad format accepted -> FAIL TestNewPanicsOnBadFormat
battery/webhook/webhook.go: bad pattern subscribed -> FAIL TestSubscribe_RejectsCRLFEventPattern,TestSubscribe_RejectsNULEventPattern
battery/webhook/webhook.go: credential client follows redirects -> FAIL TestDeliveryWithAuthRefusesRedirect
battery/webhook/webhook.go: custom client unguarded -> FAIL TestSSRF_CustomClientStillGuarded,TestSSRF_CustomTransportSettingsSurvive,TestSSRF_CustomRoundTripperGuarded,TestSSRF_PlainTransportDialGuarded,TestSSRF_DefaultCloneDialGuarded
battery/webhook/webhook.go: dead overwrites success -> FAIL TestDeadNeverOverwritesSuccess
battery/webhook/webhook.go: delivery owner dropped -> FAIL TestBridgeRecordsEventOwner,TestBridgeOwnerSurvivesRedactor,TestEraseRemovesOwnedDeliveries
battery/webhook/webhook.go: due read error not logged -> FAIL TestTickLogsDueDeliveriesError
battery/webhook/webhook.go: event name refuses C0 only -> FAIL TestEventNameRejectsC1Controls
battery/webhook/webhook.go: fallback consumes no attempt -> FAIL TestFallbackTickReachesMaxAttempts
battery/webhook/webhook.go: gone subscriber retried -> FAIL TestManager_DeadIfSubscriberRemoved
battery/webhook/webhook.go: internal URL subscribed -> FAIL TestSubscribe_RejectsSSRFURL
battery/webhook/webhook.go: invalid json payload -> FAIL TestPublish_RejectsInvalidJSONPayload
battery/webhook/webhook.go: lease raised for non-leased store -> FAIL TestNewRaisesShortLeaseOnlyForLeasedStore
battery/webhook/webhook.go: lease raised only to the timeout -> FAIL TestLeaseOutlivesClientTimeout
battery/webhook/webhook.go: lost settle not logged -> FAIL TestManager_DeliveryUpdateFailureLogged
battery/webhook/webhook.go: negative retention arms a future cutoff -> FAIL TestNegativeRetentionIsOff
battery/webhook/webhook.go: no claim fence -> FAIL TestStaleRetryKeepsLiveLease,TestStaleDeadSettleKeepsReplay
battery/webhook/webhook.go: one extra attempt before dead -> FAIL TestFallbackTickReachesMaxAttempts
battery/webhook/webhook.go: oversized payload -> FAIL TestPublish_RejectsOversizedPayload
battery/webhook/webhook.go: past-budget claim sent -> FAIL TestExpiredClaimPastBudgetIsDead
battery/webhook/webhook.go: paused ignored -> FAIL TestPausedSubscriberSurvivesReadModifySubscribe,TestSubscribe_HonorsPausedFlag
battery/webhook/webhook.go: paused subscriber gets deliveries -> FAIL TestPublish_SkipsPausedSubscribers
battery/webhook/webhook.go: plaintext auth subscribed -> FAIL TestPlaintextAuthRefused
battery/webhook/webhook.go: shutdown POST counted as failure -> FAIL TestStopMidPOSTRefundsTheAttempt
battery/webhook/webhook.go: shutdown keeps unstarted claims -> FAIL TestShutdownTickRefundsUnstarted
battery/webhook/webhook.go: store panic kills the worker -> FAIL TestStorePanicLogsAreScrubbed
battery/webhook/webhook.go: stored auth on sig header sent -> FAIL TestStoredAuthOnSigHeaderFails
battery/webhook/webhook.go: stored plaintext auth sent -> FAIL TestPlaintextAuthRefused
battery/webhook/webhook.go: success settle fenced on its lease -> FAIL TestStaleSuccessLandsOverDeadLetter
battery/webhook/webhook.go: too-short lease still sent -> FAIL TestTickStartBarFollowsClientTimeout,TestTickReleasesClaimsItCannotRun
battery/webhook/webhook.go: unknown lease counted too short -> FAIL TestUnknownLeaseIsNotTooShort
battery/webhook/webhook.go: unstorable secret accepted -> FAIL TestSubscriberRefusesUnstorableText
battery/webhook/webhook.go: zero ids -> FAIL TestNewID_NotAllZero
```

## service, log, rtc, sitesearch, telemetry, cache, email, search, setup, relay, storage, print, notify

- notify: routeSafe drops its recover -> TestPanickingHostCodeBecomesAnError
- notify: callback panic not promoted to the channel error -> TestPanickingHostCodeBecomesAnError
- notify: EmailChannel.Send drops its recover -> TestPanickingHostCodeBecomesAnError
- notify: renderFor drops its recover -> TestPanickingHostCodeBecomesAnError (test binary panicked or timed out)
- notify: channelSendSafely drops its recover -> TestPanickingHostCodeBecomesAnError (test binary panicked or timed out)
- notify: notifyOnErrorSafely drops its recover -> TestPanickingHostCodeBecomesAnError (test binary panicked or timed out)
- notify: assertSafeAddress skips email.ValidateAddress -> TestEmailChannel_RejectsCRLFRecipient TestEmailChannel_RejectsHTMLInAddress
- notify: a mistyped attachments entry is dropped instead of refused -> TestBadAttachmentsPreventSend
- notify: a rendered from override is accepted -> TestEmailChannel_RejectsRenderedFromOverride
- notify: Bcc drops out of the reserved headers -> TestEmailChannel_RejectsReservedBccHeaderOverride
- notify: reserved header compare is case-sensitive -> TestEmailChannel_RejectsReservedBccHeaderOverride TestEmailChannel_RejectsReservedContentTypeHeaderOverride TestEmailChannel_RejectsReservedFromHeaderOverride TestEmailChannel_RejectsReservedReplyToHeaderOverride TestEmailChannel_RejectsTransferEncodingOverride
- notify: a repeated router name sends twice -> TestRouterSelectionValidated
- notify: an unregistered router name is kept -> TestRouterSelectionValidated
- notify: Send ignores Recipient.Locale -> TestRecipientLocaleWins
- notify: Subject not stripped of unsafe codepoints -> TestMapTemplater_StripCRLFFromSubject TestSubjectScrubbedOfInvisible
- notify: interpolate output uncapped -> TestMapTemplater_CapsInterpolatedOutput
- notify: HTMLBody output uncapped -> TestHTMLBodyOutputIsCapped
- notify: Render shares the template Extra map -> TestTemplaterOwnsExtraMaps
- notify: Set keeps the caller Extra map -> TestTemplaterOwnsExtraMaps
- notify: a colon in a custom header name is accepted -> TestEmailChannelRejectsColonInHeaderName
- notify: per-channel pre-rendered payload ignored -> TestPerChannelPreRenderedWinsOverShared
- notify: LoggerChannel logs the text body unredacted -> TestLoggerChannelRedactsCredentialURLs
- notify: shared pre-rendered payload ignored -> TestNotifier_PreRenderedPayloadShortCircuitsTemplater
- notify: HTMLBody filled as plain text -> TestHrefValueRefusesJavascriptURL TestMapTemplaterEscapesHTMLBody TestUnquotedAttrValueStaysOneAttr
- notify: html/template.HTML values escaped -> TestMapTemplaterTrustedHTMLMarker
- notify: unfilled placeholder dropped -> TestHTMLBodyKeepsUnfilledPlaceholder
- notify: HTMLBody with a NUL is parsed -> TestHTMLBodyWithNULIsRefused
- notify: a typed nil attachments slice refused -> TestNilAttachmentSliceIsAccepted
- notify: own character list refuses an apostrophe and a display name -> TestEmailChannelAcceptsSMTPAddresses
- storage: local validateKey accepts an unclean alias -> TestAliasKeysRefused
- storage: Save skips the Windows portability rules -> TestSaveStillRejectsUnportableKeys
- storage: reads held to the portability rules -> TestExistingKeysStayReadableAndDeletable
- storage: containment checked lexically only -> TestLocalStorageSymlinkEscapeRefused
- storage: Delete removes a prefix directory -> TestLocalDirectoryKeyIsNotAnObject
- storage: open hands back a directory -> TestLocalDirectoryKeyIsNotAnObject
- storage: canceled save still renames -> TestSaveCanceledMidCopyKeepsObject
- storage: a resolve error quotes the absolute root -> TestStorageErrorsHideAbsRoot
- storage: local validateKey accepts every key -> TestAliasKeysRefused TestTraversalRejectedOnEveryOperation
- storage: checkObjectKey lets .. through -> TestS3KeyValidationPathTraversal TestS3PresignedKeyValidation
- storage: checkObjectKey accepts a control byte -> TestDefaultKeyValidatorRefusesUnformedKeys TestS3RefusesUnformedKeys
- storage: checkObjectKey accepts an absolute key -> TestDefaultKeyValidatorRefusesUnformedKeys TestS3RefusesUnformedKeys
- storage: presign accepts a zero expiry -> TestS3PresignRefusesNonPositiveExpiry
- storage: S3 Exists skips key validation -> TestS3KeyValidationEmptyKey TestS3KeyValidationPathTraversal TestS3RefusesUnformedKeys
- storage: presign skips key validation -> TestS3PresignedKeyValidation TestS3RefusesUnformedKeys
- storage: DefaultKeyValidator accepts a backslash -> TestDefaultKeyValidatorRefusesBackslash
- storage: memory Save stores an oversize body -> TestMemorySaveRefusesOversizeBody
- storage: memory Save stores after a mid-copy cancel -> TestMemorySaveCanceledMidCopyKeepsObject
- storage: checkObjectKey accepts an unclean key -> TestDefaultKeyValidatorRefusesUnformedKeys TestS3RefusesUnformedKeys
- storage: canceled resolve-then-open save still renames -> TestSaveCanceledMidCopyKeepsObject
- storage: mismatch does not wrap ErrChecksumMismatch -> TestVerifyChecksumMismatch
- storage: SaveWithChecksum reports no size -> TestSaveWithChecksumKnownVector TestSaveWithChecksumLargeStream
- storage: default file mode world-readable -> TestLocalStorage_DefaultFilesAreNotWorldReadable
- storage: directories world-traversable -> TestLocalStorage_DefaultDirectoriesAreNotWorldTraversable
- storage: re-pinning closes the live root -> TestFreshSymlinkUploadsKeepRoot
- storage: VerifyChecksum refuses uppercase hex -> TestVerifyChecksumUppercaseHex
- storage: VerifyChecksum accepts a short digest -> TestVerifyChecksumRefusesShortDigest
- storage: Close keeps the pinned roots open -> TestCloseClosesThePinnedRoot
- storage: S3Storage implements RangeGetter -> TestS3DeclinesRangeGetter
- storage: open follows a symlink planted after resolve -> TestLocalStorageRefusesMidResolvePlant
- storage: Exists stats through a planted symlink -> TestLocalStorageRefusesMidResolvePlant
- print: RequireOwner ignores the token scope -> TestPrintGatesNarrowByScope
- print: default access is Public -> TestDefaultRequiresAuth TestSecPerUserDocNotPublic
- print: root prefix mounts -> TestPrintRefusesRootPrefix
- print: RegisterRoutes mounts twice -> TestRegisterRoutesIdempotent
- print: a document may take the auto-print path -> TestDocumentPathCollisionPanics
- print: Document after Init accepted -> TestDocumentAfterInitPanics
- print: Build panic crashes -> TestBuildPanicIsClean500
- print: render panic served as 200 -> TestRenderPanicIsClean500
- print: ErrForbidden maps to 500 -> TestBuildErrorForbidden403
- print: ErrNotFound maps to 500 -> TestBuildErrorIsCleanStatus
- print: HTML route cacheable -> TestSecNoStoreOnPerUserDoc
- print: auto-print wired on PDF -> TestAutoPrintWiredOnHTMLNotPDF
- print: BaseURL spliced into CSP unchecked -> TestPDFCSPRefusesUncleanBaseURL
- print: cspOrigin keeps userinfo -> TestPDFCSPRefusesUncleanBaseURL
- print: cspOrigin accepts any host byte -> TestPDFCSPRefusesUncleanBaseURL
- print: PDF app.css from the Host header -> TestPDFAppCSSUsesBaseURLNotHost
- print: RequireAuth ignores the token scope -> TestPrintGatesNarrowByScope
- print: renders unbounded -> TestPDFRenderConcurrencyBounded
- print: filename takes a param from a multi-param route -> TestPDFFilenameParamCounts
- print: unwired PDF answers 404 -> TestNotConfiguredReturns501
- print: any CSS length accepted -> TestCustomLengthRejected
- print: app.css href not scheme-checked -> TestShellAssetsRedSchemeGuarded
- print: render slot taken before the access gate -> TestPDFAccessRefusalWinsOverBusy TestPDFRefusedRequestHoldsNoRenderSlot
- print: PDF route skips the access gate -> TestPDFAppliesAccessGate
- print: shell drops the in-document CSP -> TestPDFBlocksInternalSubresource TestPDFCSPAllowsBaseURLStyles
- print: title unescaped -> TestTitleEscaped
- print: auto-print src not scheme-checked -> TestShellAssetsRedSchemeGuarded
- print: filename keeps unsafe bytes -> TestPDFFilenameKeepsOnlySafeBytes
- print: column knob dropped -> TestPrintBaseCSSHasTheColumnKnob
- print: component CSS not inlined -> TestComponentCSSInlinedWhenUsed
- print: a document's top margin ignored -> TestEffectivePageInherits
- chromepdf: no resolver gate -> TestPDFRendererAllowListIsExact TestPDFRendererRefusesNetworkByDefault
- chromepdf: allow-list matches suffixes -> TestPDFRendererAllowListIsExact
- chromepdf: start waits keep their short default -> TestSlowStartWithinTimeout
- chromepdf: does not wait for the load event -> TestRenderPDFWaitsForStylesheet
- chromepdf: document passed as a data URL -> TestRenderPDFLargeDocument
- print: HTML route skips the access gate -> TestDefaultRequiresAuth
- print: default render cap is not 4 -> TestPDFRenderConcurrencyBounded
- print: RequireOwner ignores owns -> TestOwnerMismatchForbidden
- relay: Cookie forwarded upstream -> TestInboundCredentialsStripped
- relay: X-Real-IP forwarded upstream -> TestClientIPHeadersNotRelayedUpstream
- relay: X-Gofastr- headers forwarded -> TestEmbedGrantNotForwarded
- relay: vendor Set-Cookie reaches the client -> TestOutboundAuthHeadersStripped TestVendorBrowserStateHeadersStripped
- relay: vendor Refresh reaches the client -> TestVendorBrowserStateHeadersStripped
- relay: Service-Worker-Allowed reaches the client -> TestServiceWorkerAllowedStripped
- relay: vendor NEL reaches the client -> TestVendorOriginPolicyHeadersStripped
- relay: vendor CORS headers reach the client -> TestOutboundAuthHeadersStripped
- relay: 1xx forwarded to the client -> TestEarlyHintsKeepHostHeaders
- relay: host headers not restored after a 1xx -> TestEarlyHintsKeepHostHeaders
- relay: Upgrade forwarded -> TestRelayStripsInboundUpgrade
- relay: 101 spliced -> TestRelayRefuses101AndClosesUpstream
- relay: full Referer forwarded -> TestRefererReducedToOrigin
- relay: X-Forwarded-For copied from the client -> TestClientIPOverride
- relay: proto from a header -> TestForwardedProtoHTTPSOnTLSLeg
- relay: transport compression on -> TestGzipNotAskedForOrUnpacked
- relay: query dropped -> TestSubtreeForwardsPathAndQuery
- relay: transport honours an environment proxy -> TestTransportIgnoresEnvironmentProxy
- relay: localhost may resolve off loopback -> TestGuardedDialRejectsUnsafeResolutions
- relay: declared oversize body read -> TestBodyCapDeclaredLength413
- relay: chunked body uncapped -> TestBodyCapChunked413
- relay: upstream redirect forwarded -> TestUpstreamRedirectRefused502
- relay: refused redirect leaks the upstream body -> TestRefusedRedirectClosesUpstreamBody
- relay: nosniff not pinned -> TestOutboundAuthHeadersStripped TestRelayedContentTypeAllowList TestVendorBrowserStateHeadersStripped
- relay: no-store not forced -> TestNoStoreForcedWhenCacheOKFalse
- relay: CacheOK ignored -> TestCacheOKTruePassesCacheHeaders
- relay: response body uncapped -> TestOversizedResponseAbortsStream
- relay: oversize response ends with EOF -> TestOversizedResponseAbortsStream
- relay: vendor Link preload reaches the client -> TestLinkOtherOriginSpellingsStripped
- relay: backslash Link target read as written -> TestLinkOtherOriginSpellingsStripped
- relay: missing type left for the sniffer -> TestMissingContentTypeNotSniffed
- relay: SVG not sandboxed -> TestRelayedSVGSandboxed
- relay: bodiless response refused by type -> TestBodilessResponsePassesAnyType
- relay: inbound X-Forwarded-Host copied upstream -> TestInboundXForwardedHostNotRelayed
- relay: dial skips the internal-address check -> TestGuardedDialRejectsDNSRebindingToLoopback TestGuardedDialRejectsUnsafeResolutions
- relay: Path may take a reserved __gofastr route -> TestNewPanicsOnBadPath
- relay: mux metacharacters accepted in a path -> TestRouteConfigRequiresLiteralPaths
- relay: percent-encoding accepted in a path -> TestNewPanicsOnBadPath TestNewPanicsOnBadRouteConfig
- relay: traversal segment accepted in a path -> TestNewPanicsOnBadPath TestNewPanicsOnBadRouteConfig
- relay: empty Methods accepted -> TestNewPanicsOnBadRouteConfig
- relay: duplicate prefix accepted -> TestNewPanicsOnBadRouteConfig
- relay: plaintext public upstream accepted -> TestNewPanicsOnBadUpstream
- relay: userinfo upstream accepted -> TestNewPanicsOnBadUpstream
- relay: upstream with a query accepted -> TestNewPanicsOnBadUpstream
- relay: .internal upstream accepted -> TestNewPanicsOnBadUpstream
- relay: resolved private upstream accepted -> TestResolvedPrivateUpstreamRefused
- relay: shutdown leaves idle conns -> TestShutdownClosesIdleTransportConns
- relay: default ClientIP reads a header -> TestClientIPHeadersNotRelayedUpstream
- relay: lowercase method accepted -> TestNewPanicsOnBadRouteConfig
- relay: non-canonical percent-encoding in the tail accepted -> TestHostileTailsRejectedOrStayOnHost TestValidateTailRejectsHostileForms
- relay: control byte in the tail accepted -> TestHostileTailsRejectedOrStayOnHost TestValidateTailRejectsHostileForms
- relay: backslash in the tail accepted -> TestHostileTailsRejectedOrStayOnHost TestValidateTailRejectsHostileForms
- relay: fragment in the tail accepted -> TestHostileTailsRejectedOrStayOnHost TestValidateTailRejectsHostileForms
- relay: empty inner segment accepted -> TestValidateTailRejectsHostileForms
- relay: traversal segment in the tail accepted -> TestValidateTailRejectsHostileForms
- relay: text/css refused -> TestRelayedContentTypeAllowList
- relay: any application type relayed -> TestRelayedContentTypeAllowList
- relay: Route.ContentTypes ignored -> TestRouteContentTypesOptIn
- relay: trailing slash refused -> TestTrailingSlashTailForwarded
- relay: trailing slash dropped upstream -> TestExactRouteKeepsBaseSlash TestJoinUpstreamPathSlash TestTrailingSlashTailForwarded
- relay: unresolvable upstream refused at New -> TestBaseReturnsMountPath
- relay: outbound host from the request -> TestHostileTailsRejectedOrStayOnHost TestSubtreeForwardsPathAndQuery
- relay: transport error detail reaches the client -> TestTransportFailureHidesUpstreamDetail
- relay: malformed ContentTypes entry accepted -> TestNewPanicsOnBadContentType
- setup: token survives its first exchange -> TestToken_SingleUse_CookieSurvives TestToken_SingleUse_SecondExchangeForbidden
- setup: any token exchanges -> TestToken_WrongTokenReturns403
- setup: cookie SameSite Lax -> TestToken_ValidTokenSetsCookie
- setup: cross-site POST accepted -> TestCrossSite_DifferentOriginRejected TestCrossSite_RejectedOnPost TestSetupRedSameSiteOriginRefused
- setup: form body uncapped -> TestSetupFormCapsBody
- setup: secret trimmed -> TestWizardStoresSecretAsTyped TestWizardValidatesSecretAsTyped
- setup: force mode cannot re-run a completed step -> TestSetupForceReRunsCompletedSteps
- setup: Complete probed on every request -> TestRecheckOnePerInterval
- setup: swap while the first step runs -> TestNoSwapWhileFirstStepRuns
- setup: pages frameable -> TestSetupPagesRefuseFraming
- setup: hop page sends a referrer -> TestCrossSiteArrivalKeepsToken
- setup: headless runs on a completed install -> TestRunStepsRefusesWhenComplete
- setup: plain field kept untrimmed -> TestWizardTrimsPlainFields
- setup: an empty secret authenticates an empty cookie -> TestEmptyCookieBeforeExchangeIsRefused
- setup: the cookie secret is the URL token -> TestSetupTokenNotReplayableAsCookie
- setup: cookie always Secure -> TestCookie_SecureFlagFromRequest
- setup: force mode never on -> TestForceKeepsWizardUntilStepsRun TestSetupForceReRunsCompletedSteps
- setup: a step runs twice concurrently -> TestConcurrentStepExecution_OneRun
- setup: in-flight claim never released -> TestWizardRunsStepsAfterComplete
- setup: a probe error runs the step -> TestSetupProbeErrorFailsClosed TestSwap_CompleteErrorRecovers
- setup: healthz gets 503 -> TestSwap_HealthzReturns200
- setup: completed-elsewhere hop is a 302 -> TestCompletedElsewhereSwapsOnAnyPath
- setup: 503 without Retry-After -> TestSetupUnavailableUsesLocale
- setup: a secret is pre-filled from env -> TestSecretNeverPrefilled
- setup: an empty rejected value falls back to env -> TestRejectedStepKeepsPublicValues
- setup: admin email stored raw -> TestAdminRefusesUnstorableEmail TestAdminStepCanonicalizesEmail
- setup: weak admin password accepted -> TestAdminStep_WeakPasswordFails
- setup: admin created without the admin role -> TestAdminStep_CreatesAdminWithRole
- setup: New accepts a nil Complete -> TestNew_PanicsWithoutComplete
- setup: an unset env var counts as resolved -> TestAllFieldsFromEnv_PartialMissing TestCanRunHeadless_PartialMissing
- setup: completion page shown with Complete false -> TestSwap_CompleteStuckFalseIsHonest
- storage: Exists reports a directory -> TestLocalDirectoryKeyIsNotAnObject
- search: query terms uncapped -> TestBuildFts5QueryBounded TestBuildTsQueryBounded
- search: memory negative offset not clamped -> TestSearchPaginationBoundsClamped
- search: memory offset+limit overflow -> TestSearchPaginationBoundsClamped
- search: memory returns its own Fields map -> TestMemoryOwnsDocumentFields TestSearchCopiesOnlyReturnedFields
- search: memory keeps the caller's Fields map -> TestMemoryOwnsDocumentFields
- search: any Postgres language accepted -> TestPostgresHostileConfigRejected
- search: tsquery parsed under the default config -> TestPostgresLanguageStemming
- search: sqlite FieldEquals matches JSON text -> TestSQLiteFieldEqualsSkipsJSONText
- search: weighted non-string value skipped silently -> TestPostgresWeightedNonStringRefused
- search: SQL erasers key on the wrong column -> TestEraseRemovesPostgresDocuments TestEraseRemovesSQLiteDocuments
- search: Close leaves the eraser registered -> TestBatteryStopClosesMemory TestEraseRemovesMemoryDocuments
- search: FTS5 terms unquoted -> TestBuildFts5Query TestBuildFts5QueryBounded
- search: an empty owner erases unowned documents -> TestMemoryCloseStopsErasure
- search: dry run deletes -> TestMemoryCloseStopsErasure
- search: memory matches substrings -> TestMemoryMatchesWholeWords TestMemorySearchRequiresAllTerms
- search: memory last term not a prefix -> TestMemoryMatchesWholeWords
- search: a 63-byte table's tsv index takes the table's name -> TestPostgresLongTableIndexes
- search: released tsv index name not kept -> TestPostgresKeepsReleasedTsvIndex
- search: backfill stops at the first empty batch -> TestPostgresBackfillOutlastsLostBatch
- search: backfill stops after one batch -> TestPostgresBackfillLeavesTableOpen TestPostgresBackfillOutlastsLostBatch
- email: CR/LF accepted in a header -> TestEmail_AttachmentFilenameHeaderInjection TestEmail_HeaderInjection
- email: a Headers key with a colon accepted -> TestHeadersKeyIsOneFieldName
- email: case-folded duplicate Headers keys accepted -> TestHeadersKeysAreOneFieldEach
- email: Headers may set MIME structure and Bcc -> TestHeadersCannotNameMIMEStructure
- email: no Message-ID -> TestMessageHasDateAndMessageID
- email: caller Date not honoured -> TestHeadersOverrideDateAndMessageID
- email: body holding the boundary sent -> TestBoundaryCheckReadsEncodedLines TestNestedBoundaryCannotBeForged
- email: cleartext send without STARTTLS -> TestSMTP_NoCleartextWhenSTARTTLSUnavailable
- email: no exchange deadline -> TestSMTP_StalledServerDoesNotWedge
- email: cancel does not close the connection -> TestSMTPCancelEndsStalledSend
- email: QUIT failure reported after acceptance -> TestSendIgnoresQuitAfterAccept
- email: RCPT list appends onto the caller's To -> TestSmtpDoesNotMutateCallerTo
- email: invisible codepoint in a header not refused -> TestInvisibleHeaderCodepointIsAnError
- email: attachment content type parameters kept -> TestAttachmentContentTypeKeepsOnlyTheBaseType
- email: LogSender text body unredacted -> TestLogSender_DoesNotExposeLiveResetLinksInTextBody
- email: LogSender HTML body unredacted -> TestLogSender_DoesNotExposeLiveResetLinksInHTMLBody
- email: LogSender prints sensitive headers -> TestLogSender_DoesNotExposeSensitiveHeaders
- email: LogSender prints an unscrubbed subject -> TestLogSenderScrubsHeaders
- email: LogSender prints body control bytes -> TestLogSenderScrubsBodiesAndAttachments
- email: CRLF subject keeps its CR -> TestLoadFromFSCRLFSubject
- email: CRAM-MD5 not preferred -> TestSMTPAuthCRAMOnlyRelay TestSMTPAuthPrefersCRAM
- email: LOGIN over cleartext to a remote host -> TestSMTPLoginAuthRefusals
- email: odd relay words reach the error -> TestSMTPAuthErrorDropsOddWords
- email: headers never fold -> TestLongHeadersFoldUnder998
- email: bodies sent raw, not quoted-printable -> TestBodyLinesAreCRLFAndShort
- email: HTML body rendered with text/template -> TestEmail_TemplateInjection TestExecuteEscapesHTMLBodyVars
- email: raw UTF-8 in the Subject -> TestNonASCIIHeadersAreEncoded
- email: text+HTML sent as mixed -> TestTextAndHTMLAreAlternative
- email: a comma list validates as one mailbox -> TestValidateAddressOneMailbox
- email: envelope local part may hold <> -> TestSmtpEnvelopeRefusesPathBreak
- email: envelope local part unquoted -> TestSmtpEnvelopeQuotesLocalPart
- email: LogSender prints Bcc -> TestLogSender_DoesNotExposeBCCRecipients
- email: RedactBody misses key, secret and password params -> TestRedactBodyEveryParam
- email: RedactBody keeps Bearer values -> TestRedactBodyDropsBearerValue
- cache: unsafe method leaves the stored GET -> TestUnsafeRequestEvictsEveryLocale TestUnsafeRequestEvictsEveryVariant TestUnsafeRequestEvictsGET
- cache: unsafe method evicts one locale only -> TestUnsafeRequestEvictsEveryLocale
- cache: credentialed requests cached -> TestCacheMiddleware_DoesNotCacheAuthorizationRequestsByDefault TestCacheMiddleware_DoesNotCacheCookieAuthenticatedRequestsByDefault TestCacheMiddleware_DoesNotCacheEmbedGrantResponses TestCacheSkipsAPIKeyRequests
- cache: runtime navigation served from cache -> TestNavigationFetchesBypassCache
- cache: key omits the host -> TestCacheMiddleware_DoesNotLeakAcrossHosts
- cache: key uses the decoded path -> TestCacheKeysEscapedSlashApart
- cache: 304 stored -> TestCacheMiddleware_StoreableStatuses
- cache: 206 stored -> TestCacheMiddleware_StoreableStatuses
- cache: outer Set-Cookie response stored -> TestCacheHonoursOuterNoStore
- cache: private and no-cache responses stored -> TestCacheMiddleware_DoesNotCacheNoCacheResponses TestCacheMiddleware_DoesNotCachePrivateResponses
- cache: Vary star on a second line stored -> TestVaryStarOnSecondLineNotStored
- cache: outer Vary ignored -> TestCacheKeysAcceptLanguage
- cache: 103 recorded as the response -> TestCacheKeepsStatusAfterHints
- cache: negative limit accepted -> TestCacheLimitRefusesNegative
- cache: request no-cache served a stored variant -> TestCacheMiddleware_RequestNoCacheBypassesStoredVariant
- cache: request no-store served a stored variant -> TestCacheMiddleware_RequestNoStoreBypassesStoredVariant
- cache: fill runs under the leader's context -> TestGetOrSetRedWaiterKeepsOwnCtx
- cache: loader panic crashes -> TestGetOrSetLoaderPanicIsAnError (test binary panicked or timed out)
- cache: evicted fill reported as a miss -> TestGetOrSetReturnsLoadedValueWhenEntryIsGone
- cache: memory negative TTL stores forever -> TestMemoryCache_NegativeTTLNotImmortal
- cache: redis negative TTL passed to the client -> TestRedisCache_NegativeTTLNotImmortal TestRedisCache_NegativeTTLStoresNothing
- cache: redis backend error reported as a miss -> TestGetOrSet_BackendOutageNotMiss TestRedisCache_BackendErrorNotMiss
- cache: SCAN pattern unescaped -> TestRedisClearGlobPrefix
- cache: prefix not escaped -> TestRedisCache_PrefixNoKeyCollision TestRedisPrefixEscapeIsInjective
- cache: bounded cache never evicts -> TestMemoryCacheBoundedEvicts
- cache: evicts the most recent entry -> TestMemoryCacheLRUKeepsRecent
- cache: Clear trusts the scanner's matches -> TestRedisClearDeletesOnlyOwnedKeys
- cache: an abandoned fill keeps running -> TestGetOrSetAbandonedFillStoresNothing TestGetOrSetValueCacheStalledLoader
- cache: value caches share one fill -> TestGetOrSetDoesNotJoinSeparateValueCacheInstances
- cache: loader error result stored -> TestGetOrSetLoaderErrorNotCached
- cache: a read does not refresh recency -> TestMemoryCacheLRUKeepsRecent
- cache: OnStop leaves MemoryCache's cleanup running -> TestBatteryStopEndsMemoryCleanup
- cache: a primed 304 replayed -> TestPrimed304IsNotReplayed
- sitesearch: index built under the caller's identity -> TestIndexIgnoresCallerIdentity
- sitesearch: prefixes match mid-segment -> TestGroupsMatchWholeSegments
- sitesearch: every query retries a failed build -> TestFailedBuildWaitsToRetry
- sitesearch: query length unbounded -> TestLiveQueryBoundsParams
- sitesearch: negative MaxAge accepted -> TestNegativeMaxAgePanics
- sitesearch: Exclude misses Source pages -> TestExcludeCoversSources
- sitesearch: hidden and ignored content indexed -> TestExtractSkipsChrome TestExtractSkipsHiddenMain
- sitesearch: robots none kept -> TestExtractRobotsNoneDrops
- sitesearch: ignored noindex drops the page -> TestExtractIgnoredNoindexKeepsPage
- sitesearch: scheme-relative URL indexed -> TestBuildDropsOffSiteURLs
- sitesearch: duplicate pages indexed -> TestBuildDropsDuplicatePages
- sitesearch: no title bonus -> TestSearchTitleNamesTheAnswer
- sitesearch: page parameter unbounded -> TestScreenPages
- sitesearch: relative ScreenPath accepted -> TestScreenPathRefusesRelative
- sitesearch: one-rune terms indexed -> TestIndexTermsSplitCamelCase
- sitesearch: term counts uncapped -> TestRepeatedBodyTermIsCapped
- sitesearch: build ignores its caller's cancel -> TestCancelledBuildIsNotCached (test binary panicked or timed out)
- sitesearch: Go ranker drifts from the static JS ranker -> TestStaticSearchMatchesGo
- sitesearch: robots noindex kept -> TestExtractNoindex TestExtractRobotsNoneDrops
- telemetry: tools registered without EnableMCP -> TestMCPToolsAreOptIn
- telemetry: machine reader panic ends the round -> TestAPanickingMachineReaderDoesNotEndTheRound
- telemetry: a hung notifier is waited for -> TestHungNotifierIsLeftBehind (test binary panicked or timed out)
- telemetry: webhook error quotes the URL -> TestWebhookNotifier
- telemetry: webhook follows redirects -> TestWebhookNotifier
- telemetry: heartbeat follows redirects -> TestHeartbeatPingsOnceAMinute
- telemetry: a malformed heartbeat URL accepted at start -> TestHeartbeatPingsOnceAMinute
- telemetry: second Init accepted -> TestSecondInitIsRefused
- telemetry: global provider replaced under ExternalTracerProvider -> TestExternalTracerProviderIsLeftAlone
- telemetry: tenant-scoped admin reads the tools -> TestMCPToolsRefuseNonAdmins
- telemetry: tools open to non-admins -> TestMCPToolsRefuseNonAdmins
- telemetryadmin: tenant-scoped admin sees the pages -> TestPagesRefuseATenantScopedCaller
- telemetryui: open mount accepted -> TestInitRefusesAnOpenOrHostlessMount
- telemetryui: control bytes kept in a query value -> TestQueryValuesAreCleaned
- telemetryui: query value uncapped -> TestQueryValuesAreCleaned
- telemetryui: a wide range refreshes itself -> TestOnlyAShortReadRefreshesItself
- telemetry: MCP limit uncapped -> TestLimitArgIsCapped
- telemetryui: Authorize not asked -> TestRowsRouteRefusesAnUnadmittedCaller TestScreensRefuseAnUnadmittedCaller
- rtc: frame flood not closed -> TestFrameFloodClosesSocket
- rtc: revoked peer keeps sending -> TestRevokedPeerIsClosed
- rtc: lax frame decode -> TestFrameDecodeRedRefusesDuplicateKind TestFrameDecodeRedRefusesFoldedKeys
- rtc: oversize signal relayed -> TestOversizeSignalClosesSocket
- rtc: non-object status accepted -> TestStatusMustBeObject
- rtc: Mount without Authorize accepted -> TestMountPanicsWithoutAuthorize
- rtc: signal cap past the read limit accepted -> TestNewPanicsOnBadConfig
- rtc: TURN TTL below a minute kept -> TestTurnCredentialMatchesCoturn
- rtc: refusal reads one Upgrade line -> TestRefusalReadsEveryUpgradeLine
- rtc: any room name accepted -> TestValidJoinRefusesEachBadField
- rtc: control bytes in Join.User accepted -> TestValidJoinRefusesEachBadField
- rtc: per-user sockets uncapped -> TestSeatFloodRedCapsSocketsPerUser
- rtc: evicts the newest seat -> TestSeatFloodRedCapsSocketsPerUser
- rtc: anonymous join admitted by default -> TestAnonymousJoinRefusedByDefault
- rtc: remote status need not be an object -> TestRemotePeerEntryIsBounded
- rtc: remote user may hold control bytes -> TestRemotePeerEntryIsBounded
- rtc: any ICE scheme accepted -> TestInvalidICESchemeRefused
- rtc: TURN credential off the coturn convention -> TestTurnCredentialMatchesCoturn
- rtc: signal broadcast to the room -> TestSignalDeliveredToTargetOnly
- rtc: room cap not checked -> TestConcurrentJoinsNeverExceedCap TestRoomFullRefused
- rtc: refresh mints for revoked peers -> TestTURNRefreshSkipsRevokedPeer
- rtc: remote join past the cap applied -> TestRemoteJoinRespectsCap
- rtc: expired replica never swept -> TestRemotePeerExpiresAfterTTL
- rtc: Close publishes no empty rosters -> TestClosePublishesEmptyRosters TestDetachEmptyRosterLandsLast
- log: access path uncapped -> TestAccessPathCapped
- log: access path unscrubbed -> TestAccessEntryScrubbedOfControlBytes TestAccessInvisibleCharsScrubbed
- log: X-Forwarded-For trusted by default -> TestSpoofedXFFDoesNotChangeRemote
- log: panic value uncapped -> TestPanicEntrySizeCapped
- log: ErrAbortHandler swallowed -> TestRecoveryReraisesAbortHandler
- log: 103 logged as the status -> TestAccessLogKeepsStatusAfterHints
- log: file sink follows a symlink -> TestFileSinkRefusesSymlink
- log: default file mode world-readable -> TestFileSinkDefaultModeIs0o600
- log: rotation shifts zero-padded backups -> TestRotationTreatsZeroPaddedBackupsAsForeign
- log: webhook retries 4xx -> TestWebhookNoRetryOn4xx
- log: webhook drop not counted -> TestWebhookDroppedCounterAdvances
- log: caller header overrides Content-Type -> TestWebhookContentTypeIsForcedWhateverTheKeyCase
- log: console msg unescaped -> TestConsoleSinkNeutralizesMsgKeyAndFallback TestConsoleSinkRendersNoControlBytes
- log: log_set_level always registered -> TestMCPMutationGated
- log: dev-implied MCP tools take the host MCPGate instead of the admin gate -> TestLogMCPGateHoldsInDev
- log: forwarded_for uncapped -> TestForwardedHeaderCapped
- log: forwarded_for reads one line -> TestAccessLogAllForwardedLines
- log: malformed console entry written raw -> TestConsoleMalformedEntryIsEscaped
- log: webhook errors keep the URL -> TestWebhookGiveUpWarningHidesURLCredential TestWebhookRequestBuildErrorHidesURL
- log: metrics cacheable -> TestMetricsHandlerCarriesNoStore
- log: tools open to anyone -> TestLogReadToolsRefuseAnonCaller TestLogToolsRequireAdmin
- log: reporter panic value unscrubbed -> TestSlogReporterScrubbedOfCtrlBytes
- log: With chain replayed out of order -> TestFanoutWithAttrsAndGroups TestHandlerNestsGroupsLikeSlog
- log: backups past MaxBackups kept -> TestFileSinkRespectsMaxBackups
- log: sink failure never reported -> TestFanoutRedStderrPreviewScrubbed TestFanoutStderrFallbackOnSinkError
- log: parent dirs world-readable -> TestFileSinkParentDirDefaultModeIs0o700
- log: query string logged -> TestAccessLogOmitsQueryString
- service: any Host accepted -> TestGateRefusesRebindingHost
- service: pending tokens uncapped -> TestGateCapsPendingTokens
- service: token reusable -> TestGateTokenIsSingleUse
- service: unsigned session accepted -> TestGateRefusesForgedSessions
- service: session never expires -> TestGateSessionExpires
- service: first cookie decides -> TestGateStaleDuplicateCookieDoesNotLogOut
- service: cross-site unsafe method passes -> TestGateRefusesCrossSiteUnsafeMethod
- service: open path takes a forged cross-site write -> TestOpenPrefixKeepsCrossSiteRefusal
- service: login body unbounded -> TestGateLoginBodyIsBounded
- service: session cookie Lax -> TestGateSessionCookieAttributes
- service: token never expires -> TestGateTokenExpires
- service: GET ?t= works without the hatch -> TestGateLoginGETNeedsHatch
- service: foreign OS user accepted on the control socket -> TestControlRefusesForeignPeer
- service: control request unbounded -> TestControlBoundsRequestSize
- service: lax control decode -> TestControlRefusesMalformedRequests
- service: control socket world-writable -> TestListenControlRestrictsSocket
- service: over-long socket path accepted -> TestInstallRefusesLongSocketPathFirst TestListenControlRefusesLongPath
- service: state dir left loose -> TestEnsurePrivateDirTightens
- service: short secret accepted -> TestShortSecretRefused
- service: root install accepted -> TestInstallRefusesRoot
- service: systemd $ unescaped -> TestQuoteSystemd
- service: relative data dir accepted -> TestDataDirEnvMustBeAbsolute
- service: MCP sessions outlive the drain -> TestMCPSessionEndsBeforeDrain
- service: plist values unescaped -> TestLaunchdPlistEscapes
- service: task XML unescaped -> TestTaskXML
- service: control bytes in the data dir accepted -> TestDataDirEnvRefusesControlBytes
- service: control handler panic kills the daemon -> TestControlRecoversHandlerPanic (test binary panicked or timed out)
- service: a quote in a Windows arg left bare -> TestQuoteWindowsArg TestQuoteWindowsArgRoundTrips
- service: unpinned gate lets requests through -> TestGateRefusesEverythingBeforePin
- merge: notify Bcc not reserved -> TestEmailChannel_RejectsUnsafeRenderedHeaders
- merge: email LogSender text body unredacted -> TestLogSender_DoesNotExposeLiveResetLinks
- merge: email LogSender HTML body unredacted -> TestLogSender_DoesNotExposeLiveResetLinks
- merge: rtc tokenbucket refusal ignored -> TestFrameFloodClosesSocket
- merge: rtc revoked peer keeps sending -> TestRevokedPeerIsClosed
- merge: notify CR/LF in a header value accepted -> TestEmailChannel_RejectsUnsafeRenderedHeaders
- merge: notify header name unchecked -> TestEmailChannel_RejectsUnsafeRenderedHeaders
- merge: log_set_level marked read-only -> TestMCPToolsCarryListingHints
- merge: telemetry tools not marked read-only -> TestMCPToolsCarryListingHints
