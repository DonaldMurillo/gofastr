package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const formatSecret = "whsec-test-0123456789" // not-a-secret: test fixture

// acmeFormat is the receiver scheme issue #680 names.
var acmeFormat = SignatureFormat{
	Header:          "X-Acme-Signature",
	TimestampHeader: "X-Acme-Timestamp",
	Prefix:          "sha256=",
	Signed:          "{timestamp}.{body}",
}

func hexMAC(secret, material string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(material))
	return hex.EncodeToString(m.Sum(nil))
}

func TestDeliverySignsInReceiverFormat(t *testing.T) {
	var (
		mu  sync.Mutex
		got http.Header
		raw []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		raw, _ = io.ReadAll(r.Body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	f := acmeFormat
	mgr := New(NewMemoryStore(), Options{
		MaxAttempts: 1, Backoff: []time.Duration{0}, PollInterval: 5 * time.Millisecond,
		AllowPrivateNetworks: true, Signature: &f,
	})
	mgr.Start()
	defer mgr.Stop(context.Background())
	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{URL: srv.URL, Secret: formatSecret}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Publish(ctx, "order.created", []byte(`{"id":7}`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return got != nil }, 2*time.Second, "no delivery")

	mu.Lock()
	defer mu.Unlock()
	ts := got.Get("X-Acme-Timestamp")
	if _, err := strconv.ParseInt(ts, 10, 64); err != nil {
		t.Fatalf("timestamp header %q is not unix seconds", ts)
	}
	if want := "sha256=" + hexMAC(formatSecret, ts+"."+string(raw)); got.Get("X-Acme-Signature") != want {
		t.Fatalf("signature %q, want %q", got.Get("X-Acme-Signature"), want)
	}
	for _, h := range []string{SignatureHeader, "X-GoFastr-Timestamp"} {
		if v := got.Get(h); v != "" {
			t.Errorf("%s sent as %q beside the receiver format", h, v)
		}
	}
	if got.Get(EventHeader) != "order.created" || got.Get(DeliveryIDHeader) == "" {
		t.Errorf("event/id headers: %q %q", got.Get(EventHeader), got.Get(DeliveryIDHeader))
	}
	if !f.Verify(formatSecret, got, raw, time.Minute) {
		t.Error("delivery did not verify against its own format")
	}
}

func TestDefaultFormatIsSignWithTimestamp(t *testing.T) {
	body := []byte(`{"id":1}`)
	if a, b := defaultSignatureFormat.Sign(formatSecret, 1700000000, "a.b", "d1", body),
		SignWithTimestamp(formatSecret, 1700000000, "a.b", "d1", body); a != b {
		t.Fatalf("default format %q, SignWithTimestamp %q", a, b)
	}
	if err := defaultSignatureFormat.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFormatValidateRefuses(t *testing.T) {
	ok := acmeFormat
	cases := map[string]func(*SignatureFormat){
		"no header":           func(f *SignatureFormat) { f.Header = "" },
		"bad header name":     func(f *SignatureFormat) { f.Header = "X Acme" },
		"reserved header":     func(f *SignatureFormat) { f.Header = "content-type" },
		"duplicate header":    func(f *SignatureFormat) { f.TimestampHeader = "x-acme-signature" },
		"event on event id":   func(f *SignatureFormat) { f.EventHeader = DeliveryIDHeader },
		"no body":             func(f *SignatureFormat) { f.Signed = "{timestamp}" },
		"body twice":          func(f *SignatureFormat) { f.Signed = "{timestamp}.{body}{body}" },
		"no timestamp":        func(f *SignatureFormat) { f.Signed = "{body}" },
		"unknown placeholder": func(f *SignatureFormat) { f.Signed = "{timestamp}.{id}.{body}" },
		"unclosed brace":      func(f *SignatureFormat) { f.Signed = "{timestamp}.{body" },
		"stray close brace":   func(f *SignatureFormat) { f.Signed = "{timestamp}}.{body}" },
		"prefix body":         func(f *SignatureFormat) { f.Prefix = "{body}" },
		"prefix ts twice":     func(f *SignatureFormat) { f.Prefix = "{timestamp}{timestamp}" },
		"prefix control byte": func(f *SignatureFormat) { f.Prefix = "sha256=\n" },
		"timestamp unreachable": func(f *SignatureFormat) {
			f.TimestampHeader = ""
		},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid format refused: %v", err)
	}
	for name, mutate := range cases {
		f := acmeFormat
		mutate(&f)
		if err := f.Validate(); err == nil {
			t.Errorf("%s: accepted %+v", name, f)
		}
	}
}

func TestNewPanicsOnBadFormat(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New accepted a format with no {body}")
		}
	}()
	New(NewMemoryStore(), Options{Signature: &SignatureFormat{Header: "X-Sig", TimestampHeader: "X-Ts", Signed: "{timestamp}"}})
}

func TestFormatVerifyRefusesTampering(t *testing.T) {
	body := []byte(`{"id":1}`)
	now := time.Now().Unix()
	signed := func(f SignatureFormat, ts int64) http.Header {
		h := http.Header{}
		f.setHeaders(h, formatSecret, ts, "order.created", "d1", body)
		return h
	}
	h := signed(acmeFormat, now)
	if !acmeFormat.Verify(formatSecret, h, body, time.Minute) {
		t.Fatal("genuine request refused")
	}
	if acmeFormat.Verify(formatSecret, h, []byte(`{"id":2}`), time.Minute) {
		t.Error("altered body verified")
	}
	if acmeFormat.Verify("other-secret-0123", h, body, time.Minute) {
		t.Error("wrong secret verified")
	}
	if acmeFormat.Verify(formatSecret, signed(acmeFormat, now-3600), body, time.Minute) {
		t.Error("hour-old timestamp verified")
	}
	moved := h.Clone()
	moved.Set("X-Acme-Timestamp", strconv.FormatInt(now+1, 10))
	if acmeFormat.Verify(formatSecret, moved, body, time.Minute) {
		t.Error("timestamp header moved off the signed value verified")
	}
	plus := h.Clone()
	plus.Set("X-Acme-Timestamp", "+"+h.Get("X-Acme-Timestamp"))
	if acmeFormat.Verify(formatSecret, plus, body, time.Minute) {
		t.Error("signed timestamp spelled with a + verified")
	}
	if acmeFormat.Verify(" ", h, body, time.Minute) || acmeFormat.Verify(formatSecret, h, body, 0) {
		t.Error("blank secret or zero tolerance verified")
	}
}

func TestFormatVerifyPrefixTimestamp(t *testing.T) {
	stripe := SignatureFormat{Header: "X-Sig", Prefix: "t={timestamp},v1=", Signed: "{timestamp}.{event}.{body}"}
	body := []byte(`{}`)
	now := time.Now().Unix()
	h := http.Header{}
	stripe.setHeaders(h, formatSecret, now, "ping", "d1", body)
	if !strings.HasPrefix(h.Get("X-Sig"), "t="+strconv.FormatInt(now, 10)+",v1=") {
		t.Fatalf("signature %q does not carry its timestamp", h.Get("X-Sig"))
	}
	if !stripe.Verify(formatSecret, h, body, time.Minute) {
		t.Fatal("genuine request refused")
	}
	h.Set(EventHeader, "pong")
	if stripe.Verify(formatSecret, h, body, time.Minute) {
		t.Error("relabelled event verified")
	}
	both := stripe
	both.TimestampHeader = "X-Ts"
	hb := http.Header{}
	both.setHeaders(hb, formatSecret, now, "ping", "d1", body)
	if !both.Verify(formatSecret, hb, body, time.Minute) {
		t.Fatal("genuine request with both timestamps refused")
	}
	hb.Set("X-Ts", strconv.FormatInt(now+1, 10))
	if both.Verify(formatSecret, hb, body, time.Minute) {
		t.Error("timestamp header disagreeing with the signed one verified")
	}
	req := httptest.NewRequest(http.MethodPost, "/hook", nil)
	stripe.setHeaders(req.Header, formatSecret, now, "ping", "d1", body)
	if err := stripe.Verifier(formatSecret, time.Minute)(req, body); err != nil {
		t.Fatalf("Verifier refused a genuine request: %v", err)
	}
}

func TestSubscribeRefusesAuthOnSigHeader(t *testing.T) {
	f := acmeFormat
	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true, Signature: &f})
	_, err := mgr.Subscribe(context.Background(), Subscriber{
		URL: "https://example.com/hook", Secret: formatSecret,
		Auth: Auth{Kind: AuthAPIKey, Header: "x-acme-signature", Token: "key-0123456789"}, // not-a-secret: test fixture
	})
	if err != errAuthOnSignatureHeader {
		t.Fatalf("Subscribe err = %v, want errAuthOnSignatureHeader", err)
	}
}

func TestStoredAuthOnSigHeaderFails(t *testing.T) {
	var hits sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Store("hit", true)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	store := NewMemoryStore()
	f := acmeFormat
	mgr := New(store, Options{
		MaxAttempts: 1, Backoff: []time.Duration{0}, PollInterval: 5 * time.Millisecond,
		AllowPrivateNetworks: true, AllowPlaintextAuth: true, Signature: &f,
	})
	ctx := context.Background()
	// Stored before the format named this header: Subscribe never saw it.
	if err := store.AddSubscriber(ctx, Subscriber{
		ID: "s1", URL: srv.URL, Secret: formatSecret, Events: []string{"*"}, Active: true,
		Auth: Auth{Kind: AuthAPIKey, Header: "X-Acme-Timestamp", Token: "key-0123456789"}, // not-a-secret: test fixture
	}); err != nil {
		t.Fatal(err)
	}
	mgr.Start()
	defer mgr.Stop(context.Background())
	if _, err := mgr.Publish(ctx, "order.created", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		ds, _ := store.ListDeliveries(ctx, "s1", 1)
		return len(ds) == 1 && ds[0].Status == StatusDead
	}, 2*time.Second, "delivery never settled dead")
	ds, _ := store.ListDeliveries(ctx, "s1", 1)
	if ds[0].LastError != errAuthOnSignatureHeader.Error() {
		t.Errorf("LastError %q", ds[0].LastError)
	}
	if _, hit := hits.Load("hit"); hit {
		t.Error("the receiver was sent a delivery whose credential overwrote a signature header")
	}
}

func TestPrefixTimestampAtPrefixEnd(t *testing.T) {
	f := SignatureFormat{Header: "X-Sig", Prefix: "v1,{timestamp}", Signed: "{timestamp}.{body}"}
	body := []byte(`{}`)
	now := time.Now().Unix()
	// Find a timestamp whose MAC starts with a digit, the case a greedy
	// digit scan misreads.
	for i := int64(0); i < 64; i++ {
		h := http.Header{}
		f.setHeaders(h, formatSecret, now+i%3, "ping", "d1", body)
		mac := h.Get("X-Sig")[len("v1,")+len(strconv.FormatInt(now+i%3, 10)):]
		if mac[0] < '0' || mac[0] > '9' {
			body = append(body, ' ')
			continue
		}
		if !f.Verify(formatSecret, h, body, time.Minute) {
			t.Fatalf("genuine signature %q refused", h.Get("X-Sig"))
		}
		return
	}
	t.Fatal("no MAC starting with a digit found")
}
