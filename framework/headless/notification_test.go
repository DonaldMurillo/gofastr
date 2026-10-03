package headless

import (
	"math"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The notification family's own contracts: the toast's tone and
// dismiss, the stack's landmark, the bell's spoken count.

func TestToastToneWordAndDismiss(t *testing.T) {
	got := Toast(ToastProps{Tone: "danger", Title: "Connection lost", Live: LiveAssertive,
		TTLMS: 8000, DismissHref: "/dismiss/conn",
		Island: Island{Endpoint: "/island/toasts", Signal: "toasts"}}, nil)
	has(t, got, `role="alert"`, "an assertive toast does not say it interrupts")
	has(t, got, `>Error: </span>`, "the tone word did not arrive before the title")
	if strings.Contains(string(got), `aria-hidden="true">Error: `) {
		t.Errorf("the tone word is hidden from the reader it is for:\n%s", got)
	}
	has(t, got, `data-hui-toast-ttl-ms="8000"`, "the module's timer bound did not travel")
	has(t, got, `aria-label="Dismiss: Connection lost"`, "the dismiss control does not name what it dismisses")
	has(t, got, `data-fui-rpc="/island/toasts"`, "the dismiss link does not carry the island contract beside its href")
}

func TestToastRefusesBrokenConfigurations(t *testing.T) {
	refuse(t, "Title or Body", func() { Toast(ToastProps{}, nil) })
	refuse(t, "Tone", func() { Toast(ToastProps{Title: "x", Tone: "loud"}, nil) })
	refuse(t, "Live", func() { Toast(ToastProps{Title: "x", Live: Live("sudden")}, nil) })
	refuse(t, "negative", func() { Toast(ToastProps{Title: "x", TTLMS: -1}, nil) })
	refuse(t, "requires Island", func() {
		Toast(ToastProps{Title: "x", DismissHref: "/d"}, nil)
	})
	refuse(t, "the anchor policy allows", func() {
		Toast(ToastProps{Title: "x", DismissHref: "javascript:alert(1)",
			Island: Island{Endpoint: "/i", Signal: "s"}}, nil)
	})
}

// The stack is a named landmark carrying both contracts: the
// framework's name the kernel's header path resolves, and this
// package's the module binds.
func TestToastStackCarriesBothContracts(t *testing.T) {
	got := ToastStack(ToastStackProps{Label: "Notifications", Max: 4}, nil)
	has(t, got, `aria-label="Notifications"`, "the stack is not a named region")
	has(t, got, `role="region"`, "the stack does not claim to be a region")
	has(t, got, `data-fui-toast-stack="Notifications"`, "the kernel's stack name is missing")
	has(t, got, `data-hui-toast-stack=""`, "the module's stack hook is missing")
	has(t, got, `data-hui-toast-max="4"`, "the capacity did not travel")

	// An SSR row is visible inside it with no script.
	withRow := ToastStack(ToastStackProps{Label: "Notifications",
		Toasts: []render.HTML{Toast(ToastProps{Tone: "success", Title: "Saved"}, nil)}}, nil)
	has(t, withRow, `data-hui-toast="">`, "the SSR toast row is not inside the stack")
}

func TestToastStackRefusesAnUnnamedOrNegativeStack(t *testing.T) {
	refuse(t, "Label", func() { ToastStack(ToastStackProps{}, nil) })
	refuse(t, "negative", func() { ToastStack(ToastStackProps{Label: "Notifications", Max: -1}, nil) })
}

func TestNotificationBellSpeaksItsCount(t *testing.T) {
	got := NotificationBell(NotificationBellProps{Href: "/notifications",
		Label: "Notifications", UnreadCount: 3}, nil)
	has(t, got, `aria-label="3 unread notifications"`, "the trigger does not say its count in words")
	has(t, got, `href="/notifications"`, "the trigger is not a real link to a real page")
	has(t, got, `data-hui-notification-count="3"`, "the badge's count hook is missing")

	// Zero renders no badge: no news is no news.
	quiet := NotificationBell(NotificationBellProps{Href: "/notifications", Label: "N"}, nil)
	hasNot(t, quiet, `data-hui-notification-count="0"`, "a zero count still renders a badge")

	bound := NotificationBell(NotificationBellProps{Href: "/notifications", Label: "N",
		UnreadBind: &Bind{Signal: "unread"}}, nil)
	has(t, bound, `data-fui-signal="unread"`, "the bound count does not follow its signal")
}

func TestNotificationBellRefusesBrokenTriggers(t *testing.T) {
	refuse(t, "Href", func() { NotificationBell(NotificationBellProps{Label: "N"}, nil) })
	refuse(t, "the anchor policy allows", func() {
		NotificationBell(NotificationBellProps{Href: "javascript:alert(1)", Label: "N"}, nil)
	})
	refuse(t, "negative", func() {
		NotificationBell(NotificationBellProps{Href: "/n", Label: "N", UnreadCount: -1}, nil)
	})
}

// Control bytes never ride a title or body into the toast: a CR LF in
// either is scrubbed, the way the table's carried query is.
func TestToastScrubsControlBytesInTitleAndBody(t *testing.T) {
	got := Toast(ToastProps{Title: "Deploy\r\nfinished", Body: "api is on build\r\n482"}, nil)
	hasNot(t, got, "\r", "a carriage return reached the markup")
	has(t, got, ">Deployfinished</span>", "a CR LF in the title reached the reader")
	has(t, got, ">api is on build482</span>", "a CR LF in the body reached the reader")
}

// A widget name carrying whitespace or control bytes is refused: it
// is a key the widget layer resolves, not free text.
func TestNotificationBellRefusesAWronglyShapedOpensName(t *testing.T) {
	refuse(t, "not a widget name", func() {
		NotificationBell(NotificationBellProps{Href: "/n", Label: "N", Opens: "bell panel"}, nil)
	})
	refuse(t, "not a widget name", func() {
		NotificationBell(NotificationBellProps{Href: "/n", Label: "N", Opens: "bell\x00"}, nil)
	})
}

// The stack carries the tone words: a toast the module builds from a
// response header says its kind to a reader, in the page's language,
// and the module says no word of its own.
func TestToastStackCarriesTheToneWords(t *testing.T) {
	got := ToastStack(ToastStackProps{Label: "Notifications", Max: 4}, nil)
	has(t, got, `data-hui-toast-tone-success="Success"`, "the success word did not travel")
	has(t, got, `data-hui-toast-tone-danger="Error"`, "the danger word did not travel")
	has(t, got, `data-hui-toast-tone-warning="Warning"`, "the warning word did not travel")
	has(t, got, `data-hui-toast-tone-info="Information"`, "the info word did not travel")
}

// A TTL past the timer's range would fire at once; a label of spaces
// names nothing.
func TestToastFamilyRefusesTheEdgesOfItsRanges(t *testing.T) {
	refuse(t, "timer's range", func() { Toast(ToastProps{Title: "x", TTLMS: math.MaxInt64}, nil) })
	refuse(t, "Label", func() { ToastStack(ToastStackProps{Label: "   ", Max: 4}, nil) })
	refuse(t, "Label", func() {
		NotificationBell(NotificationBellProps{Href: "/notifications",
			Label: "   ", UnreadCount: 3}, nil)
	})
}
