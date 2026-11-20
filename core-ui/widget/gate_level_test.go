package widget_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
)

func TestStricterGateWins(t *testing.T) {
	widget.SetSessionCheck(func(*http.Request) bool { return true })
	widget.SetAuthenticatedCheck(func(*http.Request) bool { return false })
	t.Cleanup(func() {
		widget.SetSessionCheck(nil)
		widget.SetAuthenticatedCheck(nil)
	})
	def := widget.Definition{Name: "both-gates", RequireSession: true, RequireAuthenticated: true}
	if widget.GateSatisfied(&def, httptest.NewRequest("GET", "/", nil)) {
		t.Fatal("a widget asking for both gates passed on a session alone")
	}
}
