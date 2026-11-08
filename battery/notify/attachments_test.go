package notify_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/email"
	"github.com/DonaldMurillo/gofastr/battery/notify"
)

func TestEmailChannelCarriesAttachments(t *testing.T) {
	attachments := []email.Attachment{
		{Filename: "report.csv", Content: []byte("a,b\n1,2"), ContentType: "text/csv"},
		{Filename: "photo.png", Content: []byte{1, 2, 3}, ContentType: "image/png"},
	}
	sender := &captureSender{}
	ch := notify.NewEmailChannel(sender, "from@example.com")
	err := ch.Send(context.Background(), notify.Notification{To: notify.Recipient{Email: "to@example.com"}}, notify.Rendered{Extra: map[string]any{"attachments": attachments}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sender.last.Attachments, attachments) {
		t.Fatalf("attachments = %#v, want %#v", sender.last.Attachments, attachments)
	}
}

func TestNilAttachmentSliceIsAccepted(t *testing.T) {
	sender := &captureSender{}
	ch := notify.NewEmailChannel(sender, "from@example.com")
	err := ch.Send(context.Background(), notify.Notification{To: notify.Recipient{Email: "to@example.com"}}, notify.Rendered{Extra: map[string]any{"attachments": []email.Attachment(nil)}})
	if err != nil || len(sender.last.To) != 1 {
		t.Fatalf("typed nil attachments: %v, email %#v", err, sender.last)
	}
}
