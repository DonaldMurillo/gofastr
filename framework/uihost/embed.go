package uihost

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

const (
	embedLoaderPath      = fembed.LoaderPath
	embedRuntimePath     = fembed.RuntimePath
	embedExchangePath    = fembed.ExchangePath
	embedRefreshPath     = fembed.RefreshPath
	embedShellPath       = fembed.SurfacePrefix + "{surface}"
	embedShellPathParams = fembed.SurfacePrefix + "{surface}/{path...}"
	embedContentPath     = fembed.SurfacePrefix + "{surface}/content"
	embedGrantHeader     = fembed.GrantHeader
)

const maxEmbedThemeParam = 6 << 10

const embedThemeWait = 2 * time.Second

// WithEmbed serves the app's embeddable surfaces from h, built with
// framework/embed.New. Until h has signing keys every embed route answers 503.
func WithEmbed(h *fembed.Host) Option {
	return func(ds *UIHost) {
		ds.embedHost = h
		if h != nil {
			h.SetWidgetReach(func(name, surfacePath string) bool {
				def, ok := widget.Lookup(name)
				return ok && def.IsAvailableOn(surfacePath)
			})
		}
	}
}

// EmbedHost returns the configured embed host, or nil.
func (ds *UIHost) EmbedHost() *fembed.Host { return ds.embedHost }

type embedThemeState struct {
	mu       sync.Mutex
	resolved map[string]map[string]string
	used     map[string]map[string]uint64
	tick     uint64
	pending  map[string]map[string]chan struct{}
}

func (s *embedThemeState) pendingLocked(surface, param string) chan struct{} {
	if s.pending == nil {
		s.pending = map[string]map[string]chan struct{}{}
	}
	if s.pending[surface] == nil {
		s.pending[surface] = map[string]chan struct{}{}
	}
	ch, ok := s.pending[surface][param]
	if !ok {
		ch = make(chan struct{})
		s.pending[surface][param] = ch
	}
	return ch
}

func (s *embedThemeState) settleLocked(surface, param string) {
	if byParam := s.pending[surface]; byParam != nil {
		if ch, ok := byParam[param]; ok {
			close(ch)
			delete(byParam, param)
		}
	}
}

func (s *embedThemeState) waitFor(surface, param string, timeout time.Duration) (string, bool) {
	s.mu.Lock()
	ch, inFlight := chanFor(s.pending[surface], param)
	s.mu.Unlock()
	if !inFlight {
		return s.lookup(surface, param)
	}
	select {
	case <-ch:
	case <-time.After(timeout):
	}
	return s.lookup(surface, param)
}

func chanFor(m map[string]chan struct{}, param string) (chan struct{}, bool) {
	if m == nil {
		return nil, false
	}
	ch, ok := m[param]
	return ch, ok
}

func (s *embedThemeState) lookup(surface, param string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.resolved[surface][param]
	if key == "" {
		return "", false
	}
	s.touchLocked(surface, param)
	return key, true
}

func (s *embedThemeState) reserve(surface, param string, max int) (ok bool, evicted []string, dup bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolved == nil {
		s.resolved = map[string]map[string]string{}
	}
	byParam := s.resolved[surface]
	if byParam == nil {
		byParam = map[string]string{}
		s.resolved[surface] = byParam
	}
	if _, taken := byParam[param]; taken {
		return false, nil, true
	}
	for len(byParam) >= max {
		victim, ok := s.lruLocked(surface, byParam)
		if !ok {
			return false, evicted, false
		}
		evicted = append(evicted, byParam[victim])
		delete(byParam, victim)
		delete(s.used[surface], victim)
	}
	byParam[param] = ""
	s.pendingLocked(surface, param)
	s.touchLocked(surface, param)
	return true, evicted, false
}

func (s *embedThemeState) lruLocked(surface string, byParam map[string]string) (string, bool) {
	var victim string
	var oldest uint64
	found := false
	for param, key := range byParam {
		if key == "" {
			continue
		}
		n := s.used[surface][param]
		if !found || n < oldest {
			victim, oldest, found = param, n, true
		}
	}
	return victim, found
}

func (s *embedThemeState) touchLocked(surface, param string) {
	if s.used == nil {
		s.used = map[string]map[string]uint64{}
	}
	if s.used[surface] == nil {
		s.used[surface] = map[string]uint64{}
	}
	s.tick++
	s.used[surface][param] = s.tick
}

func (s *embedThemeState) record(surface, param, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolved[surface] != nil {
		s.resolved[surface][param] = key
	}
	s.settleLocked(surface, param)
}

func (s *embedThemeState) release(surface, param string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if byParam := s.resolved[surface]; byParam != nil {
		if byParam[param] == "" {
			delete(byParam, param)
		}
	}
	delete(s.used[surface], param)
	s.settleLocked(surface, param)
}

func (ds *UIHost) mountEmbed(r *router.Router) {
	if ds.embedHost == nil {
		return
	}
	r.Get(embedLoaderPath, http.HandlerFunc(ds.handleEmbedLoaderJS))
	r.Get(embedRuntimePath, http.HandlerFunc(ds.handleEmbedRuntimeJS))
	r.Post(embedExchangePath, http.HandlerFunc(ds.handleEmbedExchange))
	r.Post(embedRefreshPath, http.HandlerFunc(ds.handleEmbedRefresh))
	r.Get(embedExchangePath, http.HandlerFunc(methodNotAllowed))
	r.Get(embedRefreshPath, http.HandlerFunc(methodNotAllowed))
	r.Get(embedShellPath, http.HandlerFunc(ds.handleEmbedShell))
	r.Get(embedShellPathParams, http.HandlerFunc(ds.handleEmbedShell))
	r.Get(embedContentPath, http.HandlerFunc(ds.handleEmbedContent))
}

func (ds *UIHost) embedPagePath(s *fembed.ResolvedSurface, escaped string) (routed string, ok bool) {
	if !s.Dynamic() {
		return s.Path(), escaped == ""
	}
	decoded, _ := url.PathUnescape(escaped)
	if _, ok := fembed.RoutedPath(&url.URL{Path: decoded, RawPath: escaped}); !ok {
		return "", false
	}
	routed = app.RoutePath(escaped)
	want, ok := ds.App.Router.ScreenByPattern(s.Path())
	got, _, found := ds.App.Router.Resolve(routed)
	if !ok || !found || got != want {
		return "", false
	}
	return routed, true
}

func stripAmbientCredentials(r *http.Request) {
	r.Header.Del("Cookie")
	r.Header.Del("Authorization")
	r.Header.Del("X-API-Key")
}

func applyEmbedFraming(w http.ResponseWriter, origins []string) {
	h := w.Header()
	h.Del("X-Frame-Options")
	fields := withFrameAncestorsFields(h.Values("Content-Security-Policy"), origins)
	h.Del("Content-Security-Policy")
	for _, f := range fields {
		h.Add("Content-Security-Policy", f)
	}
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
}

func withFrameAncestorsFields(fields []string, origins []string) []string {
	policies := make([][]string, 0, len(fields))
	found := false
	for _, field := range fields {
		var kept []string
		for p := range strings.SplitSeq(field, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if hasFrameAncestors(p) {
				p = withFrameAncestors(p, origins)
				found = true
			}
			kept = append(kept, p)
		}
		if len(kept) > 0 {
			policies = append(policies, kept)
		}
	}
	if len(policies) == 0 {
		return []string{withFrameAncestors("", origins)}
	}
	if !found {
		policies[0][0] = withFrameAncestors(policies[0][0], origins)
	}
	out := make([]string, len(policies))
	for i, kept := range policies {
		out[i] = strings.Join(kept, ", ")
	}
	return out
}

func hasFrameAncestors(policy string) bool {
	for d := range strings.SplitSeq(policy, ";") {
		if strings.EqualFold(directiveName(d), "frame-ancestors") {
			return true
		}
	}
	return false
}

func directiveName(d string) string {
	if f := strings.Fields(d); len(f) > 0 {
		return f[0]
	}
	return ""
}

func withFrameAncestors(policy string, origins []string) string {
	directive := "frame-ancestors 'none'"
	if len(origins) > 0 {
		directive = "frame-ancestors " + strings.Join(origins, " ")
	}
	if strings.TrimSpace(policy) == "" {
		return directive
	}
	parts := strings.Split(policy, ";")
	out := make([]string, 0, len(parts)+1)
	replaced := false
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		if strings.EqualFold(directiveName(trimmed), "frame-ancestors") {
			out = append(out, directive)
			replaced = true
			continue
		}
		out = append(out, trimmed)
	}
	if !replaced {
		out = append(out, directive)
	}
	return strings.Join(out, "; ")
}

func (ds *UIHost) embedShellOrigins(r *http.Request, s *fembed.ResolvedSurface) []string {
	src := ds.embedHost.OriginSource()
	if src == nil {
		return s.AllowedOrigins()
	}
	resolved, err := fembed.ResolveCustomerOrigins(r.Context(), src, s.Name, r.URL.Query().Get("customer"))
	if err != nil {
		slog.Default().Warn("uihost: embed origin source failed closed: serving frame-ancestors 'none'",
			"surface", s.Name, "err", err)
		return nil
	}
	return resolved
}

func (ds *UIHost) handleEmbedLoaderJS(w http.ResponseWriter, r *http.Request) {
	stripAmbientCredentials(r)
	js, err := runtime.EmbedLoaderJS()
	if err != nil {
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(js))
}

func (ds *UIHost) handleEmbedRuntimeJS(w http.ResponseWriter, r *http.Request) {
	stripAmbientCredentials(r)
	js, err := runtime.EmbedJS()
	if err != nil {
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(js))
	if actions := ds.GetActionJS(); actions != "" {
		_, _ = w.Write([]byte("\n"))
		_, _ = w.Write([]byte(actions))
	}
}

type embedExchangeRequest struct {
	Token  string `json:"token"`
	Origin string `json:"origin"`
}

type embedGrantResponse struct {
	Grant       string `json:"grant"`
	ExpiresInMS int64  `json:"expires_in_ms"`
}

func embedError(w http.ResponseWriter, r *http.Request, status int, why string, err error) {
	if err != nil {
		slog.Default().Debug("uihost: embed request refused", "reason", why, "err", err)
	}
	i18n.HTTPError(w, r, status, i18n.StatusPhrase(status))
}

func (ds *UIHost) handleEmbedExchange(w http.ResponseWriter, r *http.Request) {
	stripAmbientCredentials(r)
	if !ds.embedHost.Ready() {
		embedError(w, r, http.StatusServiceUnavailable, "no signing key", nil)
		return
	}
	var req embedExchangeRequest
	if err := handler.DecodeStrict(http.MaxBytesReader(w, r.Body, 8<<10), &req); err != nil {
		embedError(w, r, http.StatusBadRequest, "malformed body", err)
		return
	}
	res, err := ds.embedHost.Exchange(r.Context(), req.Token, req.Origin)
	if err != nil {
		embedError(w, r, http.StatusUnauthorized, "exchange refused", err)
		return
	}
	if res.Replay {
		slog.Warn("embed: nonce exchanged more than once",
			"surface", res.Surface,
			"origin", req.Origin,
			"hint", "one replay is normal (prefetch, refresh); repeated replays mean "+
				"the page carrying the nonce is cached: mint a fresh nonce per render, "+
				"or every visitor of that cached copy acts as the same subject")
	}
	ds.writeEmbedGrant(w, res.Grant, res.Expires.UnixMilli())
}

func (ds *UIHost) handleEmbedRefresh(w http.ResponseWriter, r *http.Request) {
	stripAmbientCredentials(r)
	if !ds.embedHost.Ready() {
		embedError(w, r, http.StatusServiceUnavailable, "no signing key", nil)
		return
	}
	grant, err := ds.embedHost.Refresh(r.Context(), r.Header.Get(embedGrantHeader))
	if err != nil {
		embedError(w, r, http.StatusUnauthorized, "refresh refused", err)
		return
	}
	ds.writeEmbedGrant(w, grant.Token, grant.Expires.UnixMilli())
}

func (ds *UIHost) writeEmbedGrant(w http.ResponseWriter, grant string, expiresUnixMS int64) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	nowMS := time.Now().UnixMilli()
	ttl := max(expiresUnixMS-nowMS, 0)
	_ = json.NewEncoder(w).Encode(embedGrantResponse{Grant: grant, ExpiresInMS: ttl})
}

func (ds *UIHost) embedGrant(r *http.Request) (fembed.Grant, bool) {
	if ds.embedHost == nil || !ds.embedHost.Ready() {
		return fembed.Grant{}, false
	}
	token := r.Header.Get(embedGrantHeader)
	if token == "" {
		return fembed.Grant{}, false
	}
	g, err := ds.embedHost.VerifyGrant(r.Context(), token)
	if err != nil {
		return fembed.Grant{}, false
	}
	return g, true
}

func (ds *UIHost) embedGrantOK(r *http.Request) bool {
	_, ok := ds.embedGrant(r)
	return ok
}

func (ds *UIHost) embedSurfacePath(g fembed.Grant) string {
	if ds.embedHost == nil {
		return ""
	}
	s, ok := ds.embedHost.Lookup(g.Surface)
	if !ok {
		return ""
	}
	return s.Path()
}

func (ds *UIHost) resolveEmbedSurface(w http.ResponseWriter, r *http.Request) (*fembed.ResolvedSurface, bool) {
	name := r.PathValue("surface")
	s, ok := ds.embedHost.Lookup(name)
	if !ok {
		embedError(w, r, http.StatusNotFound, "unknown surface", nil)
		return nil, false
	}
	return s, true
}

func (ds *UIHost) handleEmbedShell(w http.ResponseWriter, r *http.Request) {
	stripAmbientCredentials(r)
	s, ok := ds.resolveEmbedSurface(w, r)
	if !ok {
		return
	}
	escaped := strings.TrimPrefix(r.URL.EscapedPath(), fembed.SurfacePrefix+s.Name)
	page, ok := ds.embedPagePath(s, escaped)
	if !ok {
		embedError(w, r, http.StatusNotFound, "path does not match the surface", nil)
		return
	}
	content := fembed.SurfacePrefix + s.Name + "/content"
	if s.Dynamic() {
		content += "?path=" + url.QueryEscape(escaped)
	}
	if !ds.embedHost.Ready() {
		embedError(w, r, http.StatusServiceUnavailable, "no signing key", nil)
		return
	}
	applyEmbedFraming(w, ds.embedShellOrigins(r, s))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	cfg := map[string]string{
		"surface":  s.Name,
		"content":  content,
		"exchange": embedExchangePath,
		"refresh":  embedRefreshPath,
		"path":     page,
	}
	cfgJSON, _ := json.Marshal(cfg)

	appCSS := "/__gofastr/app.css"
	variantKey := ds.embedThemeKey(s, r.URL.Query().Get("theme"))
	if variantKey != "" {
		appCSS += "?t=" + variantKey
	}
	shellTheme := ds.activeTheme()
	if variantKey != "" {
		if t, ok := ds.themeVariant(variantKey); ok {
			shellTheme = t
		}
	}

	var b strings.Builder
	lang := ds.LangFor(r.Context(), page)
	b.WriteString("<!DOCTYPE html>\n<html dir=\"" + i18n.Dir(lang) + "\" lang=\"" + stdhtml.EscapeString(lang) + "\">\n<head>\n")
	b.WriteString(`<meta charset="utf-8">` + "\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	b.WriteString(`<meta name="robots" content="noindex, nofollow">` + "\n")
	fmt.Fprintf(&b, `<meta name="gofastr-embed" content="%s">`+"\n", stdhtml.EscapeString(string(cfgJSON)))
	b.WriteString(colorSchemeMeta + "\n")
	b.WriteString(`<script src="/__gofastr/color-scheme.js"></script>` + "\n")
	fmt.Fprintf(&b, `<link rel="stylesheet" href="%s" id="cui-app-css">`+"\n", stdhtml.EscapeString(appCSS))
	if catalog := catalogJSONScriptFor(ds, shellTheme, variantKey); catalog != "" {
		b.WriteString(catalog)
		b.WriteByte('\n')
	}
	if manifest := runtimeModuleManifestScript(); manifest != "" {
		b.WriteString(manifest)
		b.WriteByte('\n')
	}
	if strs := runtimeStringsScript(r.Context()); strs != "" {
		b.WriteString(strs)
		b.WriteByte('\n')
	}
	b.WriteString("</head>\n<body>\n")
	b.WriteString(`<div id="gofastr-embed-root" data-cui-embed-state="loading"></div>` + "\n")
	b.WriteString(`<script src="` + embedRuntimePath + `"></script>` + "\n")
	b.WriteString("</body>\n</html>\n")

	_, _ = w.Write([]byte(b.String()))
}

func (ds *UIHost) embedThemeKey(s *fembed.ResolvedSurface, encoded string) string {
	if encoded == "" || len(s.Theme.AllowTokens) == 0 {
		return ""
	}
	if len(encoded) > maxEmbedThemeParam {
		return ""
	}
	if key, found := ds.embedThemes.lookup(s.Name, encoded); found {
		return key
	}
	max := s.Theme.MaxVariants
	if max <= 0 {
		max = fembed.DefaultMaxThemeVariants
	}
	admitted, evicted, dup := ds.embedThemes.reserve(s.Name, encoded, max)
	for _, key := range evicted {
		ds.ReleaseThemeVariant(key)
	}
	if dup {
		if key, ok := ds.embedThemes.waitFor(s.Name, encoded, embedThemeWait); ok && key != "" {
			return key
		}
		return ""
	}
	if !admitted {
		slog.Default().Warn("uihost: embed theme variants all in flight: rendering under the app theme",
			"surface", s.Name, "max", max)
		return ""
	}
	key, ok := ds.resolveEmbedTheme(s, encoded)
	if !ok {
		ds.embedThemes.release(s.Name, encoded)
		return ""
	}
	ds.embedThemes.record(s.Name, encoded, key)
	return key
}

func (ds *UIHost) resolveEmbedTheme(s *fembed.ResolvedSurface, encoded string) (string, bool) {
	if len(encoded) > maxEmbedThemeParam {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) > 4<<10 {
		return "", false
	}
	var tokens map[string]string
	if err := handler.UnmarshalStrict(raw, &tokens); err != nil || len(tokens) == 0 {
		return "", false
	}
	allowed := make(map[string]struct{}, len(s.Theme.AllowTokens))
	for _, t := range s.Theme.AllowTokens {
		allowed[t] = struct{}{}
	}
	filtered := make(map[string]string, len(tokens))
	for k, v := range tokens {
		if _, ok := allowed[k]; !ok {
			continue
		}
		filtered[k] = v
	}
	if len(filtered) == 0 {
		return "", false
	}
	themed, err := style.ApplyTokens(ds.activeTheme(), filtered)
	if err != nil {
		return "", false
	}
	return ds.RegisterThemeVariant(themed), true
}

func (ds *UIHost) handleEmbedContent(w http.ResponseWriter, r *http.Request) {
	stripAmbientCredentials(r)
	s, ok := ds.resolveEmbedSurface(w, r)
	if !ok {
		return
	}
	if !ds.embedHost.Ready() {
		embedError(w, r, http.StatusServiceUnavailable, "no signing key", nil)
		return
	}
	grant, err := ds.embedHost.VerifyGrant(r.Context(), r.Header.Get(embedGrantHeader))
	if err != nil {
		embedError(w, r, http.StatusUnauthorized, "grant refused", err)
		return
	}
	if grant.Surface != s.Name {
		embedError(w, r, http.StatusNotFound, "grant is for another surface", nil)
		return
	}
	page, ok := ds.embedPagePath(s, r.URL.Query().Get("path"))
	if !ok {
		embedError(w, r, http.StatusNotFound, "path does not match the surface", nil)
		return
	}

	ctx := app.WithRequest(r.Context(), r)
	ctx = store.WithValues(ctx)
	ctx = fembed.WithGrant(ctx, grant)
	ctx, _, reason, err := ds.embedHost.InstallSubject(ctx, grant.Subject)
	if err != nil {
		embedError(w, r, http.StatusForbidden, reason, err)
		return
	}

	res, err := ds.App.RenderPartialResult(ctx, page)
	if err != nil {
		if errors.Is(err, app.ErrScreenPanicked) {
			slog.Default().Error("uihost: embed surface screen render panicked; serving 500",
				"surface", s.Name,
				"path", textsafe.ScrubControlBytes(page),
				"panic", textsafe.Recovered(err))
			ds.serveServerError(w, r, page)
			return
		}
		slog.Default().Warn("uihost: embed surface screen render failed; serving 404",
			"surface", s.Name,
			"path", textsafe.ScrubControlBytes(page),
			"err", textsafe.Recovered(err))
		http.NotFound(w, r)
		return
	}
	switch res.Kind {
	case app.DecisionRedirect:
		embedError(w, r, http.StatusForbidden, "screen policy redirected", nil)
		return
	case app.DecisionBlock:
		status := res.Status
		if status == 0 {
			status = http.StatusForbidden
		}
		embedError(w, r, status, "screen policy blocked", nil)
		return
	}

	applyEmbedFraming(w, []string{grant.Origin})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(app.EmbedLayout().WrapCtx(r.Context(), res.HTML)))
}
