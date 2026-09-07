//go:build red

package resources

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2). Tests-only: no production edits.
//
// [provider-body-models-log]
// Property: error strings built from PEER-RESPONSE bodies never reach slog
// live — the log-sink scrub family (recovered panic values and handler
// errors are scrubbed of C0/DEL/C1/bidi before the sink; a peer response
// body is the third source class of operator-tail text, and it is
// attacker-shaped when the provider endpoint or a MITM proxy answers it).
// Surfaces: openrouter.go::Models :111-113 — a non-200 /models answer
// pours up to 8192 RAW body bytes into the returned error — which
// resources.go::ListProviders :137-145 hands to slog.Warn's "error" attr
// verbatim.
// Finding (2026-09-06): a 500 /models body carrying U+202E (bidi RLO),
// U+009B (CSI), and a raw ESC sequence flows into the default slog sink
// unscrubbed — invisible/bidi bytes forge lines in the operator's log
// tail (terminal-injection).
// Fix direction: scrub the body bytes at the error-construction site (the
// core/textsafe StripUnsafe rule the panic-log family uses), keeping the
// HTTP status digits and a bounded body preview for diagnosis.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/openrouter"
)

// redProvLogSink is a minimal slog.Handler storing record attributes
// verbatim (no JSON/text escaping), so a test can detect raw control
// bytes a real handler would have escaped away. Unique-prefix replica of
// the captureHandler pattern (core/handler/panic_logscrub_red_test.go).
type redProvLogSink struct {
	mu    sync.Mutex
	attrs map[string]string
}

func (h *redProvLogSink) Enabled(context.Context, slog.Level) bool { return true }

func (h *redProvLogSink) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.attrs == nil {
		h.attrs = map[string]string{}
	}
	r.Attrs(func(a slog.Attr) bool {
		h.attrs[a.Key] = a.Value.String()
		return true
	})
	return nil
}

func (h *redProvLogSink) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *redProvLogSink) WithGroup(string) slog.Handler      { return h }

func (h *redProvLogSink) redGet(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attrs[key]
}

func TestProviderErrRedScrubbed(t *testing.T) {
	// Hostile peer body: bidi override, C1 CSI, and a raw ESC OSC payload
	// — the terminal-forgery set — wrapped around visible text.
	const hostile = "pwn\u202eRLO \u009bCSI \x1b]0;injected\x07tail"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("setup broken: provider fetched %q, want /models", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(hostile))
	}))
	defer srv.Close()

	prev := slog.Default()
	sink := &redProvLogSink{}
	slog.SetDefault(slog.New(sink))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cat := NewCatalog()
	cat.Providers = []provider.Provider{
		&openrouter.Provider{BaseURL: srv.URL, APIKey: "k"},
	}
	// Display-only catalog: the call itself must complete (it degrades to
	// an empty model list), the finding is what lands in the log.
	out := cat.ListProviders(context.Background())
	if len(out) != 1 || out[0].Name != "openrouter" {
		t.Fatalf("setup broken: ListProviders did not degrade to the provider entry: %+v", out)
	}

	got := sink.redGet("error")
	if got == "" {
		t.Fatal("setup broken: provider error never reached the slog sink " +
			"(ListProviders must log the refused /models listing)")
	}
	for _, bad := range []string{"\u202E", "\u009B", "\x1b"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [provider-body-models-log] raw peer-response bytes reached slog live: "+
				"%q present in the \"error\" attr %q. Attack: a hostile (or MITM'd) provider endpoint "+
				"answers /models with bidi/control bytes that forge lines in the operator's log tail "+
				"(terminal-injection) — every other operator-facing error path scrubs this set; "+
				"openrouter.go::Models :111-113 pours up to 8192 raw body bytes into the error "+
				"and resources.go::ListProviders :144 logs it verbatim", bad, got)
		}
	}
	// The scrub must not eat the diagnosis: the HTTP status stays legible.
	if !strings.Contains(got, "500") {
		t.Errorf("scrub must keep the diagnosis: HTTP status digits missing from %q", got)
	}
}
