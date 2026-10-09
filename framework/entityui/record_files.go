package entityui

import (
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Image and File fields: a stored value is either a URL (absolute, or a
// path on this origin) or a storage key the app serves under
// Extensions.FilesURL. With file storage on the app the form takes an
// upload; without it the value stays an editable URL.

// uploadMaxMB is the size the upload control announces: the cap
// framework/file enforces on every upload.
const uploadMaxMB = int(file.MaxProcessFileSize >> 20)

// fileHref is where a stored Image or File value is fetched from: an
// http(s) URL or a same-origin path as is, a storage key under
// FilesURL with each segment escaped, and "" for anything else (a
// script scheme, a key that climbs with "..", a key with no FilesURL).
func (u *UI) fileHref(val string) string {
	val = strings.TrimSpace(val)
	if val == "" || strings.ContainsAny(val, "\\\x00") {
		return ""
	}
	if strings.HasPrefix(val, "/") && !strings.HasPrefix(val, "//") {
		return val
	}
	if pu, err := url.Parse(val); err == nil && pu.Scheme != "" {
		if (pu.Scheme == "https" || pu.Scheme == "http") && pu.Host != "" {
			return val
		}
		return ""
	}
	if u.ext.FilesURL == "" || strings.Contains(val, ":") {
		return ""
	}
	parts := strings.Split(val, "/")
	for i, p := range parts {
		if p == "" || p == "." || p == ".." {
			return ""
		}
		parts[i] = url.PathEscape(p)
	}
	return u.ext.FilesURL + strings.Join(parts, "/")
}

// uploads reports whether the entity's writes take a file: its CRUD
// handler has storage, so a multipart save stores the upload.
func (m *meta) uploads() bool { return m.hasAPI && m.ch != nil && m.ch.Storage != nil }

// fileValue draws a stored file read-only: a thumbnail for an image, a
// link named by its file name for a file, the empty mark when it has no
// address.
func (u *UI) fileValue(f schema.Field, label, val string, size ui.ThumbnailSize) render.HTML {
	href := u.fileHref(val)
	if href == "" {
		return ""
	}
	if f.Type == schema.Image {
		return ui.Thumbnail(ui.ThumbnailConfig{Src: href, Alt: label, Size: size})
	}
	name := path.Base(val)
	if un, err := url.PathUnescape(name); err == nil {
		name = un
	}
	return ui.Link(ui.LinkConfig{Href: href, Text: name})
}

// uploadInput draws an Image or File field as an upload: the stored
// file above a file input of the types the field takes. Leaving the
// input empty keeps the stored file (the runtime sends no part for it).
func (fb *formBuilder) uploadInput(ctx context.Context, f schema.Field, label, help, id, val string, required bool) render.HTML {
	accept := ""
	if f.Type == schema.Image {
		accept = strings.Join(file.ImageTypes, ",")
	}
	input := ui.FileUpload(ui.FileUploadConfig{
		Name: f.Name, ID: id, Label: label, Accept: accept,
		Required:  required && val == "",
		MaxSizeMB: uploadMaxMB,
		Help:      help,
	})
	if cur := fb.b.ui.fileValue(f, label, val, ui.ThumbnailLG); cur != "" {
		return ui.Stack(ui.StackConfig{Gap: ui.GapSM}, cur, input)
	}
	return input
}
