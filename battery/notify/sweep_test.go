package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/email"
)

type panicChannel struct{}

func (panicChannel) Name() string { return "log" }
func (panicChannel) Send(context.Context, Notification, Rendered) error {
	panic("channel boom")
}

type panicTemplater struct{}

func (panicTemplater) Render(context.Context, string, string, map[string]any) (Rendered, error) {
	panic("templater boom")
}

type panicSender struct{}

func (panicSender) Send(context.Context, email.Email) error { panic("sender boom") }

func TestPanickingHostCodeBecomesAnError(t *testing.T) {
	ctx := context.Background()
	pre := map[string]any{"_rendered": Rendered{Subject: "s"}}
	msg := Notification{Type: "t", To: Recipient{Email: "a@example.com"}, Data: pre}
	cases := map[string]struct {
		n    *Notifier
		msg  Notification
		want string
	}{
		"router": {New(WithChannel(NewLoggerChannel(nil)), WithRouter(func(string, Recipient, []string) []string {
			panic("router boom")
		})), msg, "router panic"},
		"templater": {New(WithChannel(NewLoggerChannel(nil)), WithTemplater(panicTemplater{})),
			Notification{Type: "t"}, "templater panicked"},
		"channel":  {New(WithChannel(panicChannel{})), msg, "channel send panicked"},
		"sender":   {New(WithChannel(NewEmailChannel(panicSender{}, "from@example.com"))), msg, "sender panicked"},
		"callback": {New(WithChannel(panicChannel{}), WithErrorCallback(func(string, Notification, error) { panic("cb boom") })), msg, "error callback"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.n.Send(ctx, tc.msg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Send = %v, want an error naming %q", err, tc.want)
			}
		})
	}
}

func TestPerChannelPreRenderedWinsOverShared(t *testing.T) {
	ch := &recordingChannel{name: "log"}
	err := New(WithChannel(ch)).Send(context.Background(), Notification{Type: "t", Data: map[string]any{
		"_rendered":     Rendered{TextBody: "shared"},
		"_rendered_log": Rendered{TextBody: "per-channel"},
	}})
	if err != nil || ch.rendered[0].TextBody != "per-channel" {
		t.Fatalf("Send = %v, rendered %+v, want the _rendered_log payload", err, ch.rendered)
	}
}

func TestHTMLBodyWithNULIsRefused(t *testing.T) {
	m := NewMapTemplater()
	m.Set("t", "email", Template{HTMLBody: "\x00[ print \"injected\" ]\x00 {{name}}"})
	r, err := m.Render(context.Background(), "t", "email", map[string]any{"name": "a"})
	if err == nil {
		t.Fatalf("Render = %q, want an error for a NUL in host markup", r.HTMLBody)
	}
}

func TestEmailChannelRejectsColonInHeaderName(t *testing.T) {
	ch := NewEmailChannel(panicSender{}, "from@example.com")
	err := ch.Send(context.Background(), Notification{To: Recipient{Email: "a@example.com"}}, Rendered{
		Extra: map[string]any{"headers": map[string]string{"X-A: b\tBcc": "1"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsafe header") {
		t.Fatalf("Send = %v, want ErrUnsafeHeader for a colon in a header name", err)
	}
}
