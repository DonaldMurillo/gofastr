package n

import (
	"fmt"
	"net/url"
	"strings"
)

// Sanctioned fix 1: %%-double before concatenation — helper returns
// clean values, so callers stay silent.
func patternWithDoubled(carry url.Values, tail string) string {
	enc := strings.ReplaceAll(carry.Encode(), "%", "%%")
	return "?" + enc + "&" + tail
}

func okDoubled(v url.Values) string {
	p := patternWithDoubled(v, "sort=%s")
	return fmt.Sprintf(p, "k")
}

// Encoded value as a VALUE argument, format stays a literal.
func okValueArg(v url.Values, pattern string) string {
	return fmt.Sprintf(pattern, v.Encode())
}

// Sanctioned fix 2: consume the verb without fmt. The local
// concatenation is deliberately not flagged (indistinguishable from
// safe use at the concat site).
func okReplaceConsumption(v url.Values) string {
	p := "?" + v.Encode() + "&p=%d"
	return strings.Replace(p, "%d", "3", 1)
}

// Encoded string used outside fmt entirely.
func okNonFmt(v url.Values) string {
	u := "https://x/?" + v.Encode()
	return u + strings.ToLower(v.Encode())
}

// Helper called WITHOUT verb literals: the join carries no verbs, so
// the result cannot corrupt a later format.
func okVerblessCall(v url.Values) string {
	return patternWithDoubled(v, "") + "x"
}

// Builder output consumed LOCALLY without fmt: silent even though the
// concatenation carries verbs.
func okBuilderLocalConsume(q string) string {
	var carry strings.Builder
	carry.WriteString(url.QueryEscape(q))
	p := "?" + carry.String() + "&p=%d"
	return strings.Replace(p, "%d", "3", 1)
}
