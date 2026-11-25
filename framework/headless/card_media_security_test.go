package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestCardTitleLinkAllowsPassiveMedia(t *testing.T) {
	for _, content := range []string{
		`<audio src="/sample.mp3" title="no controls"></audio>`,
		`<video src="/preview.mp4" muted></video>`,
	} {
		if holdsInteractiveContent(content) {
			t.Errorf("card title treats media without controls as interactive: %s", content)
			continue
		}
		markup := Card(CardProps{TitleHref: "/item", TitleContent: render.HTML(content)}, nil)
		if !strings.Contains(string(markup), `href="/item">`+content+`</a>`) {
			t.Errorf("card title link lost media %q", content)
		}
	}
	for _, content := range []render.HTML{
		`<audio controls src="/sample.mp3"></audio>`,
		`<video src="/preview.mp4" CONTROLS="false"></video>`,
	} {
		refuse(t, "TitleHref", func() {
			Card(CardProps{TitleHref: "/item", TitleContent: content}, nil)
		})
	}
}
