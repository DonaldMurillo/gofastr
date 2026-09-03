//go:build red

// RED TESTS — open findings, 2026-09-03 round-8 adversarial pass
// (tests-only; no fix applied).
// Property: each SMTP ENVELOPE argument (MAIL FROM, every RCPT TO) is
// exactly one parsed addr-spec, and Send never writes into the caller's
// Email slices. The message HEADERS are a different, already-pinned
// surface (controlbytes_red_test.go, email_security_test.go); the
// envelope is built separately and is entirely unparsed today.
// Surfaces: battery/email/smtp.go:Send — :100 concatenates To/CC/BCC
// raw (and appends into To's spare capacity), :177 client.Mail passes
// From verbatim, :183 client.Rcpt passes each entry verbatim. Go's
// net/smtp validateLine rejects only CR/LF, so commas, spaces, and
// angle brackets all reach the wire inside the envelope argument;
// net/mail is used nowhere in this package.
// Findings (three tests: two envelope input shapes + one aliasing bug):
//  1. A To entry "alice@example.com, attacker@evil.com" becomes ONE
//     RCPT argument with an embedded comma+space. net/mail
//     .ParseAddressList — the header-side semantics for the same
//     string — reads TWO recipients. Envelope set ≠ parsed set:
//     strict MTAs 501 the whole send (availability loss, mail never
//     arrives); a lenient relay that splits on the comma delivers to
//     an address the application never confirmed as a recipient.
//  2. Display-name forms ("Acme <no-reply@x>", "Bob <bob@x>") go
//     verbatim into MAIL FROM / RCPT: nested angle brackets, no
//     addr-spec extraction. Display-name composition ("Full Name
//     <email>" from a user profile) is the most common real-world
//     input shape, so this is the likeliest trigger of the round.
//  3. :100 append(append(email.To, CC...), BCC...) writes CC/BCC
//     strings into To's spare capacity — deterministic caller-visible
//     mutation of memory the caller owns.
//
// Severity: 1 and 2 are production-facing (outgoing mail path; envelope
// arguments are attacker-influencable whenever an app composes
// addresses from user input). The red pins the client-side invariant —
// each envelope arg equals the net/mail-parsed addr-spec — not any
// particular relay's parsing. 3 is MODERATE: the mutation is
// deterministic, but the BCC-disclosure consequence needs a caller that
// aliases To with a longer-lived len (mail-merge buffer reuse,
// sibling slices) — stated plainly, not inflated.
// Fix direction: normalize via net/mail before the envelope — parse
// each From/To/CC/BCC entry and pass the parsed addr.Address (bare
// addr-spec) to client.Mail/client.Rcpt, refusing entries that do not
// parse as exactly one address — and build the recipient list with an
// explicit-length copy so the caller's backing arrays are never
// written. Tests 1 and 2 pass under that single normalization fix.
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

// recordingSMTP is a cleartext fake SMTP server that records the exact
// envelope arguments (MAIL FROM / RCPT TO) the client sends — the wire
// truth — instead of only noting that DATA arrived like
// transport_security_test.go's fakeSMTP. It advertises no STARTTLS and
// accepts every command, so Send must run with AllowCleartext: true.
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

// addrSpecs expands a comma-joined address list exactly the way the
// header side (net/mail) does, returning the bare addr-specs. This is
// the expected envelope expansion; fatal on parse error because every
// list used here is deliberately well-formed.
func addrSpecs(t *testing.T, list string) []string {
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

// TestSmtpRedEnvelopeParsesRecipients: a comma-joined To entry must be
// expanded per net/mail semantics — every RCPT argument exactly one
// addr-spec — not passed whole to client.Rcpt.
func TestSmtpRedEnvelopeParsesRecipients(t *testing.T) {
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

	want := addrSpecs(t, strings.Join(to, ","))
	got := srv.recordedRcptTo()
	if !slices.Equal(got, want) {
		t.Errorf("SECURITY: [email-envelope] RCPT args = %q, want the net/mail.ParseAddressList expansion %q — "+
			"a comma-joined To entry is passed to client.Rcpt as ONE envelope argument "+
			"(smtp.go:100 concatenates raw; :183 emits it verbatim; the package never parses), "+
			"so the envelope recipient set diverges from the parsed header set: strict MTAs reject "+
			"the whole send, a lenient relay that splits the comma delivers to an address the "+
			"application never confirmed",
			got, want)
	}
}

// TestSmtpRedEnvelopeParsesDisplayNames: display-name forms in From/To
// must reach MAIL FROM / RCPT as bare addr-specs (net/mail-parsed), not
// as the raw "Name <addr>" string — the wire shows nested angle
// brackets today. Same fix as the comma shape, different input form and
// different envelope line (MAIL FROM included).
func TestSmtpRedEnvelopeParsesDisplayNames(t *testing.T) {
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

	if got, want := srv.recordedMailFrom(), addrSpecs(t, from)[0]; got != want {
		t.Errorf("SECURITY: [email-envelope] MAIL FROM arg = %q, want bare addr-spec %q — "+
			"the display-name From string is passed verbatim to client.Mail (smtp.go:177), "+
			"emitting MAIL FROM:<Acme <no-reply@…>> with nested brackets instead of the parsed addr-spec",
			got, want)
	}
	want := addrSpecs(t, strings.Join(to, ","))
	if got := srv.recordedRcptTo(); !slices.Equal(got, want) {
		t.Errorf("SECURITY: [email-envelope] RCPT args = %q, want bare addr-specs %q — "+
			"a display-name To entry is passed verbatim to client.Rcpt (smtp.go:100/:183), "+
			"emitting RCPT TO:<Bob <bob@…>> with nested brackets instead of the parsed addr-spec",
			got, want)
	}
}

// TestSmtpRedDoesNotMutateCallerTo: Send must not write CC/BCC values
// into the caller's To backing array. smtp.go:100's
// append(append(email.To, CC...), BCC...) reuses To's spare capacity,
// so any longer-lived alias over that array observes the BCC address as
// a To entry — BCC disclosure through memory aliasing.
func TestSmtpRedDoesNotMutateCallerTo(t *testing.T) {
	srv := newRecordingSMTP(t)
	defer srv.ln.Close()

	// len 1, cap 4: Send's append has spare capacity to write into.
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
	// (still zero values). Today it holds the CC and BCC strings.
	full := to[:cap(to)]
	for i := len(to); i < cap(to); i++ {
		if v := full[i]; v != "" {
			t.Errorf("SECURITY: [email-envelope] caller's To backing array mutated at index %d: %q — "+
				"append(append(email.To, CC...), BCC...) at smtp.go:100 writes CC/BCC values past "+
				"len(To) into caller-owned memory; a longer-lived alias over the array (mail-merge "+
				"buffer reuse, a sibling slice) observes the BCC address as a To entry",
				i, v)
		}
	}
}
