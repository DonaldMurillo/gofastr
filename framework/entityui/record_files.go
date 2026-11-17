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

const uploadMaxMB = int(file.MaxProcessFileSize >> 20)

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

func (m *meta) uploads() bool { return m.hasAPI && m.ch != nil && m.ch.Storage != nil }

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

func (fb *formBuilder) uploadInput(ctx context.Context, f schema.Field, label, help, id, val string, required bool) render.HTML {
	accept := ""
	if f.Type == schema.Image {
		accept = strings.Join(file.ImageTypes, ",")
	}
	input := ui.FileUpload(ui.FileUploadConfig{
		Name: fb.m.inputName(f.Name), ID: id, Label: label, Accept: accept,
		Required:  required && val == "",
		MaxSizeMB: uploadMaxMB,
		Help:      help,
		Ctx:       ctx,
	})
	if cur := fb.b.ui.fileValue(f, label, val, ui.ThumbnailLG); cur != "" {
		return ui.Stack(ui.StackConfig{Gap: ui.GapSM}, cur, input)
	}
	return input
}
