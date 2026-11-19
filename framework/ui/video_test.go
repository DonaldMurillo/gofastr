package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

func TestVideoReservesItsBoxAndWaitsForPlay(t *testing.T) {
	out := string(Video(VideoConfig{
		Sources: []VideoSource{{URL: "/v/intro.webm", Type: "video/webm"}, {URL: "/v/intro.mp4", Type: "video/mp4"}},
		Width:   1280, Height: 720,
		Poster: "/v/intro.jpg",
		Label:  "Product tour",
		Tracks: []VideoTrack{{URL: "/v/intro.en.vtt", Lang: "en", Label: "English", Default: true}},
	}))
	for _, want := range []string{
		`data-cui-comp="ui-video"`, `controls`, `playsinline`,
		`width="1280"`, `height="720"`, `preload="none"`, `poster="/v/intro.jpg"`, `aria-label="Product tour"`,
		`src="/v/intro.webm"`, `type="video/webm"`, `src="/v/intro.mp4"`, `type="video/mp4"`,
		`kind="captions"`, `srclang="en"`, `label="English"`, `default`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	if strings.Index(out, "intro.webm") > strings.Index(out, "intro.mp4") {
		t.Errorf("sources must keep the caller's order: %s", out)
	}
	if eager := string(Video(VideoConfig{Sources: []VideoSource{{URL: "/v.mp4"}}, Width: 1, Height: 1, Eager: true})); !strings.Contains(eager, `preload="metadata"`) {
		t.Errorf("Eager must fetch metadata with the page: %s", eager)
	}
}

func TestVideoDropsUnsafeURLsAndKeepsItsOwnAttributes(t *testing.T) {
	out := string(Video(VideoConfig{
		Sources: []VideoSource{{URL: "javascript:alert(1)"}, {URL: "/ok.mp4"}},
		Width:   16, Height: 9,
		Poster:     "javascript:alert(2)",
		Tracks:     []VideoTrack{{URL: "javascript:alert(3)"}},
		ExtraAttrs: html.Attrs{"preload": "auto", "poster": "/evil.png", "src": "/evil.mp4", "data-cui-x": "1"},
	}))
	if strings.Contains(out, "javascript:") || strings.Contains(out, "evil") || strings.Contains(out, "data-cui-x") || strings.Contains(out, "<track") {
		t.Errorf("SECURITY: an unsafe URL or a protected attribute reached the video: %s", out)
	}
	if !strings.Contains(out, `src="/ok.mp4"`) || !strings.Contains(out, `preload="none"`) {
		t.Errorf("the safe source or the component's preload is missing: %s", out)
	}
}

func TestVideoPanicsOnMissingRequiredFields(t *testing.T) {
	for name, cfg := range map[string]VideoConfig{
		"no sources": {Width: 1, Height: 1},
		"no size":    {Sources: []VideoSource{{URL: "/v.mp4"}}},
		"bad kind":   {Sources: []VideoSource{{URL: "/v.mp4"}}, Width: 1, Height: 1, Tracks: []VideoTrack{{URL: "/t.vtt", Kind: "caption"}}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Video did not panic")
				}
			}()
			Video(cfg)
		})
	}
}
