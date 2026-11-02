package webhook

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// deliverOnce subscribes sub on a fresh MemoryStore manager, publishes
// one event and runs one tick, returning the stored delivery.
func deliverOnce(t *testing.T, sub Subscriber, opts Options) Delivery {
	t.Helper()
	opts.AllowPrivateNetworks = true
	opts.AllowPlaintextAuth = true // httptest serves plain HTTP
	opts.MaxAttempts = 3
	opts.Backoff = []time.Duration{time.Hour}
	store := NewMemoryStore()
	mgr := New(store, opts)
	ctx := context.Background()
	if sub.Secret == "" {
		sub.Secret = "shh"
	}
	saved, err := mgr.Subscribe(ctx, sub)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if n, err := mgr.Publish(ctx, "orders.created", []byte(`{"id":1}`)); err != nil || n != 1 {
		t.Fatalf("publish = %d, %v", n, err)
	}
	mgr.tick(ctx)
	ds, err := store.ListDeliveries(ctx, saved.ID, 0)
	if err != nil || len(ds) != 1 {
		t.Fatalf("deliveries = %v, %v", ds, err)
	}
	return ds[0]
}

// headerCatcher records the headers of each request it receives.
type headerCatcher struct {
	mu   sync.Mutex
	seen []http.Header
}

func (c *headerCatcher) handler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.seen = append(c.seen, r.Header.Clone())
		c.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func (c *headerCatcher) last(t *testing.T) http.Header {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.seen) == 0 {
		t.Fatal("the receiver got no request")
	}
	return c.seen[len(c.seen)-1]
}

// Each auth kind arrives in the header a receiver checks, beside the
// signature, which stays.
func TestDeliverySendsSubscriberAuth(t *testing.T) {
	cases := []struct {
		name   string
		auth   Auth
		header string
		want   string
	}{
		{"bearer", Auth{Kind: AuthBearer, Token: "tok-1"}, "Authorization", "Bearer tok-1"},
		{"api key default header", Auth{Kind: AuthAPIKey, Token: "key-1"}, "X-API-Key", "key-1"},
		{"api key custom header", Auth{Kind: AuthAPIKey, Token: "key-2", Header: "X-Receiver-Key"}, "X-Receiver-Key", "key-2"},
		{"basic", Auth{Kind: AuthBasic, Username: "svc", Password: "pw"}, "Authorization",
			"Basic " + base64.StdEncoding.EncodeToString([]byte("svc:pw"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c headerCatcher
			srv := httptest.NewServer(c.handler(http.StatusNoContent, ""))
			defer srv.Close()
			d := deliverOnce(t, Subscriber{URL: srv.URL, Auth: tc.auth}, Options{})
			h := c.last(t)
			if got := h.Get(tc.header); got != tc.want {
				t.Fatalf("%s = %q; want %q", tc.header, got, tc.want)
			}
			if h.Get(SignatureHeader) == "" {
				t.Fatal("the signature header went missing beside the credential")
			}
			if d.Status != StatusSuccess || d.ResponseStatus != http.StatusNoContent {
				t.Fatalf("delivery = %s / %d; want success / 204", d.Status, d.ResponseStatus)
			}
		})
	}
}

// A credential-bearing delivery never follows a redirect, even through
// a caller-supplied client that does: the redirect target would get the
// API key (Go strips Authorization across hosts, not custom headers).
func TestDeliveryWithAuthRefusesRedirect(t *testing.T) {
	var elsewhere headerCatcher
	other := httptest.NewServer(elsewhere.handler(http.StatusOK, ""))
	defer other.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	following := &http.Client{Timeout: 5 * time.Second} // follows redirects
	d := deliverOnce(t, Subscriber{URL: redirector.URL, Auth: Auth{Kind: AuthAPIKey, Token: "key-1"}},
		Options{HTTPClient: following})
	elsewhere.mu.Lock()
	n := len(elsewhere.seen)
	elsewhere.mu.Unlock()
	if n != 0 {
		t.Fatalf("the redirect target received %d request(s) carrying the credential", n)
	}
	if d.Status == StatusSuccess || d.ResponseStatus != http.StatusTemporaryRedirect {
		t.Fatalf("delivery = %s / %d; want a failed attempt recording the 307", d.Status, d.ResponseStatus)
	}
}

// A failed attempt keeps the status and the start of the body, so a
// refusal can be diagnosed; the stored text is capped and cannot carry
// a control byte to an operator's terminal.
func TestDeliveryRecordsResponse(t *testing.T) {
	var c headerCatcher
	body := "invalid api key\x1b[31m" + strings.Repeat("x", 4000)
	srv := httptest.NewServer(c.handler(http.StatusUnauthorized, body))
	defer srv.Close()
	d := deliverOnce(t, Subscriber{URL: srv.URL}, Options{ResponseBodyLimit: 64})
	if d.ResponseStatus != http.StatusUnauthorized || d.LastError != "http 401" {
		t.Fatalf("delivery = %d %q; want 401 and http 401", d.ResponseStatus, d.LastError)
	}
	if !strings.HasPrefix(d.ResponseBody, "invalid api key") {
		t.Fatalf("ResponseBody = %q; want the start of the receiver's body", d.ResponseBody)
	}
	if strings.ContainsRune(d.ResponseBody, 0x1b) {
		t.Fatalf("ResponseBody kept a raw ESC: %q", d.ResponseBody)
	}
	if len(d.ResponseBody) > 64+8 { // the ESC grows to %1b
		t.Fatalf("ResponseBody is %d bytes; want about the 64-byte limit", len(d.ResponseBody))
	}

	d = deliverOnce(t, Subscriber{URL: srv.URL}, Options{ResponseBodyLimit: -1})
	if d.ResponseBody != "" || d.ResponseStatus != http.StatusUnauthorized {
		t.Fatalf("ResponseBodyLimit -1 kept %q / %d; want no body, the status kept", d.ResponseBody, d.ResponseStatus)
	}
}

func TestSubscribeRefusesMalformedAuth(t *testing.T) {
	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})
	bad := map[string]Auth{
		"unknown kind":        {Kind: "digest", Token: "x"},
		"credential, no kind": {Token: "x"},
		"bearer no token":     {Kind: AuthBearer},
		"bearer with header":  {Kind: AuthBearer, Token: "x", Header: "X-Key"},
		"api key no token":    {Kind: AuthAPIKey},
		"crlf in token":       {Kind: AuthBearer, Token: "x\r\nX-Evil: 1"}, // not-a-secret: test fixture credential
		"bad header name":     {Kind: AuthAPIKey, Token: "x", Header: "X Key"},
		"host header":         {Kind: AuthAPIKey, Token: "x", Header: "Host"},
		"content-type":        {Kind: AuthAPIKey, Token: "x", Header: "content-type"},
		"gofastr header":      {Kind: AuthAPIKey, Token: "x", Header: "X-GoFastr-Event"},
		"signature header":    {Kind: AuthAPIKey, Token: "x", Header: SignatureHeader},
		"basic no user":       {Kind: AuthBasic, Password: "p"},
		"basic colon user":    {Kind: AuthBasic, Username: "a:b", Password: "p"},
		"basic with token":    {Kind: AuthBasic, Username: "a", Password: "p", Token: "x"},
		"basic with header":   {Kind: AuthBasic, Username: "a", Password: "p", Header: "X-Key"},
		"no kind, header":     {Header: "X-Key"},
		"no kind, username":   {Username: "a"},
		"no kind, password":   {Password: "p"},
		"crlf in username":    {Kind: AuthBasic, Username: "a\r\nX-Evil", Password: "p"},
		"crlf in password":    {Kind: AuthBasic, Username: "a", Password: "p\r\nX-Evil: 1"}, // not-a-secret: test fixture credential
	}
	for name, a := range bad {
		_, err := mgr.Subscribe(context.Background(), Subscriber{URL: "http://127.0.0.1:1/", Secret: "s", Auth: a})
		if err == nil {
			t.Errorf("%s: Subscribe accepted %+v", name, a)
			continue
		}
		if a.Token != "" && strings.Contains(err.Error(), a.Token) {
			t.Errorf("%s: the error echoes the credential: %v", name, err)
		}
	}
}

// SQLStore seals the credential with the secret codec: the column never
// holds the token, and it reads back whole.
func TestSQLStoreSealsAuth(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	codec, err := NewAESGCMSecretCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSQLStore(db, WithSQLSecretCodec(codec))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	want := Auth{Kind: AuthAPIKey, Token: "very-secret-key", Header: "X-Receiver-Key"} // not-a-secret: test fixture credential
	if err := s.AddSubscriber(ctx, Subscriber{ID: "s1", URL: "https://example.com/h", Secret: "shh",
		Events: []string{"*"}, Active: true, Created: time.Now(), Auth: want}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow("SELECT auth FROM webhook_subscribers WHERE id = 's1'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "" || strings.Contains(raw, "very-secret-key") || strings.Contains(raw, "X-Receiver-Key") {
		t.Fatalf("auth column = %q; want it sealed", raw)
	}
	got, err := s.GetSubscriber(ctx, "s1")
	if err != nil || got == nil || got.Auth != want {
		t.Fatalf("GetSubscriber auth = %+v, %v; want %+v", got, err, want)
	}
	all, err := s.ListSubscribers(ctx)
	if err != nil || len(all) != 1 || all[0].Auth != want {
		t.Fatalf("ListSubscribers auth = %+v, %v", all, err)
	}
	// A subscriber without auth stores "" and reads back none.
	if err := s.AddSubscriber(ctx, Subscriber{ID: "s2", URL: "https://example.com/h", Secret: "shh",
		Events: []string{"*"}, Active: true, Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetSubscriber(ctx, "s2"); err != nil || got.Auth != (Auth{}) {
		t.Fatalf("no-auth subscriber read back %+v, %v", got, err)
	}
}

// A database an earlier release created gains the new columns at open.
func TestSQLStoreUpgradesOldTables(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`CREATE TABLE webhook_subscribers (id TEXT PRIMARY KEY, url TEXT NOT NULL, secret TEXT NOT NULL,
			events TEXT NOT NULL, active INTEGER NOT NULL, created DATETIME NOT NULL)`,
		`CREATE TABLE webhook_deliveries (id TEXT PRIMARY KEY, subscriber_id TEXT NOT NULL, event TEXT NOT NULL,
			payload BLOB NOT NULL, attempts INTEGER NOT NULL, status TEXT NOT NULL, last_error TEXT NOT NULL,
			next_attempt_at DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`INSERT INTO webhook_subscribers VALUES ('old', 'https://example.com', 'shh', '["*"]', 1, '2026-01-01 00:00:00')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewSQLStore(db, WithSQLAllowPlaintext())
	if err != nil {
		t.Fatalf("open over an old schema: %v", err)
	}
	ctx := context.Background()
	if got, err := s.GetSubscriber(ctx, "old"); err != nil || got == nil || got.Auth != (Auth{}) {
		t.Fatalf("old subscriber = %+v, %v; want it read back with no auth", got, err)
	}
	d := Delivery{ID: "d1", SubscriberID: "old", Event: "e", Payload: []byte("{}"), Status: StatusFailed,
		ResponseStatus: 503, ResponseBody: "busy", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.AddDelivery(ctx, d); err != nil {
		t.Fatal(err)
	}
	ds, err := s.ListDeliveries(ctx, "old", 0)
	if err != nil || len(ds) != 1 || ds[0].ResponseStatus != 503 || ds[0].ResponseBody != "busy" {
		t.Fatalf("delivery read back %+v, %v; want status 503 and body busy", ds, err)
	}
}

// A Manager not built by New still gets a client that refuses
// redirects, never a nil dereference on the worker.
func TestCredentialClientWithoutNew(t *testing.T) {
	for _, m := range []*Manager{{}, {opts: Options{HTTPClient: &http.Client{}}}} {
		c := m.credentialClient()
		if c == nil || c.CheckRedirect == nil {
			t.Fatalf("credentialClient = %+v; want a client that refuses redirects", c)
		}
	}
}

// A credential never crosses the network in clear text unless the
// manager opts in: Subscribe refuses an http:// URL with Auth, and a
// subscriber that reached the store anyway is not sent to.
func TestPlaintextAuthRefused(t *testing.T) {
	ctx := context.Background()
	strict := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})
	_, err := strict.Subscribe(ctx, Subscriber{URL: "http://127.0.0.1:1/h", Secret: "s", Auth: Auth{Kind: AuthBearer, Token: "tok-secret"}}) // not-a-secret: test fixture credential
	if err == nil || strings.Contains(err.Error(), "tok-secret") {
		t.Fatalf("Subscribe(http + Auth) = %v; want a refusal that does not echo the token", err)
	}
	if _, err := strict.Subscribe(ctx, Subscriber{URL: "https://93.184.216.34/h", Secret: "s", Auth: Auth{Kind: AuthBearer, Token: "x"}}); err != nil {
		t.Fatalf("Subscribe(https + Auth) = %v; want accepted", err)
	}
	if _, err := strict.Subscribe(ctx, Subscriber{URL: "http://127.0.0.1:1/h", Secret: "s"}); err != nil {
		t.Fatalf("Subscribe(http, no Auth) = %v; plain HTTP without a credential stays allowed", err)
	}
	loose := New(NewMemoryStore(), Options{AllowPrivateNetworks: true, AllowPlaintextAuth: true})
	if _, err := loose.Subscribe(ctx, Subscriber{URL: "http://127.0.0.1:1/h", Secret: "s", Auth: Auth{Kind: AuthBearer, Token: "x"}}); err != nil {
		t.Fatalf("Subscribe with AllowPlaintextAuth = %v; want accepted", err)
	}

	// A subscriber written to the store directly is still not sent to.
	var c headerCatcher
	srv := httptest.NewServer(c.handler(http.StatusOK, ""))
	defer srv.Close()
	store := NewMemoryStore()
	if err := store.AddSubscriber(ctx, Subscriber{ID: "direct", URL: srv.URL, Secret: "s", Events: []string{"*"}, Active: true,
		Created: time.Now(), Auth: Auth{Kind: AuthAPIKey, Token: "key-secret"}}); err != nil { // not-a-secret: test fixture credential
		t.Fatal(err)
	}
	mgr := New(store, Options{AllowPrivateNetworks: true, MaxAttempts: 3, Backoff: []time.Duration{time.Hour}})
	if _, err := mgr.Publish(ctx, "orders.created", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	mgr.tick(ctx)
	c.mu.Lock()
	n := len(c.seen)
	c.mu.Unlock()
	if n != 0 {
		t.Fatalf("the receiver got %d plain-HTTP request(s) carrying the credential", n)
	}
	ds, _ := store.ListDeliveries(ctx, "direct", 0)
	if len(ds) != 1 || ds[0].Status == StatusSuccess || ds[0].LastError != errPlaintextAuth.Error() {
		t.Fatalf("delivery = %+v; want a failed attempt naming the refusal", ds)
	}
}

// A receiver that echoes the request's credential in its body must not
// put it in the stored, unsealed ResponseBody.
func TestDeliveryRedactsEchoedCredential(t *testing.T) {
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad key " + r.Header.Get("X-API-Key") + " / " + r.Header.Get("Authorization")))
	}))
	defer echo.Close()
	for name, a := range map[string]Auth{
		"api key": {Kind: AuthAPIKey, Token: "key-very-secret"},                   // not-a-secret: test fixture credential
		"bearer":  {Kind: AuthBearer, Token: "tok-very-secret"},                   // not-a-secret: test fixture credential
		"basic":   {Kind: AuthBasic, Username: "svc", Password: "pw-very-secret"}, // not-a-secret: fixture credential the echo test must see redacted
	} {
		d := deliverOnce(t, Subscriber{URL: echo.URL, Auth: a}, Options{})
		basic := base64.StdEncoding.EncodeToString([]byte("svc:pw-very-secret"))
		if strings.Contains(d.ResponseBody, "very-secret") || strings.Contains(d.ResponseBody, basic) {
			t.Errorf("%s: ResponseBody kept the echoed credential: %q", name, d.ResponseBody)
		}
		if !strings.Contains(d.ResponseBody, redactedCredential) {
			t.Errorf("%s: ResponseBody = %q; want the echo replaced by %s", name, d.ResponseBody, redactedCredential)
		}
	}
}
