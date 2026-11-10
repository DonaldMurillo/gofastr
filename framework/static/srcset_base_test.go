package static

import (
	"strings"
	"testing"
)

// A <picture>'s <source srcset> is what the browser loads; the <img src>
// beside it is only the fallback for a browser without <picture>. On a
// subpath deploy the src was prefixed and the srcset was not, so every
// ui.Image with Sources and every ui.PipelineImage asked the apex for its
// file and drew nothing.
func TestRewriteBaseURLs_PrefixesSrcset(t *testing.T) {
	b := &Builder{BasePath: "/gofastr"}
	in := `<picture><source srcset="/img/hero-400.webp 400w, /img/hero-800.webp 800w" sizes="100vw">` +
		`<source type="image/avif" srcset="/__gofastr/img/a.avif 1x, /__gofastr/img/b.avif 2x">` +
		`<img class="fui-image__img" src="/img/hero.jpg" srcset="/img/hero.jpg, https://cdn.example.com/x.jpg 2x, //cdn.example.com/y.jpg 3x" alt=""></picture>` +
		`<img srcset="data:image/jpeg;base64,/9j/4AAQ 1x, photo.jpg 2x" alt="">`
	out := b.rewriteBaseURLs(in)
	for _, want := range []string{
		`srcset="/gofastr/img/hero-400.webp 400w, /gofastr/img/hero-800.webp 800w"`,
		`srcset="/gofastr/__gofastr/img/a.avif 1x, /gofastr/__gofastr/img/b.avif 2x"`,
		`src="/gofastr/img/hero.jpg"`,
		`srcset="/gofastr/img/hero.jpg, https://cdn.example.com/x.jpg 2x, //cdn.example.com/y.jpg 3x"`,
		`srcset="data:image/jpeg;base64,/9j/4AAQ 1x, photo.jpg 2x"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rewriteBaseURLs: want %q; got:\n%s", want, out)
		}
	}
	if apex := (&Builder{}).rewriteBaseURLs(in); apex != in {
		t.Errorf("an apex export must leave srcset as written; got:\n%s", apex)
	}
}
