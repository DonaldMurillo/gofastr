package uihost

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// docRoomComp declares the presence topic of the document it shows.
type docRoomComp struct{ id string }

func (c *docRoomComp) SetParams(m map[string]string) { c.id = m["id"] }
func (c *docRoomComp) PresenceTopics(context.Context) []string {
	return []string{"doc:" + c.id}
}
func (c *docRoomComp) Render() render.HTML { return render.HTML("<h1>Doc " + c.id + "</h1>") }

func newDocRoomHost(t *testing.T) *UIHost {
	t.Helper()
	a := app.NewApp("rooms")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/docs/{id}", &docRoomComp{}, nil)
	return New(a)
}

var sseMetaRe = regexp.MustCompile(`<meta name="gofastr-sse" content="([^"]+)">`)

// loadRoomPage renders path for user and returns the page's stream URL and
// its session cookie.
func loadRoomPage(t *testing.T, ds *UIHost, path, user string) (string, *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if user != "" {
		req = req.WithContext(handler.SetUser(req.Context(), &testPresenceUser{id: user, email: user + "@test.com"}))
	}
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", path, rec.Code)
	}
	m := sseMetaRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("GET %s: no gofastr-sse meta", path)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if strings.Contains(c.Name, "gofastr-session") {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("GET %s: no session cookie", path)
	}
	return html.UnescapeString(m[1]), cookie
}

// openStream connects to sseURL as user until the test ends.
func openStream(t *testing.T, ds *UIHost, sseURL string, cookie *http.Cookie, user string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if user != "" {
		ctx = handler.SetUser(ctx, &testPresenceUser{id: user, email: user + "@test.com"})
	}
	req := httptest.NewRequest(http.MethodGet, sseURL, nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "__Host-gofastr-session", Value: cookie.Value})
	done := make(chan struct{})
	go func() { ds.handleSSE(httptest.NewRecorder(), req); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

// waitNoJoin gives a stream time to join, then asserts topic stayed empty.
func waitNoJoin(t *testing.T, ds *UIHost, topic, why string) {
	t.Helper()
	time.Sleep(80 * time.Millisecond)
	if r := ds.Islands.PresenceRoster(topic); len(r) != 0 {
		t.Fatalf("SECURITY: %s joined %s: %v", why, topic, r)
	}
}

// Issue #614 O31: the page names its own presence topics, signed into its
// stream URL, so the viewer joins with no AuthorizeTopic hook.
func TestDeclaredTopicJoins(t *testing.T) {
	ds := newDocRoomHost(t)
	sseURL, cookie := loadRoomPage(t, ds, "/docs/42", "alice")
	if !strings.Contains(sseURL, "declared=doc%3A42") {
		t.Fatalf("stream URL %q does not carry the declared topic", sseURL)
	}
	openStream(t, ds, sseURL, cookie, "alice")
	pollUntil(t, func() bool { return len(ds.Islands.PresenceRoster("doc:42")) == 1 }, time.Second, "declared topic joins")
}

// A client cannot widen the signed list, move it to another user, or keep
// it past its lifetime.
func TestForgedDeclaredRefused(t *testing.T) {
	ds := newDocRoomHost(t)
	sseURL, cookie := loadRoomPage(t, ds, "/docs/42", "alice")

	widened := strings.Replace(sseURL, "declared=doc%3A42", "declared=doc%3A42%2Cdoc%3A7", 1)
	openStream(t, ds, widened, cookie, "alice")
	waitNoJoin(t, ds, "doc:7", "a topic added to the signed list")
	waitNoJoin(t, ds, "doc:42", "a tampered list")

	openStream(t, ds, sseURL, cookie, "bob")
	waitNoJoin(t, ds, "doc:42", "another user's signed list")

	u, _ := url.Parse(sseURL)
	q := u.Query()
	q.Set("dsig", ds.signDeclaredTopics("alice", "doc:42", time.Now().Add(-time.Minute)))
	u.RawQuery = q.Encode()
	openStream(t, ds, u.String(), cookie, "alice")
	waitNoJoin(t, ds, "doc:42", "an expired signature")
}

// A list signed under a key the host still verifies with (a rotation's
// drain window) keeps joining.
func TestDeclaredSurvivesRotation(t *testing.T) {
	ds := newDocRoomHost(t)
	old := []byte("old-session-key-0123456789abcdef") // not-a-secret: test key
	ds.SetSessionKey(old)
	sseURL, _ := loadRoomPage(t, ds, "/docs/42", "alice")
	ds.SetSessionKeys([]byte("new-session-key-0123456789abcdef"), old) // not-a-secret: test key
	sess := ds.CreateSession()
	u, _ := url.Parse(sseURL)
	q := u.Query()
	q.Set("session", sess.ID)
	u.RawQuery = q.Encode()
	openStream(t, ds, u.String(), &http.Cookie{Value: sess.Token}, "alice")
	pollUntil(t, func() bool { return len(ds.Islands.PresenceRoster("doc:42")) == 1 }, time.Second, "list signed by a previous key joins")
}

// A topic named in the URL needs AuthorizeTopic; with no hook it is refused.
func TestPresenceQueryNeedsHook(t *testing.T) {
	ds := newDocRoomHost(t)
	sess := ds.CreateSession()
	openStream(t, ds, "/__gofastr/sse?session="+sess.ID+"&presence=doc:9", &http.Cookie{Value: sess.Token}, "alice")
	waitNoJoin(t, ds, "doc:9", "a ?presence= topic with no hook")

	ds.Islands.SetAuthorizeTopic(func(_ context.Context, topic string) bool { return topic == "doc:9" })
	sess2 := ds.CreateSession()
	openStream(t, ds, "/__gofastr/sse?session="+sess2.ID+"&presence=doc:9", &http.Cookie{Value: sess2.Token}, "alice")
	pollUntil(t, func() bool { return len(ds.Islands.PresenceRoster("doc:9")) == 1 }, time.Second, "hook admits the requested topic")
}

// A screen its policy swaps for an alternate (a sign-in prompt) declares
// nothing: the page drawn is the alt, and the alt names no topic.
func TestRefusedScreenDeclaresNothing(t *testing.T) {
	a := app.NewApp("rooms-guarded")
	a.Register("/", &testHomeComp{}, nil)
	alt := app.PolicyFunc(func(context.Context) app.Decision {
		return app.Decision{Kind: app.DecisionRenderAlt, AltFactory: func() component.Component { return &testHomeComp{} }}
	})
	a.RegisterScreen(app.NewScreen("/docs/{id}", &docRoomComp{}).WithPolicy(alt), nil)
	ds := New(a)
	sseURL, _ := loadRoomPage(t, ds, "/docs/42", "mallory")
	if strings.Contains(sseURL, "declared=") {
		t.Fatalf("SECURITY: the refused screen's topic rode its alternate: %q", sseURL)
	}
}

// missingDocComp found no document the viewer may see: it answers 404 and
// draws its own not-found state, while PresenceTopics still names the id
// from the URL.
type missingDocComp struct{ docRoomComp }

func (c *missingDocComp) ScreenStatusCode() int { return http.StatusNotFound }

// A page that answers 404 refused the viewer, so the topics its screen
// names from the URL stay unsigned.
func TestNotFoundPageDeclaresNothing(t *testing.T) {
	a := app.NewApp("rooms-missing")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/docs/{id}", &missingDocComp{}, nil)
	ds := New(a)
	req := httptest.NewRequest(http.MethodGet, "/docs/42", nil)
	req = req.WithContext(handler.SetUser(req.Context(), &testPresenceUser{id: "mallory", email: "mallory@test.com"}))
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /docs/42: %d, want 404", rec.Code)
	}
	m := sseMetaRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("no gofastr-sse meta on the 404 page")
	}
	if strings.Contains(m[1], "declared=") {
		t.Fatalf("SECURITY: the 404 page signed its screen's topic: %q", m[1])
	}
}
