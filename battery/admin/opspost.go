package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/url"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const maxAdminBodyBytes int64 = 1 << 20

func isRPC(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

func (b *Battery) readOps(w http.ResponseWriter, r *http.Request, page string) (url.Values, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBodyBytes)
	tooLarge := func(err error) bool {
		_, ok := errors.AsType[*http.MaxBytesError](err)
		return ok
	}
	if isRPC(r) {
		var raw map[string]any
		//gofastr:allow(floatnum) only strings and string arrays become form values; a number is refused
		if err := handler.DecodeStrict(r.Body, &raw); err != nil {
			if tooLarge(err) {
				i18n.HTTPError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
				return nil, false
			}
			b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
			return nil, false
		}
		vals := url.Values{}
		for k, v := range raw {
			switch t := v.(type) {
			case string:
				vals.Set(k, t)
			case []any:
				for _, item := range t {
					s, ok := item.(string)
					if !ok {
						b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
						return nil, false
					}
					vals.Add(k, s)
				}
			default:
				b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
				return nil, false
			}
		}
		return vals, true
	}
	if err := r.ParseForm(); err != nil {
		if tooLarge(err) {
			i18n.HTTPError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
			return nil, false
		}
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return nil, false
	}
	return r.PostForm, true
}

type result struct {
	key i18n.Phrase
	ok  bool
}

var results = map[string]result{
	"done":                   {i18nui.AdminDone, true},
	"replayed":               {i18nui.AdminReplayed, true},
	"replayed-all":           {i18nui.AdminReplayedAll, true},
	"replayed-some":          {i18nui.AdminReplayedSome, true},
	"granted":                {i18nui.AdminGranted, true},
	"permissions-saved":      {i18nui.AdminPermissionsSaved, true},
	"roles-saved":            {i18nui.AdminRolesSaved, true},
	"name-saved":             {i18nui.AdminNameSaved, true},
	"module-enabled":         {i18nui.AdminModuleEnabled, true},
	"module-disabled":        {i18nui.AdminModuleDisabled, true},
	"module-bumped":          {i18nui.AdminModuleBumped, true},
	"module-revoked":         {i18nui.AdminModuleRevoked, true},
	"refused":                {i18nui.AdminRefused, false},
	"failed":                 {i18nui.AdminFailed, false},
	"bad-input":              {i18nui.AdminBadInput, false},
	"grant-refused":          {i18nui.AdminGrantRefused, false},
	"assign-refused":         {i18nui.AdminAssignRefused, false},
	"own-admin-refused":      {i18nui.AdminOwnAdminRefused, false},
	"own-permission-refused": {i18nui.AdminOwnPermRefused, false},
	"name-refused":           {i18nui.AdminNameRefused, false},
	"unknown-capability":     {i18nui.AdminUnknownCapability, false},
	"revoked-by-wildcard":    {i18nui.AdminRevokedByWildcard, false},
	"module-failed":          {i18nui.AdminModuleFailed, false},
	"module-refused":         {i18nui.AdminModuleRefused, false},
	"queue-load-failed":      {i18nui.AdminQueueLoadFailed, false},
	"replay-unsupported":     {i18nui.AdminFailed, false},
	"replay-duplicate":       {i18nui.AdminReplayDuplicate, false},
	"replay-nothing":         {replayNothing, false},
}

func (b *Battery) done(w http.ResponseWriter, r *http.Request, page, name string) {
	res := results[name]
	if isRPC(r) {
		ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusSuccess, Title: res.key.Text(r.Context()), TTL: 4000})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, withResult(page, name), http.StatusSeeOther)
}

func (b *Battery) refuse(w http.ResponseWriter, r *http.Request, page string, status int, name string, fields ...string) {
	res := results[name]
	if isRPC(r) {
		message := res.key.Text(r.Context())
		body := map[string]any{"error": message}
		if len(fields) > 0 {
			fieldErrors := make(map[string]string, len(fields))
			for _, field := range fields {
				fieldErrors[field] = message
			}
			body["fields"] = fieldErrors
		}
		w.Header().Set("Cache-Control", "no-store")
		encoded, err := json.Marshal(body)
		if err != nil {
			slog.Warn("admin: encode refusal body", "error", err)
			handler.WriteError(w, r, handler.Fail(http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError)))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		w.Write(encoded)
		w.Write([]byte("\n"))
		return
	}
	http.Redirect(w, r, withResult(page, name), http.StatusSeeOther)
}

func withResult(page, name string) string {
	u, err := url.Parse(page)
	if err != nil {
		return page
	}
	q := u.Query()
	q.Set("result", name)
	u.RawQuery = q.Encode()
	return u.String()
}

func resultNotice(ctx context.Context) render.HTML {
	r := appui.RequestFromContext(ctx)
	if r == nil || r.URL == nil {
		return ""
	}
	res, ok := results[r.URL.Query().Get("result")]
	if !ok {
		return ""
	}
	variant := ui.StatusSuccess
	if !res.ok {
		variant = ui.StatusDanger
	}
	return ui.Callout(ui.CalloutConfig{Variant: variant}, render.Text(res.key.Text(ctx)))
}
