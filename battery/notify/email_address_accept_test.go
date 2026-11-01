package notify_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/notify"
)

// The email channel accepts every address the SMTP battery sends to:
// an apostrophe is legal in a local part (o'brien@example.com) and a
// From may carry a display name. The channel's own list of refused
// characters dropped both before the sender saw them. It still refuses
// anything that is not exactly one mailbox.
func TestEmailChannelAcceptsSMTPAddresses(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ to, from string }{
		{"o'brien@example.com", "from@example.com"},
		{"Ada O'Neil <ada@example.com>", "App <noreply@example.com>"},
		{"first.last+tag@example.com", `"Support Team" <help@example.com>`},
	} {
		sender := &captureSender{}
		ch := notify.NewEmailChannel(sender, c.from)
		err := ch.Send(context.Background(), notify.Notification{To: notify.Recipient{Email: c.to}}, notify.Rendered{Subject: "hi"})
		if err != nil {
			t.Errorf("to %q from %q: %v", c.to, c.from, err)
			continue
		}
		if len(sender.last.To) != 1 || sender.last.To[0] != c.to || sender.last.From != c.from {
			t.Errorf("sender got to=%q from=%q", sender.last.To, sender.last.From)
		}
	}
	for _, to := range []string{"a@example.com, b@example.com", "no-at-sign", `"a>b"@example.com`, ""} {
		sender := &captureSender{}
		ch := notify.NewEmailChannel(sender, "from@example.com")
		err := ch.Send(context.Background(), notify.Notification{To: notify.Recipient{Email: to}}, notify.Rendered{Subject: "hi"})
		if to == "" {
			continue // no recipient: the channel skips the send
		}
		if !errors.Is(err, notify.ErrUnsafeRecipient) || len(sender.last.To) != 0 {
			t.Errorf("to %q: err = %v, sender got %q; want ErrUnsafeRecipient and no send", to, err, sender.last.To)
		}
	}
}
