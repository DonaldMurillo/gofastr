package notify_test

import (
	"context"
	"errors"
	"github.com/DonaldMurillo/gofastr/battery/notify"
	"testing"
)

func TestBadAttachmentsPreventSend(t *testing.T) {
	for _, value := range []any{"report.pdf", []string{"report.pdf"}, nil} {
		sender := &captureSender{}
		ch := notify.NewEmailChannel(sender, "from@example.com")
		err := ch.Send(context.Background(), notify.Notification{To: notify.Recipient{Email: "to@example.com"}}, notify.Rendered{Subject: "hello", Extra: map[string]any{"attachments": value}})
		if !errors.Is(err, notify.ErrInvalidAttachments) {
			t.Errorf("%T attachments: error %v", value, err)
		}
		if len(sender.last.To) > 0 {
			t.Error("invalid attachment request sent email")
		}
	}
}
