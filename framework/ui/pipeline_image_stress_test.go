package ui

import (
	"strings"
	"testing"
)

// TestPipelineImageTypeEscaped: Source.Type that contains > and " must
// be HTML-escaped, not interpolated into the type attribute literally.
func TestPipelineImageTypeEscaped(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/x.jpg", Alt: "x", Width: 1, Height: 1,
		Sources: []PipelineSource{
			{URL: "/a.webp", Width: 100, Type: `image/webp"><script>alert(1)</script>`},
		},
	})
	s := string(h)
	if strings.Contains(s, "<script>") {
		t.Fatalf("Type attribute not escaped — XSS possible: %s", s)
	}
	if !strings.Contains(s, "&lt;script&gt;") && !strings.Contains(s, "&quot;") {
		t.Fatalf("expected escaped output; got: %s", s)
	}
}

// TestPipelineImageDuplicateURLs documents that duplicate (URL, Width, Type)
// triples are emitted twice in srcset — no dedup.
func TestPipelineImageDuplicateURLs(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/x.jpg", Alt: "x", Width: 1, Height: 1,
		Sources: []PipelineSource{
			{URL: "/a.webp", Width: 100, Type: "image/webp"},
			{URL: "/a.webp", Width: 100, Type: "image/webp"},
		},
	})
	s := string(h)
	count := strings.Count(s, "/a.webp 100w")
	t.Logf("duplicate URL appears %d time(s) in srcset (no dedup)", count)
}

// TestPipelineImage1000Sources: does N=1000 sources produce a valid but
// huge HTML output?
func TestPipelineImage1000Sources(t *testing.T) {
	srcs := make([]PipelineSource, 1000)
	for i := range srcs {
		srcs[i] = PipelineSource{URL: "/x.webp", Width: i + 1, Type: "image/webp"}
	}
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/x.jpg", Alt: "x", Width: 1, Height: 1, Sources: srcs,
	})
	t.Logf("1000-source PipelineImage length: %d bytes", len(string(h)))
}
