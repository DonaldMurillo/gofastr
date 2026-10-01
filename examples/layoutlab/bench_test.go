package main

// Fill-concurrency benchmark — /slow3/:ms resolves three fills
// (toolbar, aside, rail) that each sleep ms millis; the decided
// concurrent loading pays ~1×ms where a sequential walk would pay
// 3×ms. Full page and subtree partial, at ms=0 and ms=200:
//
//	go test ./examples/layoutlab/ -run '^$' -bench BenchmarkSlow3 -benchmem -count=1

import (
	"context"
	"testing"
)

func benchmarkSlow3(b *testing.B, ms string, partial bool) {
	b.Helper()
	site := buildSite()
	path := "/slow3/" + ms
	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		var err error
		if partial {
			_, err = site.RenderPartialFromResult(ctx, path, "/")
		} else {
			_, err = site.RenderPageResult(ctx, path)
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSlow3(b *testing.B) {
	shape := map[bool]string{false: "full", true: "partial"}
	for _, ms := range []string{"0", "200"} {
		for _, partial := range []bool{false, true} {
			b.Run(ms+"ms/"+shape[partial], func(b *testing.B) {
				benchmarkSlow3(b, ms, partial)
			})
		}
	}
}
