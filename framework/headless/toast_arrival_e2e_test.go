package headless

import (
	"net/http"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestServerToastArrivalHasLiveRole(t *testing.T) {
	stack := ToastStack(ToastStackProps{Label: "News", ID: "news"}, nil)
	rows := Toast(ToastProps{ID: "default", Title: "Saved"}, nil) +
		Toast(ToastProps{ID: "polite", Title: "Synced", Live: LivePolite}, nil) +
		Toast(ToastProps{ID: "assertive", Title: "Failed", Live: LiveAssertive}, nil)
	server := startBehaviorServer(t, string(stack), func(mux *http.ServeMux) {
		mux.HandleFunc("/news", func(w http.ResponseWriter, request *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(rows))
		})
	})
	ctx := behaviorPage(t, server)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`fetch('/news').then(response => response.text()).then(markup => document.getElementById('news').insertAdjacentHTML('beforeend', markup))`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('#news [data-hui-toast]').length === 3`) {
		t.Fatal("server toast rows did not arrive")
	}
	var correct bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`['default', 'polite', 'assertive'].every(id => {
		const row = document.getElementById(id);
		return row.getAttribute('role') === (id === 'assertive' ? 'alert' : 'status') && !row.hasAttribute('aria-live');
	}) && !document.getElementById('news').hasAttribute('aria-live')`, &correct)); err != nil {
		t.Fatal(err)
	}
	if !correct {
		t.Fatal("server toast arrival lacks one role-only live region per row")
	}
}
