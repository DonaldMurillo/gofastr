package email

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeSMTP is a minimal SMTP server that does NOT advertise STARTTLS.
// It records whether it ever received DATA in cleartext.
type fakeSMTP struct {
	ln          net.Listener
	mu          sync.Mutex
	gotData     bool
	advertise   string // extra EHLO capabilities line, empty for none
}

func newFakeSMTP(t *testing.T, advertise string) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeSMTP{ln: ln, advertise: advertise}
	go f.serve()
	return f
}

func (f *fakeSMTP) addr() (host string, port string) {
	h, p, _ := net.SplitHostPort(f.ln.Addr().String())
	return h, p
}

func (f *fakeSMTP) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	w := func(s string) { _, _ = conn.Write([]byte(s)) }
	w("220 fake ESMTP\r\n")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			// Deliberately do NOT advertise STARTTLS (stripping attack).
			if f.advertise != "" {
				w("250-fake\r\n250 " + f.advertise + "\r\n")
			} else {
				w("250 fake\r\n")
			}
		case strings.HasPrefix(cmd, "MAIL"):
			w("250 OK\r\n")
		case strings.HasPrefix(cmd, "RCPT"):
			w("250 OK\r\n")
		case strings.HasPrefix(cmd, "DATA"):
			f.mu.Lock()
			f.gotData = true
			f.mu.Unlock()
			w("354 End data with <CR><LF>.<CR><LF>\r\n")
			// consume until terminator
			for {
				l, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(l, "\r\n") == "." {
					break
				}
			}
			w("250 OK\r\n")
		case strings.HasPrefix(cmd, "QUIT"):
			w("221 Bye\r\n")
			return
		default:
			w("250 OK\r\n")
		}
	}
}

func (f *fakeSMTP) receivedData() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotData
}

// TestSMTP_NoCleartextWhenSTARTTLSUnavailable asserts that when the
// server cannot offer STARTTLS (e.g. an active MITM stripped the
// capability) the sender refuses to transmit the message in cleartext.
func TestSMTP_NoCleartextWhenSTARTTLSUnavailable(t *testing.T) {
	srv := newFakeSMTP(t, "") // does not advertise STARTTLS
	defer srv.ln.Close()
	host, portStr := srv.addr()

	var port int
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}

	sender := NewSMTPSender(SMTPConfig{
		Host:   host,
		Port:   port,
		UseTLS: false, // opportunistic STARTTLS path
	})

	err := sender.Send(context.Background(), Email{
		From:     "a@b.test",
		To:       []string{"x@y.test"},
		Subject:  "secret",
		TextBody: "confidential body",
	})
	if err == nil {
		t.Fatalf("SECURITY: [email] Send succeeded over cleartext when STARTTLS was unavailable")
	}
	if srv.receivedData() {
		t.Fatalf("SECURITY: [email] message body was transmitted in cleartext (DATA reached server) despite no TLS")
	}
}
