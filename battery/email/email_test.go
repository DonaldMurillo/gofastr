package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmailCreation(t *testing.T) {
	email := Email{
		From:     "sender@example.com",
		To:       []string{"alice@example.com", "bob@example.com"},
		CC:       []string{"cc@example.com"},
		BCC:      []string{"bcc@example.com"},
		Subject:  "Test Subject",
		TextBody: "Hello, World!",
		HTMLBody: "<h1>Hello, World!</h1>",
		Attachments: []Attachment{
			{
				Filename:    "test.txt",
				Content:     []byte("test content"),
				ContentType: "text/plain",
			},
		},
		Headers: map[string]string{
			"X-Custom": "value",
		},
	}

	if email.From != "sender@example.com" {
		t.Errorf("expected From to be sender@example.com, got %s", email.From)
	}
	if len(email.To) != 2 {
		t.Errorf("expected 2 To recipients, got %d", len(email.To))
	}
	if len(email.CC) != 1 {
		t.Errorf("expected 1 CC recipient, got %d", len(email.CC))
	}
	if len(email.BCC) != 1 {
		t.Errorf("expected 1 BCC recipient, got %d", len(email.BCC))
	}
	if email.Subject != "Test Subject" {
		t.Errorf("expected Subject to be Test Subject, got %s", email.Subject)
	}
	if email.TextBody != "Hello, World!" {
		t.Errorf("expected TextBody to be Hello, World!, got %s", email.TextBody)
	}
	if email.HTMLBody != "<h1>Hello, World!</h1>" {
		t.Errorf("expected HTMLBody, got %s", email.HTMLBody)
	}
	if len(email.Attachments) != 1 {
		t.Errorf("expected 1 attachment, got %d", len(email.Attachments))
	}
	if email.Attachments[0].Filename != "test.txt" {
		t.Errorf("expected attachment filename test.txt, got %s", email.Attachments[0].Filename)
	}
	if email.Headers["X-Custom"] != "value" {
		t.Errorf("expected X-Custom header to be value, got %s", email.Headers["X-Custom"])
	}
}

func TestEmailEmptyFields(t *testing.T) {
	email := Email{
		From:    "sender@example.com",
		To:      []string{"alice@example.com"},
		Subject: "Minimal",
	}

	if len(email.CC) != 0 {
		t.Errorf("expected no CC, got %d", len(email.CC))
	}
	if len(email.BCC) != 0 {
		t.Errorf("expected no BCC, got %d", len(email.BCC))
	}
	if len(email.Attachments) != 0 {
		t.Errorf("expected no attachments, got %d", len(email.Attachments))
	}
	if len(email.Headers) != 0 {
		t.Errorf("expected no headers, got %d", len(email.Headers))
	}
}

func TestLogSenderDefault(t *testing.T) {
	sender := NewLogSender()
	if sender == nil {
		t.Fatal("expected non-nil LogSender")
	}
	if sender.w != os.Stdout {
		t.Error("expected default writer to be os.Stdout")
	}
}

func TestLogSenderCustomWriter(t *testing.T) {
	var buf bytes.Buffer
	sender := NewLogSender(&buf)

	email := Email{
		From:     "sender@example.com",
		To:       []string{"alice@example.com"},
		Subject:  "Test Log",
		TextBody: "Hello from log sender",
	}

	err := sender.Send(context.Background(), email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "sender@example.com") {
		t.Error("expected output to contain sender email")
	}
	if !strings.Contains(output, "alice@example.com") {
		t.Error("expected output to contain recipient email")
	}
	if !strings.Contains(output, "Test Log") {
		t.Error("expected output to contain subject")
	}
	if !strings.Contains(output, "Hello from log sender") {
		t.Error("expected output to contain text body")
	}
}

func TestLogSenderHTMLBody(t *testing.T) {
	var buf bytes.Buffer
	sender := NewLogSender(&buf)

	email := Email{
		From:     "sender@example.com",
		To:       []string{"alice@example.com"},
		Subject:  "HTML Test",
		HTMLBody: "<h1>Hello HTML</h1>",
	}

	err := sender.Send(context.Background(), email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "<h1>Hello HTML</h1>") {
		t.Error("expected output to contain HTML body")
	}
}

func TestLogSenderWithAttachments(t *testing.T) {
	var buf bytes.Buffer
	sender := NewLogSender(&buf)

	email := Email{
		From:    "sender@example.com",
		To:      []string{"alice@example.com"},
		Subject: "With Attachment",
		Attachments: []Attachment{
			{
				Filename:    "doc.pdf",
				Content:     []byte("PDF content here"),
				ContentType: "application/pdf",
			},
		},
	}

	err := sender.Send(context.Background(), email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "doc.pdf") {
		t.Error("expected output to contain attachment filename")
	}
	if !strings.Contains(output, "application/pdf") {
		t.Error("expected output to contain attachment content type")
	}
}

func TestLogSenderWithCCandBCC(t *testing.T) {
	var buf bytes.Buffer
	sender := NewLogSender(&buf)

	email := Email{
		From:    "sender@example.com",
		To:      []string{"alice@example.com"},
		CC:      []string{"cc1@example.com", "cc2@example.com"},
		BCC:     []string{"bcc@example.com"},
		Subject: "CC and BCC",
	}

	err := sender.Send(context.Background(), email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "cc1@example.com") {
		t.Error("expected output to contain CC recipients")
	}
	// BCC must NOT appear — contradicts the old behaviour but matches the
	// security contract in TestLogSender_DoesNotExposeBCCRecipients.
	if strings.Contains(output, "bcc@example.com") {
		t.Error("LogSender must not expose BCC recipients in logs")
	}
}

func TestTemplateExecution(t *testing.T) {
	tmpl := Template{
		Subject:  "Welcome, {{.Name}}!",
		TextBody: "Hello {{.Name}},\nYour account is ready.",
		HTMLBody: "<h1>Welcome, {{.Name}}!</h1><p>Your account is ready.</p>",
	}

	data := map[string]any{
		"Name": "Alice",
	}

	email, err := Execute(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if email.Subject != "Welcome, Alice!" {
		t.Errorf("expected subject 'Welcome, Alice!', got %q", email.Subject)
	}
	if !strings.Contains(email.TextBody, "Hello Alice,") {
		t.Errorf("expected text body to contain 'Hello Alice,', got %q", email.TextBody)
	}
	if !strings.Contains(email.HTMLBody, "<h1>Welcome, Alice!</h1>") {
		t.Errorf("expected HTML body to contain rendered name, got %q", email.HTMLBody)
	}
}

func TestTemplateExecutionMultipleVariables(t *testing.T) {
	tmpl := Template{
		Subject:  "Order #{{.OrderID}} confirmed",
		TextBody: "Hi {{.Name}}, your order of ${{.Amount}} is confirmed.",
		HTMLBody: "<p>Hi {{.Name}}, your order of ${{.Amount}} is confirmed.</p>",
	}

	data := map[string]any{
		"Name":    "Bob",
		"OrderID": "12345",
		"Amount":  "49.99",
	}

	email, err := Execute(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if email.Subject != "Order #12345 confirmed" {
		t.Errorf("expected subject 'Order #12345 confirmed', got %q", email.Subject)
	}
	if !strings.Contains(email.TextBody, "Hi Bob,") {
		t.Errorf("expected text body to contain 'Hi Bob,', got %q", email.TextBody)
	}
	if !strings.Contains(email.TextBody, "$49.99") {
		t.Errorf("expected text body to contain '$49.99', got %q", email.TextBody)
	}
}

func TestTemplateExecutionTextOnly(t *testing.T) {
	tmpl := Template{
		Subject:  "Hello {{.Name}}",
		TextBody: "Text only body for {{.Name}}.",
	}

	data := map[string]any{"Name": "Charlie"}

	email, err := Execute(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if email.TextBody != "Text only body for Charlie." {
		t.Errorf("expected text body, got %q", email.TextBody)
	}
	if email.HTMLBody != "" {
		t.Errorf("expected empty HTML body, got %q", email.HTMLBody)
	}
}

func TestTemplateExecutionHTMLOnly(t *testing.T) {
	tmpl := Template{
		Subject:  "HTML Only",
		HTMLBody: "<p>Hello {{.Name}}</p>",
	}

	data := map[string]any{"Name": "Dave"}

	email, err := Execute(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if email.TextBody != "" {
		t.Errorf("expected empty text body, got %q", email.TextBody)
	}
	if email.HTMLBody != "<p>Hello Dave</p>" {
		t.Errorf("expected HTML body, got %q", email.HTMLBody)
	}
}

func TestLoadFromDir(t *testing.T) {
	// Create a temporary directory with template files.
	dir := t.TempDir()

	// Write a welcome template pair.
	txtContent := "Subject: Welcome!\nHello {{.Name}}, welcome aboard!"
	if err := os.WriteFile(filepath.Join(dir, "welcome.txt"), []byte(txtContent), 0644); err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	htmlContent := "<h1>Welcome!</h1><p>Hello {{.Name}}, welcome aboard!</p>"
	if err := os.WriteFile(filepath.Join(dir, "welcome.html"), []byte(htmlContent), 0644); err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	templates, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(templates))
	}

	tmpl, ok := templates["welcome"]
	if !ok {
		t.Fatal("expected template named 'welcome'")
	}

	if tmpl.Subject != "Welcome!" {
		t.Errorf("expected subject 'Welcome!', got %q", tmpl.Subject)
	}
	if !strings.Contains(tmpl.TextBody, "Hello {{.Name}}") {
		t.Errorf("expected text body to contain template syntax, got %q", tmpl.TextBody)
	}
	if !strings.Contains(tmpl.HTMLBody, "<h1>Welcome!</h1>") {
		t.Errorf("expected HTML body to contain h1 tag, got %q", tmpl.HTMLBody)
	}
}

func TestLoadFromDirWithSubjectFile(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "reset.subject"), []byte("Password Reset"), 0644); err != nil {
		t.Fatalf("failed to write subject file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reset.txt"), []byte("Click here to reset: {{.URL}}"), 0644); err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	templates, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmpl, ok := templates["reset"]
	if !ok {
		t.Fatal("expected template named 'reset'")
	}
	if tmpl.Subject != "Password Reset" {
		t.Errorf("expected subject 'Password Reset', got %q", tmpl.Subject)
	}
}

func TestLoadFromDirEmpty(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadFromDir(dir)
	if err == nil {
		t.Error("expected error for empty directory")
	}
}

func TestLoadFromDirNonexistent(t *testing.T) {
	_, err := LoadFromDir("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestLoadFromDirHTMLOnly(t *testing.T) {
	dir := t.TempDir()

	htmlContent := "Subject: HTML Only\n<p>Hello {{.Name}}</p>"
	if err := os.WriteFile(filepath.Join(dir, "promo.html"), []byte(htmlContent), 0644); err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	templates, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmpl, ok := templates["promo"]
	if !ok {
		t.Fatal("expected template named 'promo'")
	}
	if tmpl.TextBody != "" {
		t.Errorf("expected empty text body, got %q", tmpl.TextBody)
	}
	if !strings.Contains(tmpl.HTMLBody, "<p>Hello {{.Name}}</p>") {
		t.Errorf("expected HTML body, got %q", tmpl.HTMLBody)
	}
}

func TestSMTPConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  SMTPConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: SMTPConfig{
				Host:     "smtp.example.com",
				Port:     587,
				Username: "user@example.com",
				Password: "password",
				UseTLS:   false,
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: SMTPConfig{
				Port:     587,
				Username: "user@example.com",
				Password: "password",
			},
			wantErr: true,
		},
		{
			name: "port zero",
			config: SMTPConfig{
				Host: "smtp.example.com",
				Port: 0,
			},
			wantErr: true,
		},
		{
			name: "port too large",
			config: SMTPConfig{
				Host: "smtp.example.com",
				Port: 70000,
			},
			wantErr: true,
		},
		{
			name: "valid with TLS",
			config: SMTPConfig{
				Host:   "smtp.gmail.com",
				Port:   465,
				UseTLS: true,
			},
			wantErr: false,
		},
		{
			name: "valid without auth",
			config: SMTPConfig{
				Host: "localhost",
				Port: 1025,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSMTPConfigAddr(t *testing.T) {
	config := SMTPConfig{Host: "smtp.example.com", Port: 587}
	expected := "smtp.example.com:587"
	if config.addr() != expected {
		t.Errorf("expected %q, got %q", expected, config.addr())
	}
}

func TestSMTPSenderNew(t *testing.T) {
	config := SMTPConfig{
		Host: "smtp.example.com",
		Port: 587,
	}
	sender := NewSMTPSender(config)
	if sender == nil {
		t.Fatal("expected non-nil SMTPSender")
	}
	if sender.config.Host != "smtp.example.com" {
		t.Errorf("expected host smtp.example.com, got %s", sender.config.Host)
	}
}

func TestSMTPSenderNoRecipients(t *testing.T) {
	sender := NewSMTPSender(SMTPConfig{
		Host: "localhost",
		Port: 1025,
	})

	email := Email{
		From:    "sender@example.com",
		Subject: "No recipients",
	}

	err := sender.Send(context.Background(), email)
	if err == nil {
		t.Error("expected error for no recipients")
	}
	if !strings.Contains(err.Error(), "at least one recipient") {
		t.Errorf("expected recipient error, got %v", err)
	}
}

func TestAttachmentEncoding(t *testing.T) {
	// Test that attachments are correctly base64-encoded in the message.
	email := Email{
		From:     "sender@example.com",
		To:       []string{"alice@example.com"},
		Subject:  "Test Attachments",
		TextBody: "See attached.",
		Attachments: []Attachment{
			{
				Filename:    "hello.txt",
				Content:     []byte("Hello, World!"),
				ContentType: "text/plain",
			},
		},
	}

	msg, err := buildMessage(email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgStr := string(msg)

	// Verify base64-encoded content is present.
	expected := base64.StdEncoding.EncodeToString([]byte("Hello, World!"))
	if !strings.Contains(msgStr, expected) {
		t.Errorf("expected base64 content %q in message, got %s", expected, msgStr)
	}

	// Verify attachment headers.
	if !strings.Contains(msgStr, `name="hello.txt"`) {
		t.Error("expected attachment filename in message")
	}
	if !strings.Contains(msgStr, "text/plain") {
		t.Error("expected text/plain content type in message")
	}
	if !strings.Contains(msgStr, "Content-Transfer-Encoding: base64") {
		t.Error("expected base64 transfer encoding in message")
	}
}

func TestBuildMessageSimple(t *testing.T) {
	email := Email{
		From:     "sender@example.com",
		To:       []string{"alice@example.com"},
		Subject:  "Simple",
		TextBody: "Just text.",
	}

	msg, err := buildMessage(email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgStr := string(msg)
	if !strings.Contains(msgStr, "From: sender@example.com") {
		t.Error("expected From header")
	}
	if !strings.Contains(msgStr, "To: alice@example.com") {
		t.Error("expected To header")
	}
	if !strings.Contains(msgStr, "Subject: Simple") {
		t.Error("expected Subject header")
	}
	if !strings.Contains(msgStr, "Just text.") {
		t.Error("expected text body")
	}
}

func TestBuildMessageWithCC(t *testing.T) {
	email := Email{
		From:    "sender@example.com",
		To:      []string{"alice@example.com"},
		CC:      []string{"bob@example.com"},
		Subject: "With CC",
	}

	msg, err := buildMessage(email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgStr := string(msg)
	if !strings.Contains(msgStr, "Cc: bob@example.com") {
		t.Error("expected Cc header")
	}
}

func TestBuildMessageCustomHeaders(t *testing.T) {
	email := Email{
		From:    "sender@example.com",
		To:      []string{"alice@example.com"},
		Subject: "Custom Headers",
		Headers: map[string]string{
			"X-Mailer":   "GoFastr",
			"X-Priority": "1",
		},
	}

	msg, err := buildMessage(email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgStr := string(msg)
	if !strings.Contains(msgStr, "X-Mailer: GoFastr") {
		t.Error("expected X-Mailer header")
	}
	if !strings.Contains(msgStr, "X-Priority: 1") {
		t.Error("expected X-Priority header")
	}
}

func TestBuildMessageMultipart(t *testing.T) {
	email := Email{
		From:     "sender@example.com",
		To:       []string{"alice@example.com"},
		Subject:  "Multipart",
		TextBody: "Plain text version.",
		HTMLBody: "<p>HTML version.</p>",
	}

	msg, err := buildMessage(email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgStr := string(msg)
	if !strings.Contains(msgStr, "multipart/mixed") {
		t.Error("expected multipart content type")
	}
	if !strings.Contains(msgStr, "Plain text version.") {
		t.Error("expected text body in multipart message")
	}
	if !strings.Contains(msgStr, "<p>HTML version.</p>") {
		t.Error("expected HTML body in multipart message")
	}
}

func TestErrSendFailed(t *testing.T) {
	err := fmt.Errorf("%w: something went wrong", ErrSendFailed)
	if !IsErrSendFailed(err) {
		t.Error("expected error to match ErrSendFailed")
	}
}

func TestSenderInterface(t *testing.T) {
	// Verify both SMTPSender and LogSender implement Sender.
	var _ Sender = (*SMTPSender)(nil)
	var _ Sender = (*LogSender)(nil)
}

// IsErrSendFailed checks if an error wraps ErrSendFailed.
func IsErrSendFailed(err error) bool {
	return err != nil && strings.Contains(err.Error(), "email: send failed")
}
