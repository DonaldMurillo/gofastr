package email

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"
)

// Once connected, Send ignored its context: net/smtp takes none, and only
// the I/O deadline (10 s by default) ended a stalled exchange, so a
// cancelled send held the mail queue's shutdown and could still deliver
// and report success if the relay resumed first (round 7 R7-077).
// Cancelling now closes the connection and Send returns the ctx error.
func TestSMTPCancelEndsStalledSend(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	stall := make(chan struct{})
	defer close(stall)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-stall // accept, then never send the greeting
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	// The I/O deadline is far past the cancel, so only the cancel can end it.
	s := NewSMTPSender(SMTPConfig{Host: "127.0.0.1", Port: port, AllowCleartext: true, DialTimeout: 10 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Send(ctx, Email{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", TextBody: "b"})
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Send = %v, want an error wrapping context.Canceled", err)
		}
		if !errors.Is(err, ErrSendFailed) {
			t.Fatalf("Send = %v, want it to wrap ErrSendFailed too", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled Send kept waiting on a stalled relay")
	}
}
