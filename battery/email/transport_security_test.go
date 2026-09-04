package email

import (
	"bufio"
	"context"
	"net"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP is a minimal SMTP server that does NOT advertise STARTTLS.
// It records whether it ever received DATA in cleartext.
type fakeSMTP struct {
	ln        net.Listener
	mu        sync.Mutex
	gotData   bool
	advertise string // extra EHLO capabilities line, empty for none
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

// A server that accepts the dial and then never sends the 220 greeting
// must not wedge the worker: the connection deadline covers the whole
// SMTP exchange, not only the connect.
func TestSMTP_StalledServerDoesNotWedge(t *testing.T) {
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
	s := NewSMTPSender(SMTPConfig{Host: "127.0.0.1", Port: port, AllowCleartext: true, DialTimeout: 300 * time.Millisecond})

	done := make(chan error, 1)
	go func() {
		done <- s.Send(context.Background(), Email{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", TextBody: "b"})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Send succeeded against a server that never spoke SMTP")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Send wedged on a stalled SMTP server — no I/O deadline on the connection")
	}
}

// recordingSMTP is a cleartext fake SMTP server that records the exact
// envelope arguments (MAIL FROM / RCPT TO) the client sends — the wire
// truth — instead of only noting that DATA arrived like fakeSMTP above.
// It advertises no STARTTLS and accepts every command, so Send must run
// with AllowCleartext: true.
type recordingSMTP struct {
	ln       net.Listener
	mu       sync.Mutex
	mailFrom string
	rcptTo   []string
}

func newRecordingSMTP(t *testing.T) *recordingSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &recordingSMTP{ln: ln}
	go s.serve()
	return s
}

func (s *recordingSMTP) port() int {
	_, p, _ := net.SplitHostPort(s.ln.Addr().String())
	n, _ := strconv.Atoi(p)
	return n
}

// senderFor returns an SMTPSender pointed at the recording fake,
// configured the only way Send can complete against it (no STARTTLS
// advertised → AllowCleartext).
func (s *recordingSMTP) senderFor() *SMTPSender {
	return NewSMTPSender(SMTPConfig{
		Host:           "127.0.0.1",
		Port:           s.port(),
		AllowCleartext: true,
		DialTimeout:    5 * time.Second,
	})
}

func (s *recordingSMTP) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *recordingSMTP) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	w := func(str string) { _, _ = conn.Write([]byte(str)) }
	w("220 recording ESMTP\r\n")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		raw := strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(raw)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			w("250 recording\r\n") // no STARTTLS advertised
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			s.mu.Lock()
			s.mailFrom = stripEnvelopeBrackets(raw[len("MAIL FROM:"):])
			s.mu.Unlock()
			w("250 OK\r\n")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			s.mu.Lock()
			s.rcptTo = append(s.rcptTo, stripEnvelopeBrackets(raw[len("RCPT TO:"):]))
			s.mu.Unlock()
			w("250 OK\r\n")
		case strings.HasPrefix(cmd, "DATA"):
			w("354 end data with <CR><LF>.<CR><LF>\r\n")
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
			w("221 bye\r\n")
			return
		default:
			w("250 OK\r\n")
		}
	}
}

func (s *recordingSMTP) recordedMailFrom() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mailFrom
}

func (s *recordingSMTP) recordedRcptTo() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.rcptTo)
}

// stripEnvelopeBrackets removes the single outer <> pair net/smtp's
// Mail/Rcpt always wrap around the argument (MAIL FROM:<%s>), so the
// recorded value is exactly the string Send passed in.
func stripEnvelopeBrackets(arg string) string {
	if len(arg) >= 2 && arg[0] == '<' && arg[len(arg)-1] == '>' {
		return arg[1 : len(arg)-1]
	}
	return arg
}

// envelopeAddrSpecs expands a comma-joined address list exactly the way the
// header side (net/mail) does, returning the bare addr-specs. This is the
// expected envelope expansion; fatal on parse error because every list used
// here is deliberately well-formed.
func envelopeAddrSpecs(t *testing.T, list string) []string {
	t.Helper()
	addrs, err := mail.ParseAddressList(list)
	if err != nil {
		t.Fatalf("net/mail.ParseAddressList(%q): %v", list, err)
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.Address)
	}
	return out
}

// Property: every RCPT TO argument is exactly one net/mail-parsed
// addr-spec. A comma-joined To entry must expand per header-side (net/mail)
// semantics, not be passed whole to client.Rcpt: the envelope recipient set
// must never diverge from the parsed header set — strict MTAs 501 the whole
// send, and a lenient relay that splits the comma delivers to an address
// the application never confirmed as a recipient.
func TestSmtpEnvelopeParsesRecipients(t *testing.T) {
	srv := newRecordingSMTP(t)
	defer srv.ln.Close()

	to := []string{"alice@example.com, attacker@evil.com"}
	if err := srv.senderFor().Send(context.Background(), Email{
		From:     "noreply@example.test",
		To:       to,
		Subject:  "envelope",
		TextBody: "body",
	}); err != nil {
		t.Fatalf("send against recording fake: %v", err)
	}

	want := envelopeAddrSpecs(t, strings.Join(to, ","))
	if got := srv.recordedRcptTo(); !slices.Equal(got, want) {
		t.Errorf("SECURITY: [email-envelope] RCPT args = %q, want the net/mail.ParseAddressList expansion %q", got, want)
	}
}

// Property: display-name forms ("Bob <bob@x>") reach MAIL FROM / RCPT TO as
// bare addr-specs, never as the raw string — net/smtp writes MAIL
// FROM:<%s> with no quoting of its own, so the raw form emits nested angle
// brackets on the wire.
func TestSmtpEnvelopeParsesDisplayNames(t *testing.T) {
	srv := newRecordingSMTP(t)
	defer srv.ln.Close()

	from := "Acme <no-reply@example.com>"
	to := []string{"Bob <bob@example.com>"}
	if err := srv.senderFor().Send(context.Background(), Email{
		From:     from,
		To:       to,
		Subject:  "display names",
		TextBody: "body",
	}); err != nil {
		t.Fatalf("send against recording fake: %v", err)
	}

	if got, want := srv.recordedMailFrom(), envelopeAddrSpecs(t, from)[0]; got != want {
		t.Errorf("SECURITY: [email-envelope] MAIL FROM arg = %q, want bare addr-spec %q", got, want)
	}
	want := envelopeAddrSpecs(t, strings.Join(to, ","))
	if got := srv.recordedRcptTo(); !slices.Equal(got, want) {
		t.Errorf("SECURITY: [email-envelope] RCPT args = %q, want bare addr-specs %q", got, want)
	}
}

// Property: Send never writes into the caller's Email slices. Building the
// recipient list with append(append(email.To, CC...), BCC...)) reuses To's
// spare capacity, so a longer-lived alias over that backing array
// (mail-merge buffer reuse, a sibling slice) observes the BCC address as a
// To entry — BCC disclosure through memory aliasing.
func TestSmtpDoesNotMutateCallerTo(t *testing.T) {
	srv := newRecordingSMTP(t)
	defer srv.ln.Close()

	// len 1, cap 4: Send's append would have spare capacity to write into.
	to := make([]string, 1, 4)
	to[0] = "alice@example.com"

	if err := srv.senderFor().Send(context.Background(), Email{
		From:     "noreply@example.test",
		To:       to,
		CC:       []string{"carol@example.test"},
		BCC:      []string{"hidden-bcc@example.test"},
		Subject:  "aliasing",
		TextBody: "body",
	}); err != nil {
		t.Fatalf("send against recording fake: %v", err)
	}

	// The caller's backing array beyond len(To) must be untouched
	// (still zero values).
	full := to[:cap(to)]
	for i := len(to); i < cap(to); i++ {
		if v := full[i]; v != "" {
			t.Errorf("SECURITY: [email-envelope] caller's To backing array mutated at index %d: %q", i, v)
		}
	}
}
