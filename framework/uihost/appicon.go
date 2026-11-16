package uihost

import (
	"fmt"
	stdimage "image"
	"image/draw"
	"log/slog"
	"maps"
	"net/http"
	"sort"

	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
)

var appIconSizes = []int{32, 180, 192, 512}

func appIconPath(size int) string {
	return fmt.Sprintf("/__gofastr/icons/icon-%d.png", size)
}

// WithAppIcon derives 32, 180, 192 and 512 px PNG icons, the favicon and
// their head links from one source image; an undecodable source logs a
// warning and leaves the host without icons.
func WithAppIcon(source []byte) Option {
	return func(ds *UIHost) {
		src, err := fwimage.DecodeBytes(source)
		if err != nil {
			slog.Default().Warn("uihost: WithAppIcon source is not a decodable image; skipping icon generation", "err", err)
			return
		}
		sq := squareCrop(src.AutoOrient())
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

func squareCrop(img *fwimage.Image) *fwimage.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == h {
		return img
	}
	side := min(h, w)
	x0 := b.Min.X + (w-side)/2
	y0 := b.Min.Y + (h-side)/2
	dst := stdimage.NewNRGBA(stdimage.Rect(0, 0, side, side))
	draw.Draw(dst, dst.Bounds(), img.GoImage(), stdimage.Pt(x0, y0), draw.Src)
	return fwimage.FromImage(dst, img.Format())
}

// AppIconAssets maps each generated icon path, and /favicon.ico, to its
// PNG bytes; nil without WithAppIcon.
func (ds *UIHost) AppIconAssets() map[string][]byte {
	if len(ds.appIcons) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(ds.appIcons)+1)
	maps.Copy(out, ds.appIcons)
	out["/favicon.ico"] = ds.appIcons[appIconPath(32)]
	return out
}

func (ds *UIHost) reconcileAppIcons() {
	if len(ds.appIcons) == 0 || ds.pwaConfig == nil || len(ds.pwaConfig.Icons) > 0 {
		return
	}
	ds.pwaConfig.Icons = []PWAIcon{
		{Src: appIconPath(192), Sizes: "192x192", Type: "image/png"},
		{Src: appIconPath(512), Sizes: "512x512", Type: "image/png"},
	}
}

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
