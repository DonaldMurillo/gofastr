package bom

import "testing"

func TestTrimDropsOneMark(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"x":                 "x",
		UTF8 + "x":          "x",
		UTF8 + UTF8 + "x":   UTF8 + "x",
		"x" + UTF8:          "x" + UTF8,
		"\xEF\xBB":          "\xEF\xBB",
		"\uFEFFKEY=value\n": "KEY=value\n",
	}
	for in, want := range cases {
		if got := Trim(in); got != want {
			t.Errorf("Trim(%q) = %q, want %q", in, got, want)
		}
		if got := string(TrimBytes([]byte(in))); got != want {
			t.Errorf("TrimBytes(%q) = %q, want %q", in, got, want)
		}
	}
}
