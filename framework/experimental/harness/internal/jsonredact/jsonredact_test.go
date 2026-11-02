package jsonredact

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestStringsRedactsDecodedText(t *testing.T) {
	re := regexp.MustCompile(`<[^>]*>`)
	in := []byte(`{"a":"x <dom@x.com>","n":12345678901234567890,"b":["<k>",true]}`)
	out, ok := Strings(in, func(s string) string { return re.ReplaceAllString(s, "R") })
	if !ok {
		t.Fatal("valid JSON refused")
	}
	want := `{"a":"x R","n":12345678901234567890,"b":["R",true]}`
	if string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
	if !json.Valid(out) {
		t.Error("output is not JSON")
	}
}

func TestStringsRefusesInvalidJSON(t *testing.T) {
	if _, ok := Strings([]byte(`{"a":"cut`), func(s string) string { return s }); ok {
		t.Error("invalid JSON accepted")
	}
}

func TestStringsKeepsEscapedQuotes(t *testing.T) {
	in := []byte(`{"a":"say \"hi\" \\","b":"k"}`)
	out, ok := Strings(in, func(s string) string {
		if s == "k" {
			return "K"
		}
		return s
	})
	if !ok || string(out) != `{"a":"say \"hi\" \\","b":"K"}` {
		t.Errorf("got %s ok=%v", out, ok)
	}
}

// Members hands a string value its member name, and every other string
// (an object key, an array element) "".
func TestMembersPassesTheMemberName(t *testing.T) {
	in := []byte(`{"a" : "va", "env": {"K": "vk"}, "arr": ["e1", "e2"], "n": 1, "b": "vb"}`)
	got := map[string]string{}
	out, ok := Members(in, func(key, s string) string {
		got[s] = key
		return s
	})
	if !ok || string(out) != string(in) {
		t.Fatalf("Members changed an untouched document: ok=%v out=%s", ok, out)
	}
	want := map[string]string{
		"a": "", "va": "a", "env": "", "K": "", "vk": "K",
		"arr": "", "e1": "", "e2": "", "n": "", "b": "", "vb": "b",
	}
	for s, k := range want {
		if got[s] != k {
			t.Errorf("string %q got key %q, want %q", s, got[s], k)
		}
	}
}
