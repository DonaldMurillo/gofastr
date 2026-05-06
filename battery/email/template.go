package email

import (
	"bytes"
	"fmt"
	"html/template"
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
	subject, err := executeText(tmpl.Subject, data)
	if err != nil {
		return Email{}, fmt.Errorf("email: render subject: %w", err)
	}
	email.Subject = subject

	// Render text body
	if tmpl.TextBody != "" {
		text, err := executeText(tmpl.TextBody, data)
		if err != nil {
			return Email{}, fmt.Errorf("email: render text body: %w", err)
		}
		email.TextBody = text
	}

	// Render HTML body
	if tmpl.HTMLBody != "" {
		html, err := executeHTML(tmpl.HTMLBody, data)
		if err != nil {
			return Email{}, fmt.Errorf("email: render html body: %w", err)
		}
		email.HTMLBody = html
	}

	return email, nil
}

// executeText renders a text/template string with the given data.
func executeText(text string, data map[string]any) (string, error) {
	t, err := ttpl.New("text").Parse(text)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// executeHTML renders an html/template string with the given data.
func executeHTML(html string, data map[string]any) (string, error) {
	t, err := template.New("html").Parse(html)
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

	// Build templates from the file groups.
	for base, files := range fileMap {
		tmpl := Template{}

		// Subject from .subject file, or extract from text.
		if subject, ok := files[".subject"]; ok {
			tmpl.Subject = strings.TrimSpace(subject)
		}

		// Text body.
		if text, ok := files[".txt"]; ok {
			// Extract subject from first line if not already set.
			if tmpl.Subject == "" {
				lines := strings.SplitN(text, "\n", 2)
				if strings.HasPrefix(lines[0], "Subject: ") {
					tmpl.Subject = strings.TrimPrefix(lines[0], "Subject: ")
					if len(lines) > 1 {
						text = lines[1]
					} else {
						text = ""
					}
				}
			}
			tmpl.TextBody = text
		}

		// HTML body.
		if html, ok := files[".html"]; ok {
			// Extract subject from first line if not already set.
			if tmpl.Subject == "" {
				lines := strings.SplitN(html, "\n", 2)
				if strings.HasPrefix(lines[0], "Subject: ") {
					tmpl.Subject = strings.TrimPrefix(lines[0], "Subject: ")
					if len(lines) > 1 {
						html = lines[1]
					} else {
						html = ""
					}
				}
			}
			tmpl.HTMLBody = html
		}

		templates[base] = tmpl
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
		tmpl := Template{}

		if subject, ok := files[".subject"]; ok {
			tmpl.Subject = strings.TrimSpace(subject)
		}

		if text, ok := files[".txt"]; ok {
			if tmpl.Subject == "" {
				lines := strings.SplitN(text, "\n", 2)
				if strings.HasPrefix(lines[0], "Subject: ") {
					tmpl.Subject = strings.TrimPrefix(lines[0], "Subject: ")
					if len(lines) > 1 {
						text = lines[1]
					} else {
						text = ""
					}
				}
			}
			tmpl.TextBody = text
		}

		if html, ok := files[".html"]; ok {
			if tmpl.Subject == "" {
				lines := strings.SplitN(html, "\n", 2)
				if strings.HasPrefix(lines[0], "Subject: ") {
					tmpl.Subject = strings.TrimPrefix(lines[0], "Subject: ")
					if len(lines) > 1 {
						html = lines[1]
					} else {
						html = ""
					}
				}
			}
			tmpl.HTMLBody = html
		}

		templates[base] = tmpl
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("email: no templates found in fs")
	}

	return templates, nil
}
