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

// Template represents an email template with subject, text, and HTML bodies.
// Fields may contain Go template directives like {{.Name}}.
type Template struct {
	Subject  string
	TextBody string
	HTMLBody string
}

// Execute fills the template with the provided data and returns a complete Email.
// The From and To fields must be set by the caller after execution.
func Execute(tmpl Template, data map[string]any) (Email, error) {
	var email Email

	// Render subject
	subject, err := renderTemplate(ttpl.New, "text", tmpl.Subject, data)
	if err != nil {
		return Email{}, fmt.Errorf("email: render subject: %w", err)
	}
	email.Subject = subject

	// Render text body
	if tmpl.TextBody != "" {
		text, err := renderTemplate(ttpl.New, "text", tmpl.TextBody, data)
		if err != nil {
			return Email{}, fmt.Errorf("email: render text body: %w", err)
		}
		email.TextBody = text
	}

	// Render HTML body
	if tmpl.HTMLBody != "" {
		html, err := renderTemplate(template.New, "html", tmpl.HTMLBody, data)
		if err != nil {
			return Email{}, fmt.Errorf("email: render html body: %w", err)
		}
		email.HTMLBody = html
	}

	return email, nil
}

// renderTemplate parses src with the passed template constructor
// (text/template's or html/template's New) and executes it against
// data, returning the rendered string. name is the template name error
// messages carry. It replaces the former executeText and executeHTML,
// whose bodies were identical except for the engine and that name.
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
// It expects files with .txt and .html extensions, paired by name.
// For example, "welcome.txt" and "welcome.html" become a Template named "welcome".
// Subject lines are extracted from the first line of .txt files if it starts
// with "Subject: ", or from .html files if no .txt is present.
// Alternatively, a .subject file with the same base name can provide the subject.
func LoadFromDir(dir string) (map[string]Template, error) {
	templates := make(map[string]Template)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("email: read template dir: %w", err)
	}

	// Group files by base name (without extension).
	fileMap := make(map[string]map[string]string) // basename -> ext -> content
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

// templateFromFiles builds one Template from a basename's grouped
// contents (extension → file bytes): the subject comes from a .subject
// file when present, otherwise from a leading "Subject: " line
// stripped off the .txt or .html body. Replaces the two identical
// build loops formerly inline in LoadFromDir and LoadFromFS.
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

// stripSubjectLine extracts a leading "Subject: " line from body when
// subject is empty, returning the new subject and the remaining body
// (empty when the subject line was the only line). Replaces the two
// near-identical .txt/.html extraction blocks the build loops carried.
func stripSubjectLine(subject, body string) (string, string) {
	if subject != "" {
		return subject, body
	}
	lines := strings.SplitN(body, "\n", 2)
	after, ok := strings.CutPrefix(lines[0], "Subject: ")
	if !ok {
		return subject, body
	}
	if len(lines) > 1 {
		return after, lines[1]
	}
	return after, ""
}
