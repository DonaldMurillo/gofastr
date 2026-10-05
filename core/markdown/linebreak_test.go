package markdown

import "testing"

// Source docs are hard-wrapped at ~72 columns. A plain newline inside a
// paragraph is a soft break (CommonMark): it renders as a newline, which
// the browser folds into a space. Only an explicit hard break, two or more
// trailing spaces or a trailing backslash, renders as <br>.
func TestSoftBreakRendersAsNewline(t *testing.T) {
	got := string(RenderHTML("first line\nsecond line\n"))
	want := "<p>first line\nsecond line</p>\n"
	if got != want {
		t.Fatalf("soft break:\n got: %q\nwant: %q", got, want)
	}
}

func TestHardBreaksRenderAsBr(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"two spaces":   {"first  \nsecond\n", "<p>first<br>\nsecond</p>\n"},
		"many spaces":  {"first     \nsecond\n", "<p>first<br>\nsecond</p>\n"},
		"backslash":    {"first\\\nsecond\n", "<p>first<br>\nsecond</p>\n"},
		"one space":    {"first \nsecond\n", "<p>first\nsecond</p>\n"},
		"list item":    {"- one  \n  two\n", "<ul>\n  <li>one<br>\ntwo</li>\n</ul>\n"},
		"list soft":    {"- one\n  two\n", "<ul>\n  <li>one\ntwo</li>\n</ul>\n"},
		"blockquote":   {"> one\n> two\n", "<blockquote>\n<p>one\ntwo</p>\n</blockquote>\n"},
		"code kept":    {"`a  `\nb\n", "<p><code>a  </code>\nb</p>\n"},
		"trailing end": {"last  \n", "<p>last</p>\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := string(RenderHTML(tc.src)); got != tc.want {
				t.Fatalf("\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}
