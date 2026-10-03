package headless

import (
	"strings"
	"testing"
)

// The slot is always in the document and a message arrives in it, so
// the default is hidden: a banner rendered visible by accident is
// page furniture announcing itself on every load.
func TestSystemBannerShipsHiddenUntilShown(t *testing.T) {
	hidden := SystemBanner(SystemBannerProps{ID: "sys", Title: "Maintenance tonight"}, nil)
	has(t, hidden, `hidden=""`, "the banner ships visible — something was meant to show it and nothing does")
	shown := SystemBanner(SystemBannerProps{ID: "sys", Title: "Maintenance tonight", Shown: true}, nil)
	hasNot(t, shown, `hidden`, "Shown did not show it")
}

// The tone is said in words before the title, read and not seen, so
// the kind of message survives for anyone who cannot see the colour.
func TestSystemBannerToneIsSaidInWords(t *testing.T) {
	got := SystemBanner(SystemBannerProps{
		ID: "sys", Title: "Connection lost", Tone: "warning",
	}, nil)
	has(t, got, "Warning: ", "the tone exists only as colour")
	if strings.Index(string(got), "Warning: ") > strings.Index(string(got), "Connection lost") {
		t.Error("the tone word is read after the title — the kind of message must arrive before the message")
	}
}

// Polite by default, assertive only when offline. A system message is
// important and must not interrupt; losing the connection is the one
// thing worth interrupting for, because everything the reader does
// next will fail until it is back.
func TestSystemBannerIsQuietUnlessOffline(t *testing.T) {
	got := SystemBanner(SystemBannerProps{ID: "sys", Title: "Deploy in progress", Shown: true}, nil)
	has(t, got, `role="status"`, "a system message interrupts by default")
	hasNot(t, got, "aria-live", "the policy is stated twice and the message may be announced twice")
	no := false
	off := SystemBanner(SystemBannerProps{ID: "sys", Title: "Connection lost", Offline: true, Tone: "warning", Dismiss: &no}, nil)
	has(t, off, `role="alert"`, "losing the connection is not announced as urgent")
	has(t, off, `aria-live="assertive"`, "the offline banner does not say how urgent it is")
	has(t, off, `data-hui-system-offline=""`, "the runtime cannot find the banner it owns")
}

// The offline banner is the runtime's: it is shown when the framework
// reports the connection lost and hidden on reconnect, so it always
// ships hidden and the page cannot show it.
func TestSystemBannerOfflineRefusesShown(t *testing.T) {
	no := false
	off := SystemBanner(SystemBannerProps{ID: "sys", Title: "Connection lost", Offline: true, Tone: "warning", Dismiss: &no}, nil)
	has(t, off, `hidden=""`, "the runtime owns showing the offline banner, so it must ship hidden")
	mustRefuse(t, "an offline banner shipping shown", func() {
		SystemBanner(SystemBannerProps{ID: "sys", Title: "Connection lost", Offline: true, Shown: true, Dismiss: &no}, nil)
	})
}

// The offline banner carries no dismiss, because its ending is the
// reconnect: a dismissal remembered for the session would hide the
// next outage too, and the module skips the dismissed set for it
// precisely because there is nothing to dismiss. Both the default
// (nil) and an explicit yes are refused; only a stated no renders.
func TestSystemBannerOfflineCarriesNoDismiss(t *testing.T) {
	no := false
	off := SystemBanner(SystemBannerProps{
		ID: "sys", Title: "Connection lost", Offline: true, Tone: "warning", Dismiss: &no}, nil)
	hasNot(t, off, "data-hui-system-dismiss", "the offline banner carries a dismiss whose memory would hide the next outage")
	mustRefuse(t, "an offline banner with the default dismiss", func() {
		SystemBanner(SystemBannerProps{ID: "sys", Title: "Connection lost", Offline: true, Tone: "warning"}, nil)
	})
	yes := true
	mustRefuse(t, "an offline banner with an explicit dismiss", func() {
		SystemBanner(SystemBannerProps{ID: "sys", Title: "Connection lost", Offline: true, Tone: "warning", Dismiss: &yes}, nil)
	})
}

// The dismiss is a button, not a link: dismissing needs no navigation,
// only the module that binds data-hui-system-dismiss. It names what it
// dismisses, because three banners each called "Dismiss" say which
// nothing.
func TestSystemBannerDismissIsAButtonWithAName(t *testing.T) {
	got := SystemBanner(SystemBannerProps{ID: "sys", Title: "A new version is ready", Tone: "success"}, nil)
	has(t, got, "data-hui-system-dismiss", "the dismiss carries no hook for the runtime")
	has(t, got, `aria-label="Dismiss: A new version is ready"`, "the dismiss does not say what it closes")
	if !strings.Contains(string(got), "<button") {
		t.Error("the dismiss is not a button")
	}
}

// The dismiss is the default and refusing it is the deliberate act,
// not the other way round: a system message the reader cannot send
// away is furniture that outstays its news.
func TestSystemBannerDismissDefaultsToYes(t *testing.T) {
	no := false
	got := SystemBanner(SystemBannerProps{ID: "sys", Title: "Deploy in progress", Dismiss: &no}, nil)
	hasNot(t, got, "data-hui-system-dismiss", "Dismiss: false still rendered a dismiss button")
	defaulted := SystemBanner(SystemBannerProps{ID: "sys", Title: "Deploy in progress"}, nil)
	has(t, defaulted, "data-hui-system-dismiss", "the default dismissed the banner")
}

// The id is the message's identity — the runtime remembers it so the
// same message is not shown twice — so a banner without one is a
// banner whose promise cannot be kept.
func TestSystemBannerRequiresAnIDAndATitle(t *testing.T) {
	mustRefuse(t, "a banner with no ID", func() {
		SystemBanner(SystemBannerProps{Title: "Connection lost"}, nil)
	})
	mustRefuse(t, "a banner with no Title", func() {
		SystemBanner(SystemBannerProps{ID: "sys"}, nil)
	})
}

// The tone word is derived from the tone, so a tone nobody spelled is
// a banner nobody tinted and nobody heard the kind of.
func TestSystemBannerRefusesAnUnknownTone(t *testing.T) {
	mustRefuse(t, "an unknown Tone", func() {
		SystemBanner(SystemBannerProps{ID: "sys", Title: "Connection lost", Tone: "urgent"}, nil)
	})
}
