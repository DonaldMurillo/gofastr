package uihost

import (
	"fmt"
	stdimage "image"
	"image/draw"
	"log/slog"
	"net/http"
	"sort"

	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
)

// appIconSizes are the square pixel sizes derived from a WithAppIcon
// source: 32 (classic favicon), 180 (apple-touch-icon), 192 and 512
// (Android / PWA manifest).
var appIconSizes = []int{32, 180, 192, 512}

// appIconPath returns the serving path for a generated icon size.
func appIconPath(size int) string {
	return fmt.Sprintf("/__gofastr/icons/icon-%d.png", size)
}

// WithAppIcon derives the app's entire icon surface from ONE source
// image (PNG/JPEG/WebP…, ideally ≥512px; non-square sources are
// center-cropped). The host generates 32/180/192/512px PNGs at startup,
// serves them under /__gofastr/icons/, answers /favicon.ico with the
// 32px icon, and emits the matching <link rel="icon"> and
// <link rel="apple-touch-icon"> head tags. When WithPWA is enabled and
// PWAConfig.Icons is empty, the 192/512 icons also populate the
// manifest (explicitly declared icons always win).
//
// Pair it with go:embed for a zero-file-management setup:
//
//	//go:embed logo.png
//	var logo []byte
//	uihost.New(app, uihost.WithAppIcon(logo))
//
// An undecodable source logs a warning and leaves the host without
// icons (the /favicon.ico 204 fallback still applies) — icons are
// decoration, not something worth failing startup over.
func WithAppIcon(source []byte) Option {
	return func(ds *UIHost) {
		src, err := fwimage.DecodeBytes(source)
		if err != nil {
			slog.Default().Warn("uihost: WithAppIcon source is not a decodable image; skipping icon generation", "err", err)
			return
		}
		sq := squareCrop(src)
		icons := make(map[string][]byte, len(appIconSizes))
		for _, size := range appIconSizes {
			b, err := sq.Resize(size, size).PNG().Bytes()
			if err != nil {
				slog.Default().Warn("uihost: WithAppIcon failed to encode icon; skipping icon generation", "size", size, "err", err)
				return
			}
			icons[appIconPath(size)] = b
		}
		ds.appIcons = icons
		ds.headTags = append(ds.headTags,
			fmt.Sprintf(`<link rel="icon" type="image/png" sizes="32x32" href="%s">`, appIconPath(32)),
			fmt.Sprintf(`<link rel="icon" type="image/png" sizes="192x192" href="%s">`, appIconPath(192)),
			fmt.Sprintf(`<link rel="apple-touch-icon" sizes="180x180" href="%s">`, appIconPath(180)),
		)
	}
}

// squareCrop returns img center-cropped to a square. Square sources are
// returned unchanged.
func squareCrop(img *fwimage.Image) *fwimage.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == h {
		return img
	}
	side := w
	if h < side {
		side = h
	}
	x0 := b.Min.X + (w-side)/2
	y0 := b.Min.Y + (h-side)/2
	dst := stdimage.NewNRGBA(stdimage.Rect(0, 0, side, side))
	draw.Draw(dst, dst.Bounds(), img.GoImage(), stdimage.Pt(x0, y0), draw.Src)
	return fwimage.FromImage(dst, img.Format())
}

// AppIconAssets returns the generated icon files (URL path → PNG bytes)
// plus the /favicon.ico alias. Empty when WithAppIcon was not configured
// (or its source failed to decode). The static exporter dumps these so
// a serverless deploy ships the same icon surface as the live server.
func (ds *UIHost) AppIconAssets() map[string][]byte {
	if len(ds.appIcons) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(ds.appIcons)+1)
	for p, b := range ds.appIcons {
		out[p] = b
	}
	out["/favicon.ico"] = ds.appIcons[appIconPath(32)]
	return out
}

// reconcileAppIcons runs once after all options are applied (option
// order must not matter): when the host has generated icons AND a PWA
// config with no explicitly declared icons, the generated 192/512
// icons populate the manifest.
func (ds *UIHost) reconcileAppIcons() {
	if len(ds.appIcons) == 0 || ds.pwaConfig == nil || len(ds.pwaConfig.Icons) > 0 {
		return
	}
	ds.pwaConfig.Icons = []PWAIcon{
		{Src: appIconPath(192), Sizes: "192x192", Type: "image/png"},
		{Src: appIconPath(512), Sizes: "512x512", Type: "image/png"},
	}
}

// mountAppIcons registers the generated icon routes, including the
// /favicon.ico alias (PNG bytes; every current browser resolves the
// icon by Content-Type, not extension). No-op without WithAppIcon —
// /favicon.ico then keeps its 204 no-favicon fallback in serveOrRender.
func (ds *UIHost) mountAppIcons(register func(path string, h http.HandlerFunc)) {
	if len(ds.appIcons) == 0 {
		return
	}
	paths := make([]string, 0, len(ds.appIcons))
	for p := range ds.appIcons {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		body := ds.appIcons[p]
		register(p, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Write(body)
		})
	}
	favicon := ds.appIcons[appIconPath(32)]
	register("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(favicon)
	})
}
