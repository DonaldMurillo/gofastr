package notify

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

var welcomeSubject = i18n.Define("notify_test.welcome", "Welcome")

type phraseTemplater struct{}

func (phraseTemplater) Render(ctx context.Context, _, _ string, _ map[string]any) (Rendered, error) {
	return Rendered{Subject: welcomeSubject.Text(ctx, nil)}, nil
}

type subjectChannel struct{ subject, tag string }

func (c *subjectChannel) Name() string { return "log" }
func (c *subjectChannel) Send(ctx context.Context, _ Notification, r Rendered) error {
	c.subject, c.tag = r.Subject, i18n.FromContext(ctx).Tag
	return nil
}

// The message speaks the recipient's language, not the sender's: a
// French admin notifying a German user sends German. A tag the catalog
// lacks falls back; no tag keeps the sender's.
func TestRecipientLocaleWins(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "notify_test.welcome", i18n.Message{Text: "Bienvenue"})
	cat.Set("de", "notify_test.welcome", i18n.Message{Text: "Willkommen"})
	ctx := i18n.WithTranslator(context.Background(), i18n.NewTranslator(cat, "en"))
	ctx = i18n.WithContext(ctx, i18n.Locale{Tag: "fr"})

	for locale, want := range map[string]string{"de-AT": "Willkommen", "ja": "Welcome", "": "Bienvenue"} {
		c := &subjectChannel{}
		n := New(WithChannel(c), WithTemplater(phraseTemplater{}))
		if err := n.Send(ctx, Notification{Type: "welcome", To: Recipient{Locale: locale}}); err != nil {
			t.Fatal(err)
		}
		if c.subject != want {
			t.Errorf("Locale %q: subject = %q, want %q", locale, c.subject, want)
		}
	}
}
