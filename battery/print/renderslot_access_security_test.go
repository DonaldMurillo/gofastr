package print

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// servePDF took a render slot before it ran the access policy, so every
// request the policy was about to refuse held one of the battery-wide
// slots for as long as the policy ran. RequireOwner's owns callback is a
// data lookup; a signed-in caller asking for other people's documents
// (each a 403) kept the slots busy, and the owners' own PDFs answered 503.
// Property: only a request the policy admits can occupy a render slot.
func TestPDFRefusedRequestHoldsNoRenderSlot(t *testing.T) {
	arrived, release := make(chan struct{}), make(chan struct{})
	policy := func(r *http.Request) (int, i18n.Phrase) {
		if r.Header.Get("X-Who") == "stranger" {
			arrived <- struct{}{}
			<-release
			return http.StatusForbidden, i18n.Phrase{}
		}
		return 0, i18n.Phrase{}
	}
	b := New(Config{MaxConcurrentRenders: 1, PDFRenderer: &fakeRenderer{pdf: []byte("%PDF-1.4")}}).
		Document(Document{Name: "invoice", Path: "/invoice", Access: policy, Build: docBuild("<p>x</p>")})
	h := mount(t, b)

	refused := make(chan int, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/print/invoice/pdf", nil)
		req.Header.Set("X-Who", "stranger")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		refused <- rec.Code
	}()
	<-arrived // the stranger's request is inside the access policy

	owner := get(t, h, "/print/invoice/pdf")
	close(release)
	if owner.Code != http.StatusOK {
		t.Errorf("owner's PDF while a refused request was being checked: got %d, want 200", owner.Code)
	}
	if code := <-refused; code != http.StatusForbidden {
		t.Errorf("stranger: got %d, want 403", code)
	}
}

// A caller the policy refuses learns that, not whether the renderer is
// busy: with every slot held, an unauthenticated request is still a 401.
func TestPDFAccessRefusalWinsOverBusy(t *testing.T) {
	g := &gateRenderer{arrived: make(chan struct{}, 1), release: make(chan struct{})}
	b := New(Config{MaxConcurrentRenders: 1, PDFRenderer: g}).
		Document(Document{Name: "invoice", Path: "/invoice", Build: docBuild("<p>x</p>")})
	h := mount(t, b)

	done := make(chan struct{})
	go func() {
		defer close(done)
		get(t, authed(h), "/print/invoice/pdf")
	}()
	<-g.arrived // the one slot is rendering
	anon := get(t, h, "/print/invoice/pdf")
	close(g.release)
	<-done
	if anon.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated request while the render slot is busy: got %d, want 401", anon.Code)
	}
}
