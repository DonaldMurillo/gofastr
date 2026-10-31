package email

import (
	"bufio"
	"context"
	"encoding/base64"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// authSMTP is a fake relay that advertises a fixed AUTH list, refuses
// any mechanism outside it with 504, and refuses MAIL until a login
// succeeded.
type authSMTP struct {
	ln    net.Listener
	mechs string // the AUTH line's mechanisms, "" for no AUTH extension

	mu    sync.Mutex
	tried []string  // mechanisms named by AUTH commands, in order
	plain string    // the decoded PLAIN response
	login [2]string // the decoded LOGIN username and password
	data  bool
}

func newAuthSMTP(t *testing.T, mechs string) *authSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &authSMTP{ln: ln, mechs: mechs}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.handle(c)
		}
	}()
	return f
}

func (f *authSMTP) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	w := func(s string) { _, _ = conn.Write([]byte(s)) }
	authed := false
	w("220 fake ESMTP\r\n")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			if f.mechs == "" {
				w("250 fake\r\n")
			} else {
				w("250-fake\r\n250 AUTH " + f.mechs + "\r\n")
			}
		case strings.HasPrefix(cmd, "AUTH "):
			fields := strings.Fields(line)
			mech := strings.ToUpper(fields[1])
			f.mu.Lock()
			f.tried = append(f.tried, mech)
			f.mu.Unlock()
			if !strings.Contains(" "+strings.ToUpper(f.mechs)+" ", " "+mech+" ") {
				w("504 5.5.4 Unrecognized authentication type\r\n")
				continue
			}
			switch mech {
			case "PLAIN":
				raw, _ := base64.StdEncoding.DecodeString(fields[2])
				f.mu.Lock()
				f.plain = string(raw)
				f.mu.Unlock()
			case "LOGIN":
				var got [2]string
				for i, prompt := range []string{"Username:", "Password:"} {
					w("334 " + base64.StdEncoding.EncodeToString([]byte(prompt)) + "\r\n")
					l, err := br.ReadString('\n')
					if err != nil {
						return
					}
					raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(l))
					got[i] = string(raw)
				}
				f.mu.Lock()
				f.login = got
				f.mu.Unlock()
			case "CRAM-MD5":
				w("334 " + base64.StdEncoding.EncodeToString([]byte("<1.2@fake>")) + "\r\n")
				if _, err := br.ReadString('\n'); err != nil {
					return
				}
			}
			authed = true
			w("235 2.7.0 Authentication successful\r\n")
		case strings.HasPrefix(cmd, "MAIL"):
			if !authed {
				w("530 5.7.0 Authentication required\r\n")
				continue
			}
			w("250 OK\r\n")
		case strings.HasPrefix(cmd, "DATA"):
			w("354 go ahead\r\n")
			for {
				l, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(l, "\r\n") == "." {
					break
				}
			}
			f.mu.Lock()
			f.data = true
			f.mu.Unlock()
			w("250 OK\r\n")
		case strings.HasPrefix(cmd, "QUIT"):
			w("221 Bye\r\n")
			return
		default:
			w("250 OK\r\n")
		}
	}
}

func (f *authSMTP) send(t *testing.T) error {
	t.Helper()
	host, p, _ := net.SplitHostPort(f.ln.Addr().String())
	port, _ := strconv.Atoi(p)
	s := NewSMTPSender(SMTPConfig{
		Host:           host,
		Port:           port,
		Username:       "user",
		Password:       "pw", // not-a-secret: test fixture
		AllowCleartext: true,
	})
	return s.Send(context.Background(), Email{
		From:     "a@b.test",
		To:       []string{"x@y.test"},
		Subject:  "hi",
		TextBody: "body",
	})
}

// SES and Gmail advertise PLAIN and LOGIN, no CRAM-MD5. A refused
// mechanism makes net/smtp abort and QUIT, so the sender must pick an
// advertised one up front rather than try another on the same client.
func TestSMTPAuthPlainOnlyRelay(t *testing.T) {
	f := newAuthSMTP(t, "PLAIN LOGIN")
	if err := f.send(t); err != nil {
		t.Fatalf("send to a PLAIN LOGIN relay: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.tried, ",") != "PLAIN" {
		t.Fatalf("AUTH tried %v, want only PLAIN", f.tried)
	}
	if f.plain != "\x00user\x00pw" {
		t.Fatalf("PLAIN response %q", f.plain)
	}
	if !f.data {
		t.Fatal("the message never reached DATA")
	}
}

func TestSMTPAuthCRAMOnlyRelay(t *testing.T) {
	f := newAuthSMTP(t, "CRAM-MD5")
	if err := f.send(t); err != nil {
		t.Fatalf("send to a CRAM-MD5 relay: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.tried, ",") != "CRAM-MD5" {
		t.Fatalf("AUTH tried %v, want only CRAM-MD5", f.tried)
	}
}

// Office 365 advertises LOGIN and XOAUTH2 only.
func TestSMTPAuthLoginOnlyRelay(t *testing.T) {
	f := newAuthSMTP(t, "LOGIN XOAUTH2")
	if err := f.send(t); err != nil {
		t.Fatalf("send to a LOGIN XOAUTH2 relay: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.tried, ",") != "LOGIN" {
		t.Fatalf("AUTH tried %v, want only LOGIN", f.tried)
	}
	if f.login != [2]string{"user", "pw"} {
		t.Fatalf("LOGIN sent %q", f.login)
	}
}

// With every mechanism offered, CRAM-MD5 goes first: it never puts the
// password on the wire, and it was the sender's choice before.
func TestSMTPAuthPrefersCRAM(t *testing.T) {
	f := newAuthSMTP(t, "LOGIN PLAIN CRAM-MD5")
	if err := f.send(t); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.tried, ",") != "CRAM-MD5" {
		t.Fatalf("AUTH tried %v, want only CRAM-MD5", f.tried)
	}
}

// Credentials are configured and the relay offers no mechanism the
// sender speaks: refuse, naming what the relay offered, before MAIL.
func TestSMTPAuthNoUsableMechanism(t *testing.T) {
	f := newAuthSMTP(t, "XOAUTH2 GSSAPI")
	err := f.send(t)
	if err == nil || !strings.Contains(err.Error(), "XOAUTH2 GSSAPI") {
		t.Fatalf("got %v, want an error naming the offered mechanisms", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tried) != 0 || f.data {
		t.Fatalf("tried %v, data %v: want no AUTH and no DATA", f.tried, f.data)
	}
}

// PLAIN goes before LOGIN when both are offered: one round trip for
// the same credentials.
func TestSMTPAuthPrefersPlainOverLogin(t *testing.T) {
	f := newAuthSMTP(t, "LOGIN PLAIN")
	if err := f.send(t); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.tried, ",") != "PLAIN" {
		t.Fatalf("AUTH tried %v, want only PLAIN", f.tried)
	}
}

// LOGIN sends the password as PLAIN does, so it refuses where
// smtp.PlainAuth refuses: cleartext to another machine, or a host the
// credentials were not configured for.
func TestSMTPLoginAuthRefusals(t *testing.T) {
	a := &loginAuth{username: "user", password: "pw", host: "smtp.example"} // not-a-secret: test fixture
	if _, _, err := a.Start(&smtp.ServerInfo{Name: "smtp.example"}); err == nil {
		t.Fatal("LOGIN started over cleartext to a remote host")
	}
	if _, _, err := a.Start(&smtp.ServerInfo{Name: "other.example", TLS: true}); err == nil {
		t.Fatal("LOGIN started for a host it was not configured for")
	}
	if _, _, err := a.Start(&smtp.ServerInfo{Name: "smtp.example", TLS: true}); err != nil {
		t.Fatalf("LOGIN over TLS: %v", err)
	}
	for _, want := range []string{"user", "pw"} {
		got, err := a.Next([]byte("prompt"), true)
		if err != nil || string(got) != want {
			t.Fatalf("Next = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := a.Next([]byte("again"), true); err == nil {
		t.Fatal("a third challenge was answered")
	}
}

// The relay's AUTH words reach the error; only mechanism-shaped words do.
func TestSMTPAuthErrorDropsOddWords(t *testing.T) {
	s := NewSMTPSender(SMTPConfig{Host: "h", Username: "u", Password: "p"}) // not-a-secret: test fixture
	_, err := s.pickAuth("XOAUTH2 \x1b[31mred GSSAPI")
	if err == nil || strings.ContainsRune(err.Error(), 0x1b) || !strings.Contains(err.Error(), "XOAUTH2 GSSAPI") {
		t.Fatalf("got %q", err)
	}
}
