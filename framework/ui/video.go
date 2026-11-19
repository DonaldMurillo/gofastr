package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// VideoSource is one encoding of a Video. The browser plays the first one
// it supports, so list the most efficient format first.
type VideoSource struct {
	URL  string // required
	Type string // MIME type, such as "video/webm"; lets the browser skip a format it can't play without fetching it
}

// VideoTrack is a timed text track: captions, subtitles or chapters.
type VideoTrack struct {
	URL     string // WebVTT file: required
	Kind    string // "captions" (default), "subtitles", "descriptions", "chapters" or "metadata"
	Lang    string // BCP 47 language of the track, such as "en"
	Label   string // the name the player's track menu shows
	Default bool
}

// VideoConfig configures a Video.
type VideoConfig struct {
	// Sources are the encodings to try, in order. At least one is required.
	Sources []VideoSource

	// Width and Height are the video's intrinsic pixel size, both required:
	// they reserve the box before anything loads.
	Width  int
	Height int

	// Poster is the image shown before playback.
	Poster string

	// Label names the video for assistive technology when nothing on the
	// page already does.
	Label string

	Tracks []VideoTrack

	// Eager fetches the video's metadata (duration, first frame) with the
	// page. By default nothing is downloaded until the reader presses play.
	Eager bool

	// Aspect forces an aspect ratio distinct from Width and Height.
	Aspect ImageAspect

	// Rounded toggles a token-driven border-radius treatment.
	Rounded bool

	ID    string
	Class string

	// ExtraAttrs land on the <video>; class, id, src, poster, preload and
	// data-cui-* are the component's.
	ExtraAttrs html.Attrs
}

var videoTrackKinds = map[string]bool{"captions": true, "subtitles": true, "descriptions": true, "chapters": true, "metadata": true}

// Video renders a <video> with player controls that reserves its box from
// Width and Height and downloads nothing before play unless Eager is set.
// Sources and tracks whose URL fails the resource policy are dropped. It
// panics without Sources, without Width and Height, or on an unknown
// track Kind.
func Video(cfg VideoConfig) render.HTML {
	if len(cfg.Sources) == 0 {
		panic("ui: Video requires at least one Source")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		panic("ui: Video requires Width and Height > 0 to prevent layout shift")
	}
	kids := make([]render.HTML, 0, len(cfg.Sources)+len(cfg.Tracks))
	for _, s := range cfg.Sources {
		if !urlsafe.OK(s.URL, urlsafe.Resource) {
			continue
		}
		attrs := map[string]string{"src": s.URL, "data-cui-internal": ""}
		if s.Type != "" {
			attrs["type"] = s.Type
		}
		kids = append(kids, render.VoidTag("source", attrs))
	}
	for _, tr := range cfg.Tracks {
		kind := tr.Kind
		if kind == "" {
			kind = "captions"
		}
		if !videoTrackKinds[kind] {
			panic(`ui: Video track unknown Kind "` + kind + `". Pick one of: captions, subtitles, descriptions, chapters, metadata`)
		}
		if !urlsafe.OK(tr.URL, urlsafe.Resource) {
			continue
		}
		attrs := map[string]string{"src": tr.URL, "kind": kind, "data-cui-internal": ""}
		if tr.Lang != "" {
			attrs["srclang"] = tr.Lang
		}
		if tr.Label != "" {
			attrs["label"] = tr.Label
		}
		if tr.Default {
			attrs["default"] = ""
		}
		kids = append(kids, render.VoidTag("track", attrs))
	}

	attrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "src", "poster", "preload")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	attrs["controls"] = ""
	attrs["playsinline"] = ""
	attrs["width"] = strconv.Itoa(cfg.Width)
	attrs["height"] = strconv.Itoa(cfg.Height)
	attrs["preload"] = "none"
	if cfg.Eager {
		attrs["preload"] = "metadata"
	}
	if cfg.Poster != "" && urlsafe.OK(cfg.Poster, urlsafe.ImageSource) {
		attrs["poster"] = cfg.Poster
	}
	if cfg.Label != "" {
		attrs["aria-label"] = cfg.Label
	}
	cls := "fui-video"
	if cfg.Aspect != ImageAspectAuto {
		cls += " fui-video--aspect-" + string(cfg.Aspect)
	}
	if cfg.Rounded {
		cls += " fui-video--rounded"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return videoStyle.WrapHTML(html.Video(html.VideoConfig{ID: cfg.ID, Class: cls, ExtraAttrs: attrs}, kids...))
}

var videoStyle = registry.RegisterStyle("ui-video", videoCSS)

func videoCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-video"] {
  display: block;
  max-inline-size: 100%;
  block-size: auto;
  object-fit: contain;
  background: var(--color-surface-soft, #F4F4F5);
}
:where([data-cui-comp="ui-video"]).fui-video--rounded { border-radius: var(--radii-md, 8px); }
:where([data-cui-comp="ui-video"]).fui-video--aspect-1-1  { aspect-ratio: 1 / 1;  inline-size: 100%; }
:where([data-cui-comp="ui-video"]).fui-video--aspect-4-3  { aspect-ratio: 4 / 3;  inline-size: 100%; }
:where([data-cui-comp="ui-video"]).fui-video--aspect-16-9 { aspect-ratio: 16 / 9; inline-size: 100%; }
:where([data-cui-comp="ui-video"]).fui-video--aspect-21-9 { aspect-ratio: 21 / 9; inline-size: 100%; }
:where([data-cui-comp="ui-video"]).fui-video--aspect-3-4  { aspect-ratio: 3 / 4;  inline-size: 100%; }`
}
