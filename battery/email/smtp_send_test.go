package email

import (
	"context"
	"errors"
	"testing"
)

func simpleMail() Email {
	return Email{
		From: "a@example.com", To: []string{"b@example.com"},
		Subject: "s", TextBody: "hi",
	}
}

// Once DATA's final 250 arrives the relay has queued the message; a
// QUIT that then fails (EOF, timeout) is not a failed send. Reporting it
// as one made queue retries deliver the same message again.
func TestSendIgnoresQuitAfterAccept(t *testing.T) {
	srv := newRecordingSMTP(t)
	defer srv.ln.Close()
	srv.dropOnQuit = true

	if err := srv.senderFor().Send(context.Background(), simpleMail()); err != nil {
		t.Fatalf("Send after an accepted DATA returned %v; the message is queued", err)
	}
}

// A refusal before acceptance is a failed send and wraps ErrSendFailed,
// as the package promises.
func TestSendRejectedDataWrapsErrSendFailed(t *testing.T) {
	srv := newRecordingSMTP(t)
	defer srv.ln.Close()
	srv.dataReply = "554 rejected"

	err := srv.senderFor().Send(context.Background(), simpleMail())
	if !errors.Is(err, ErrSendFailed) {
		t.Fatalf("Send with DATA refused returned %v, want an ErrSendFailed", err)
	}
}
