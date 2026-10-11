package ui

import (
	"regexp"
	"strings"
	"testing"
)

const longID = "c86a70b90b096dfa646910e48fd975dc"

// A long id shows its first characters, and the copy button copies all
// of it: its target holds the full value.
func TestShortIDCopiesTheFullValue(t *testing.T) {
	out := string(ShortID(ShortIDConfig{Value: longID}))
	code := regexp.MustCompile(`<code[^>]*>([^<]*)</code>`).FindStringSubmatch(out)
	if code == nil || code[1] != "c86a70b9…" {
		t.Fatalf("visible id = %v, want c86a70b9…:\n%s", code, out)
	}
	target := regexp.MustCompile(`data-hui-copy-target="([^"]+)"`).FindStringSubmatch(out)
	if target == nil {
		t.Fatalf("no copy button:\n%s", out)
	}
	full := regexp.MustCompile(`id="` + regexp.QuoteMeta(target[1]) + `"[^>]*>([^<]*)<`).FindStringSubmatch(out)
	if full == nil || full[1] != longID {
		t.Fatalf("copy target holds %v, want the full id:\n%s", full, out)
	}
	if !strings.Contains(out, `title="`+longID+`"`) {
		t.Errorf("the short id carries no title with the full value:\n%s", out)
	}
}

// Screen readers hear the full id once: the shortened text is hidden
// from them.
func TestShortIDReadsFullValueOnce(t *testing.T) {
	out := string(ShortID(ShortIDConfig{Value: longID}))
	if !regexp.MustCompile(`<code[^>]*aria-hidden="true"`).MatchString(out) {
		t.Fatalf("the shortened text is not hidden from screen readers:\n%s", out)
	}
}

// An id no longer than Length shows whole, with no second copy.
func TestShortIDKeepsAShortValue(t *testing.T) {
	out := string(ShortID(ShortIDConfig{Value: "job-42"}))
	if strings.Contains(out, "…") || strings.Count(out, ">job-42<") != 1 {
		t.Fatalf("a short id is cut or doubled:\n%s", out)
	}
	if strings.Contains(out, `aria-hidden="true">job-42`) {
		t.Errorf("a whole id is hidden from screen readers:\n%s", out)
	}
}

// Length counts characters, not bytes, so a cut never splits one.
func TestShortIDCutsWholeRunes(t *testing.T) {
	out := string(ShortID(ShortIDConfig{Value: "ééééé", Length: 3}))
	if !strings.Contains(out, ">ééé…<") {
		t.Fatalf("cut is not three whole runes:\n%s", out)
	}
}

func TestShortIDRequiresValue(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an empty Value rendered")
		}
	}()
	ShortID(ShortIDConfig{})
}
