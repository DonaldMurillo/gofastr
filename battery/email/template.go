package email

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	ttpl "text/template"
)

// Template represents an email template with subject, text, and HTML
// bodies.
type Template struct {
	Subject  string
	TextBody string
	HTMLBody string
}

// Execute fills the template with the provided data and returns a
// complete Email.
func Execute(tmpl Template, data map[string]any) (Email, error) {
	var email Email

	subject, err := renderTemplate(ttpl.New, "text", tmpl.Subject, data)
	if err != nil {
		return Email{}, fmt.Errorf("email: render subject: %w", err)
	}
	email.Subject = subject

	if tmpl.TextBody != "" {
		text, err := renderTemplate(ttpl.New, "text", tmpl.TextBody, data)
		if err != nil {
			return Email{}, fmt.Errorf("email: render text body: %w", err)
		}
		email.TextBody = text
	}

	if tmpl.HTMLBody != "" {
		html, err := renderTemplate(template.New, "html", tmpl.HTMLBody, data)
		if err != nil {
			return Email{}, fmt.Errorf("email: render html body: %w", err)
		}
		email.HTMLBody = html
	}

	return email, nil
}

func renderTemplate[T interface {
	Parse(string) (T, error)
	Execute(io.Writer, any) error
}](newT func(string) T, name, src string, data map[string]any) (string, error) {
	t, err := newT(name).Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// LoadFromDir loads email templates from a directory.
func LoadFromDir(dir string) (map[string]Template, error) {
	templates := make(map[string]Template)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("email: read template dir: %w", err)
	}

	fileMap := make(map[string]map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".txt" && ext != ".html" && ext != ".subject" {
			continue
		}

		base := strings.TrimSuffix(name, ext)
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("email: read template file %s: %w", name, err)
		}

		if fileMap[base] == nil {
			fileMap[base] = make(map[string]string)
		}
		fileMap[base][ext] = string(content)
	}

	for base, files := range fileMap {
		templates[base] = templateFromFiles(files)
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("email: no templates found in %s", dir)
	}

	return templates, nil
}

// LoadFromFS loads email templates from an fs.FS.
func LoadFromFS(fsys fs.FS) (map[string]Template, error) {
	templates := make(map[string]Template)

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("email: read template fs: %w", err)
	}

	fileMap := make(map[string]map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".txt" && ext != ".html" && ext != ".subject" {
			continue
		}

		base := strings.TrimSuffix(name, ext)
		content, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("email: read template file %s: %w", name, err)
		}

		if fileMap[base] == nil {
			fileMap[base] = make(map[string]string)
		}
		fileMap[base][ext] = string(content)
	}

	for base, files := range fileMap {
		templates[base] = templateFromFiles(files)
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("email: no templates found in fs")
	}

	return templates, nil
}

func templateFromFiles(files map[string]string) Template {
	var tmpl Template
	if subject, ok := files[".subject"]; ok {
		tmpl.Subject = strings.TrimSpace(subject)
	}
	if text, ok := files[".txt"]; ok {
		tmpl.Subject, tmpl.TextBody = stripSubjectLine(tmpl.Subject, text)
	}
	if html, ok := files[".html"]; ok {
		tmpl.Subject, tmpl.HTMLBody = stripSubjectLine(tmpl.Subject, html)
	}
	return tmpl
}

func stripSubjectLine(subject, body string) (string, string) {
	if subject != "" {
		return subject, body
	}
	lines := strings.SplitN(body, "\n", 2)
	after, ok := strings.CutPrefix(lines[0], "Subject: ")
	if !ok {
		return subject, body
	}
	after = strings.TrimSuffix(after, "\r")
	if len(lines) > 1 {
		return after, lines[1]
	}
	return after, ""
}
