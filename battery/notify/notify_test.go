package notify

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// ----- routing --------------------------------------------------------------

func TestDefaultRouter_SelectsByRecipientFields(t *testing.T) {
	all := []string{"log", "email", "sms", "webhook", "push", "inapp"}

	cases := []struct {
		name string
		to   Recipient
		want []string
	}{
		{"emailOnly", Recipient{Email: "a@b.c"}, []string{"log", "email", "inapp"}},
		{"smsOnly", Recipient{Phone: "+1"}, []string{"log", "sms", "inapp"}},
		{"webhookOnly", Recipient{Webhook: "https://x"}, []string{"log", "webhook", "inapp"}},
		{"pushOnly", Recipient{PushTokens: []string{"t1"}}, []string{"log", "push", "inapp"}},
		{"none", Recipient{UserID: "u1"}, []string{"log", "inapp"}},
		{"all", Recipient{Email: "a", Phone: "p", Webhook: "w", PushTokens: []string{"t"}}, all},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultRouter("anything", tc.to, all)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

// ----- MapTemplater --------------------------------------------------------

func TestMapTemplater_InterpolatesPlaceholders(t *testing.T) {
	m := NewMapTemplater()
	m.Set("order.shipped", "email", Template{
		Subject:  "Order #{{id}} shipped",
		TextBody: "Hi {{name}}, your order is on its way.",
	})
	r, err := m.Render(context.Background(), "order.shipped", "email", map[string]any{
		"id":   42,
		"name": "Alice",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if r.Subject != "Order #42 shipped" {
		t.Fatalf("subject: %q", r.Subject)
	}
	if r.TextBody != "Hi Alice, your order is on its way." {
		t.Fatalf("text: %q", r.TextBody)
	}
}

func TestMapTemplater_MissingTemplateError(t *testing.T) {
	m := NewMapTemplater()
	if _, err := m.Render(context.Background(), "no.such", "email", nil); err == nil {
		t.Fatal("expected error for missing template")
	}
}

// ----- Notifier end-to-end --------------------------------------------------

// recordingChannel captures every Send for assertions in tests.
type recordingChannel struct {
	mu       sync.Mutex
	name     string
	calls    []Notification
	rendered []Rendered
	failWith error
}

func (c *recordingChannel) Name() string { return c.name }
func (c *recordingChannel) Send(_ context.Context, n Notification, r Rendered) error {
	c.mu.Lock()
	c.calls = append(c.calls, n)
	c.rendered = append(c.rendered, r)
	c.mu.Unlock()
	return c.failWith
}

func TestNotifier_FansOutToSelectedChannels(t *testing.T) {
	emailCh := &recordingChannel{name: "email"}
	smsCh := &recordingChannel{name: "sms"}
	logCh := &recordingChannel{name: "log"}

	tmpl := NewMapTemplater()
	tmpl.Set("welcome", "email", Template{Subject: "Hi {{name}}", TextBody: "Welcome!"})
	tmpl.Set("welcome", "sms", Template{TextBody: "Welcome, {{name}}"})
	tmpl.Set("welcome", "log", Template{TextBody: "log: welcome"})

	n := New(
		WithTemplater(tmpl),
		WithChannel(emailCh),
		WithChannel(smsCh),
		WithChannel(logCh),
	)

	err := n.Send(context.Background(), Notification{
		Type: "welcome",
		To:   Recipient{Email: "a@b.c", Phone: "+1", UserID: "u"},
		Data: map[string]any{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(emailCh.calls) != 1 || emailCh.rendered[0].Subject != "Hi Alice" {
		t.Fatalf("email not delivered: %+v", emailCh.rendered)
	}
	if len(smsCh.calls) != 1 || smsCh.rendered[0].TextBody != "Welcome, Alice" {
		t.Fatalf("sms not delivered: %+v", smsCh.rendered)
	}
	if len(logCh.calls) != 1 {
		t.Fatalf("log not delivered")
	}
}

func TestNotifier_SkipsChannelsWithoutAddress(t *testing.T) {
	emailCh := &recordingChannel{name: "email"}
	smsCh := &recordingChannel{name: "sms"}
	tmpl := NewMapTemplater()
	tmpl.Set("x", "email", Template{TextBody: "e"})
	tmpl.Set("x", "sms", Template{TextBody: "s"})

	n := New(WithTemplater(tmpl), WithChannel(emailCh), WithChannel(smsCh))
	_ = n.Send(context.Background(), Notification{
		Type: "x",
		To:   Recipient{Email: "a@b.c"}, // no phone
		Data: map[string]any{},
	})
	if len(emailCh.calls) != 1 {
		t.Fatalf("expected email delivery")
	}
	if len(smsCh.calls) != 0 {
		t.Fatalf("sms should NOT fire without phone; got %d", len(smsCh.calls))
	}
}

func TestNotifier_PerChannelErrorIsObservableAndContinues(t *testing.T) {
	good := &recordingChannel{name: "log"}
	bad := &recordingChannel{name: "email", failWith: errors.New("smtp down")}
	tmpl := NewMapTemplater()
	tmpl.Set("x", "email", Template{TextBody: "e"})
	tmpl.Set("x", "log", Template{TextBody: "l"})

	var observed atomic.Int32
	n := New(
		WithTemplater(tmpl),
		WithChannel(good),
		WithChannel(bad),
		WithErrorCallback(func(channel string, _ Notification, err error) {
			if channel == "email" && err != nil {
				observed.Add(1)
			}
		}),
	)
	err := n.Send(context.Background(), Notification{
		Type: "x",
		To:   Recipient{Email: "a@b.c"},
	})
	if err == nil {
		t.Fatalf("expected Send to surface the email error")
	}
	if !strings.Contains(err.Error(), "smtp down") {
		t.Fatalf("error should wrap channel error: %v", err)
	}
	if observed.Load() != 1 {
		t.Fatalf("error callback never fired")
	}
	if len(good.calls) != 1 {
		t.Fatalf("log channel should still have fired despite email failure")
	}
}

func TestNotifier_NoChannelsSelectedReturnsErr(t *testing.T) {
	tmpl := NewMapTemplater()
	tmpl.Set("x", "email", Template{TextBody: "e"})
	n := New(
		WithTemplater(tmpl),
		WithChannel(&recordingChannel{name: "email"}),
	)
	err := n.Send(context.Background(), Notification{Type: "x", To: Recipient{UserID: "u1"}})
	if !errors.Is(err, ErrNoChannels) {
		t.Fatalf("expected ErrNoChannels, got %v", err)
	}
}

func TestNotifier_PreRenderedPayloadShortCircuitsTemplater(t *testing.T) {
	ch := &recordingChannel{name: "log"}
	// No templater installed — pre-rendered payload must be used.
	n := New(WithChannel(ch))
	err := n.Send(context.Background(), Notification{
		Type: "anything",
		To:   Recipient{UserID: "u"},
		Data: map[string]any{
			"_rendered": Rendered{TextBody: "pre-rendered hello"},
		},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if ch.rendered[0].TextBody != "pre-rendered hello" {
		t.Fatalf("pre-rendered payload not used: %+v", ch.rendered[0])
	}
}

// ----- LoggerChannel --------------------------------------------------------

func TestLoggerChannel_WritesToProvidedLogger(t *testing.T) {
	var buf bytes.Buffer
	l := log.New(&buf, "", 0)
	c := NewLoggerChannel(l)
	if c.Name() != "log" {
		t.Fatalf("name: %q", c.Name())
	}
	_ = c.Send(context.Background(), Notification{
		Type: "evt",
		To:   Recipient{UserID: "u", Email: "x@y.z"},
	}, Rendered{Subject: "hi", TextBody: "body"})
	if !strings.Contains(buf.String(), "type=evt") || !strings.Contains(buf.String(), "subject=\"hi\"") {
		t.Fatalf("log channel didn't write expected line: %q", buf.String())
	}
}
