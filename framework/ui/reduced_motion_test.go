package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// mediaBlocks splits css into the bodies of every @media block whose
// prelude contains query and the CSS left outside them.
func mediaBlocks(css, query string) (inside []string, outside string) {
	var out strings.Builder
	for {
		at := strings.Index(css, "@media")
		if at < 0 {
			out.WriteString(css)
			break
		}
		open := strings.IndexByte(css[at:], '{')
		if open < 0 {
			out.WriteString(css)
			break
		}
		open += at
		depth, end := 0, -1
	scan:
		for i := open; i < len(css); i++ {
			switch css[i] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				end = i
				break scan
			}
		}
		if end < 0 {
			out.WriteString(css)
			break
		}
		if strings.Contains(css[at:open], query) {
			out.WriteString(css[:at])
			inside = append(inside, css[open+1:end])
		} else {
			out.WriteString(css[:end+1])
		}
		css = css[end+1:]
	}
	return inside, out.String()
}

// keyframes returns the body of the named @keyframes rule.
func keyframes(t *testing.T, css, name string) string {
	t.Helper()
	at := strings.Index(css, "@keyframes "+name)
	if at < 0 {
		t.Fatalf("no @keyframes %s in:\n%s", name, css)
	}
	open := at + strings.IndexByte(css[at:], '{')
	depth := 0
	for i := open; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
		if depth == 0 {
			return css[open+1 : i]
		}
	}
	t.Fatalf("unterminated @keyframes %s", name)
	return ""
}

var motionProps = regexp.MustCompile(`\b(transform|translate|rotate|scale|background-position|inset|left|right|top|bottom|inline-size|block-size|width|height)\s*:`)

// The collapsible's chevron turns only for readers who have not asked
// for reduced motion: its transition lives inside a no-preference block.
func TestCollapsibleChevronTurnsOnlyWithoutReducedMotion(t *testing.T) {
	css := collapsibleStyle.Entry().CSSFor(style.DefaultTheme())
	inside, outside := mediaBlocks(css, "prefers-reduced-motion: no-preference")
	if strings.Contains(outside, "transition") {
		t.Errorf("a collapsible transition sits outside the no-preference block:\n%s", outside)
	}
	if len(inside) != 1 || !strings.Contains(inside[0], "transition:transform") {
		t.Errorf("the chevron's transform transition is not gated on no-preference: %q", inside)
	}
}

// Reduced motion stops the spinner: every part swaps its spin, pulse or
// ripple for a keyframe that only changes opacity.
func TestSpinnerHoldsStillUnderReducedMotion(t *testing.T) {
	css := spinnerCSS(style.DefaultTheme())
	inside, _ := mediaBlocks(css, "prefers-reduced-motion: reduce")
	if len(inside) != 1 {
		t.Fatalf("want one reduced-motion block in the spinner sheet, got %d", len(inside))
	}
	block := inside[0]
	for _, part := range []string{".fui-spinner__ring", ".fui-spinner__dot", ".fui-spinner__cell"} {
		if !strings.Contains(block, part) {
			t.Errorf("reduced motion does not cover %s:\n%s", part, block)
		}
	}
	if !strings.Contains(block, "animation: fui-spinner-breathe") {
		t.Errorf("reduced motion keeps the moving keyframes:\n%s", block)
	}
	if motionProps.MatchString(keyframes(t, css, "fui-spinner-breathe")) {
		t.Errorf("the reduced-motion keyframe moves something")
	}
}

// An indeterminate bar animates a sweep, and under reduced motion the
// sweep becomes a still band whose keyframe touches only opacity.
func TestProgressIndeterminateSweepsAndHoldsStillUnderReducedMotion(t *testing.T) {
	css := progressCSS(style.DefaultTheme())
	_, outside := mediaBlocks(css, "prefers-reduced-motion: reduce")
	if !strings.Contains(outside, ".fui-progress__bar:indeterminate {") ||
		!strings.Contains(outside, "animation: fui-progress-sweep") {
		t.Errorf("an indeterminate bar draws no sweep:\n%s", outside)
	}
	if !strings.Contains(keyframes(t, css, "fui-progress-sweep"), "background-position") {
		t.Errorf("the sweep keyframe moves nothing")
	}
	// The forced-colors band has a reduced-motion block of its own, so
	// one block holds the still band and none restarts the sweep.
	inside, _ := mediaBlocks(css, "prefers-reduced-motion: reduce")
	held := false
	for _, blk := range inside {
		if strings.Contains(blk, ":indeterminate") && strings.Contains(blk, "animation: fui-progress-breathe") {
			held = true
		}
		if strings.Contains(blk, "fui-progress-sweep") {
			t.Errorf("a reduced-motion block runs the sweep: %q", blk)
		}
	}
	if !held {
		t.Fatalf("reduced motion leaves the indeterminate sweep running: %q", inside)
	}
	if motionProps.MatchString(keyframes(t, css, "fui-progress-breathe")) {
		t.Errorf("the reduced-motion keyframe moves something")
	}
}

// A negative Value renders a <progress> with no value attribute, which
// is what the :indeterminate rules match.
func TestProgressNegativeValueIsIndeterminate(t *testing.T) {
	h := string(Progress(ProgressConfig{Label: "Uploading", Value: -1}))
	at := strings.Index(h, "<progress")
	if at < 0 {
		t.Fatalf("no <progress> in %s", h)
	}
	tag := h[at : at+strings.IndexByte(h[at:], '>')]
	if strings.Contains(tag, " value=") {
		t.Errorf("an indeterminate bar carries a value: %s", tag)
	}
}
