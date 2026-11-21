package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/DonaldMurillo/gofastr/battery/email"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// EmailPreview is one mail the Emails page renders with sample data.
type EmailPreview struct {
	// Name is the mail's unique tab and URL key, 1–64 lowercase letters,
	// digits, "-" or "_", starting with a letter or digit.
	Name string

	// Render builds the mail from sample data; it runs unelevated, and an
	// error or a panic draws a generic notice.
	Render func(ctx context.Context) (email.Email, error)
}

var emailPreviewName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func checkEmailPreviews(previews []EmailPreview) error {
	seen := map[string]bool{}
	for i, p := range previews {
		if !emailPreviewName.MatchString(p.Name) {
			return fmt.Errorf("admin: EmailPreviews[%d].Name %q must be lowercase letters, digits, - and _", i, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("admin: EmailPreviews[%d].Name %q is used twice", i, p.Name)
		}
		seen[p.Name] = true
		if p.Render == nil {
			return fmt.Errorf("admin: EmailPreviews[%d] (%s) has no Render", i, p.Name)
		}
	}
	return nil
}

const emailFrameCSP = "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src https: data:; " +
	"font-src https: data:; frame-ancestors 'self'; base-uri 'none'; form-action 'none'"

func (b *Battery) emailPreview(name string) (EmailPreview, bool) {
	for _, p := range b.cfg.EmailPreviews {
		if p.Name == name {
			return p, true
		}
	}
	return EmailPreview{}, false
}

func (b *Battery) renderEmail(ctx context.Context, p EmailPreview) (msg email.Email, err error) {
	defer func() {
		reason := ""
		if rec := recover(); rec != nil {
			msg, err, reason = email.Email{}, errPreviewPanic, "panic"
		} else if err != nil {
			reason = fmt.Sprintf("render: %T", err)
		}
		if reason != "" {
			b.logger().Error("admin: email preview failed", "preview", p.Name, "reason", reason)
		}
	}()
	return p.Render(crud.WithoutElevation(ctx))
}

var errPreviewPanic = errors.New("admin: email preview panicked")

func (b *Battery) renderEmails(ctx context.Context, _ map[string]string) render.HTML {
	sel := b.cfg.EmailPreviews[0]
	if r := appui.RequestFromContext(ctx); r != nil {
		if p, ok := b.emailPreview(r.URL.Query().Get("name")); ok {
			sel = p
		}
	}
	tabs := make([]ui.TabNavItem, len(b.cfg.EmailPreviews))
	for i, p := range b.cfg.EmailPreviews {
		tabs[i] = ui.TabNavItem{
			Text:    p.Name,
			Href:    b.cfg.PathPrefix + "/emails?name=" + urlQueryEscape(p.Name),
			Current: p.Name == sel.Name,
		}
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.AdminEmails.Text(ctx),
			Subtitle: i18nui.AdminEmailsSub.Text(ctx),
		}),
		ui.TabNav(ui.TabNavConfig{Items: tabs, Label: i18nui.AdminEmailTemplates.Text(ctx)}),
		b.emailDetail(ctx, sel),
	)
}

func (b *Battery) emailDetail(ctx context.Context, p EmailPreview) render.HTML {
	msg, err := b.renderEmail(ctx, p)
	if err != nil {
		return ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.AdminEmailFailed.Text(ctx)))
	}
	none := func(key i18n.Phrase) render.HTML {
		return ui.Callout(ui.CalloutConfig{Variant: ui.StatusInfo}, render.Text(key.Text(ctx)))
	}
	htmlBody := none(i18nui.AdminEmailNoHTML)
	if msg.HTMLBody != "" {
		htmlBody = ui.AspectRatioComponent(ui.AspectRatioConfig{Ratio: ui.AspectRatio4_3},
			html.IFrame(html.IFrameConfig{
				Src:   b.cfg.PathPrefix + "/_email/" + pathSegment(p.Name),
				Title: i18nui.AdminEmailHTML.Text(ctx),
			}))
	}
	textBody := none(i18nui.AdminEmailNoText)
	if msg.TextBody != "" {
		textBody = ui.CodeBlock(ui.CodeBlockConfig{Ctx: ctx, Code: msg.TextBody, Language: "text", Scroll: true, Wrap: true})
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.DetailList(ui.DetailListConfig{Items: []ui.DetailItem{
			{Label: i18nui.AdminEmailSubject.Text(ctx), Value: render.Text(msg.Subject)},
		}}),
		ui.Card(ui.CardConfig{Heading: i18nui.AdminEmailHTML.Text(ctx), HeadingLevel: 2}, htmlBody),
		ui.Card(ui.CardConfig{Heading: i18nui.AdminEmailText.Text(ctx), HeadingLevel: 2}, textBody),
	)
}

func (b *Battery) mountEmails(r *router.Router) {
	if len(b.cfg.EmailPreviews) == 0 {
		return
	}
	r.Get(b.cfg.PathPrefix+"/_email/{name}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := b.emailPreview(r.PathValue("name"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		msg, err := b.renderEmail(r.Context(), p)
		if err != nil {
			i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		if msg.HTMLBody == "" {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", emailFrameCSP)
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(msg.HTMLBody))
	}))
}
