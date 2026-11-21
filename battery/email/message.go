package email

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func buildMessage(email Email) ([]byte, error) {
	if err := assertNoHeaderInjection("From", email.From); err != nil {
		return nil, err
	}
	if err := assertNoHeaderInjection("Subject", email.Subject); err != nil {
		return nil, err
	}
	for _, a := range email.To {
		if err := assertNoHeaderInjection("To", a); err != nil {
			return nil, err
		}
	}
	for _, a := range email.CC {
		if err := assertNoHeaderInjection("Cc", a); err != nil {
			return nil, err
		}
	}
	for _, a := range email.BCC {
		if err := assertNoHeaderInjection("Bcc", a); err != nil {
			return nil, err
		}
	}
	if err := checkCustomHeaders(email.Headers); err != nil {
		return nil, err
	}
	for _, att := range email.Attachments {
		if err := assertNoHeaderInjection("Attachment.Filename", att.Filename); err != nil {
			return nil, err
		}
		if err := assertNoHeaderInjection("Attachment.ContentType", att.ContentType); err != nil {
			return nil, err
		}
	}

	var buf strings.Builder

	if err := writeHeader(&buf, "From", scrubHeaderValue(encodeAddrHeader(email.From))); err != nil {
		return nil, err
	}
	if err := writeHeader(&buf, "To", scrubHeaderValue(encodeAddrHeader(strings.Join(email.To, ", ")))); err != nil {
		return nil, err
	}
	if len(email.CC) > 0 {
		if err := writeHeader(&buf, "Cc", scrubHeaderValue(encodeAddrHeader(strings.Join(email.CC, ", ")))); err != nil {
			return nil, err
		}
	}
	if err := writeHeader(&buf, "Subject", scrubHeaderValue(encodeTextHeader(email.Subject))); err != nil {
		return nil, err
	}

	if !hasHeader(email.Headers, "Date") {
		buf.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	}
	if !hasHeader(email.Headers, "Message-ID") {
		id, err := newMessageID(email.From)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSendFailed, err)
		}
		if err := writeHeader(&buf, "Message-ID", scrubHeaderValue(id)); err != nil {
			return nil, err
		}
	}

	if err := writeCustomHeaders(&buf, email.Headers); err != nil {
		return nil, err
	}

	hasAttachments := len(email.Attachments) > 0
	hasTextAndHTML := email.TextBody != "" && email.HTMLBody != ""
	email.TextBody, email.HTMLBody = qpBody(email.TextBody), qpBody(email.HTMLBody)

	if hasAttachments || hasTextAndHTML {
		newChecked := func() (string, error) {
			boundary, err := newBoundary()
			if err != nil {
				return "", fmt.Errorf("%w: %v", ErrSendFailed, err)
			}
			if bodyContainsBoundary(email.TextBody, boundary) || bodyContainsBoundary(email.HTMLBody, boundary) {
				return "", fmt.Errorf("%w: message body contains the MIME boundary delimiter (refusing to send to prevent MIME part injection)", ErrSendFailed)
			}
			return boundary, nil
		}
		buf.WriteString("MIME-Version: 1.0\r\n")

		if !hasAttachments {
			if err := writeBodyEntity(&buf, email, newChecked); err != nil {
				return nil, err
			}
			return []byte(buf.String()), nil
		}

		boundary, err := newChecked()
		if err != nil {
			return nil, err
		}
		buf.WriteString("Content-Type: multipart/mixed; boundary=" + boundary + "\r\n")
		buf.WriteString("\r\n")

		if email.TextBody != "" || email.HTMLBody != "" {
			buf.WriteString("--" + boundary + "\r\n")
			if err := writeBodyEntity(&buf, email, newChecked); err != nil {
				return nil, err
			}
		}

		for _, att := range email.Attachments {
			ct := att.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			baseCT, err := safeMediaType(ct)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid attachment content-type: %v", ErrSendFailed, err)
			}
			buf.WriteString("--" + boundary + "\r\n")
			if err := writeHeader(&buf, "Content-Type", scrubHeaderValue(baseCT)+"; "+scrubHeaderValue(encodeParam("name", att.Filename))); err != nil {
				return nil, err
			}
			buf.WriteString("Content-Transfer-Encoding: base64\r\n")
			if err := writeHeader(&buf, "Content-Disposition", "attachment; "+scrubHeaderValue(encodeParam("filename", att.Filename))); err != nil {
				return nil, err
			}
			buf.WriteString("\r\n")
			buf.WriteString(encodeBase64(att.Content))
			buf.WriteString("\r\n")
		}

		buf.WriteString("--" + boundary + "--\r\n")
	} else {
		buf.WriteString("MIME-Version: 1.0\r\n")
		if email.HTMLBody != "" {
			buf.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
		} else {
			buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		}
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		if email.TextBody != "" {
			buf.WriteString(email.TextBody)
		} else if email.HTMLBody != "" {
			buf.WriteString(email.HTMLBody)
		}
	}

	return []byte(buf.String()), nil
}

func writeBodyEntity(buf *strings.Builder, email Email, mint func() (string, error)) error {
	textPart := func() {
		buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(email.TextBody + "\r\n")
	}
	htmlPart := func() {
		buf.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(email.HTMLBody + "\r\n")
	}
	switch {
	case email.TextBody != "" && email.HTMLBody != "":
		boundary, err := mint()
		if err != nil {
			return err
		}
		buf.WriteString("Content-Type: multipart/alternative; boundary=" + boundary + "\r\n")
		buf.WriteString("\r\n")
		buf.WriteString("--" + boundary + "\r\n")
		textPart()
		buf.WriteString("--" + boundary + "\r\n")
		htmlPart()
		buf.WriteString("--" + boundary + "--\r\n")
	case email.HTMLBody != "":
		htmlPart()
	default:
		textPart()
	}
	return nil
}

func qpBody(s string) string {
	var b strings.Builder
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}

func encodeBase64(data []byte) string {
	return b64Encode(data)
}

var newBoundary = randomBoundary

func randomBoundary() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("boundary generation failed: %w", err)
	}
	return "gofastr-boundary-" + hex.EncodeToString(b[:]), nil
}

func hasHeader(headers map[string]string, name string) bool {
	for k := range headers {
		if isASCII(k) && strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

func isFieldName(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if c := s[i]; c < 33 || c > 126 || c == ':' {
			return false
		}
	}
	return true
}

var reMessageIDDomain = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)

func newMessageID(from string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("message id generation failed: %w", err)
	}
	domain := "localhost"
	if addrs, err := mail.ParseAddressList(from); err == nil && len(addrs) > 0 {
		if at := strings.LastIndexByte(addrs[0].Address, '@'); at >= 0 {
			if d := addrs[0].Address[at+1:]; len(d) <= 253 && reMessageIDDomain.MatchString(d) {
				domain = d
			}
		}
	}
	return "<" + hex.EncodeToString(b[:]) + "@" + domain + ">", nil
}

func bodyContainsBoundary(body, boundary string) bool {
	delim := "--" + boundary
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, delim) {
			return true
		}
	}
	return false
}

func safeMediaType(ct string) (string, error) {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return "", fmt.Errorf("cannot parse media type %q: %w", ct, err)
	}
	return mt, nil
}

func quoteParamValue(s string) string {
	s = textsafe.StripInvisible(s)
	if strings.ContainsFunc(s, textsafe.IsC1) {
		s = strings.Map(func(r rune) rune {
			if textsafe.IsC1(r) {
				return -1
			}
			return r
		}, s)
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteByte('"')
	for i := range len(s) {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		if c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func encodeTextHeader(s string) string {
	s = scrubHeaderValue(s)
	enc := mime.QEncoding.Encode("utf-8", s)
	if enc != s {
		return enc
	}
	for _, w := range strings.Split(s, " ") {
		if len(w) > foldWidth-1 {
			return qWords(strings.Trim(s, " "))
		}
	}
	return enc
}

func qWords(s string) string {
	const prefix, suffix = "=?utf-8?q?", "?="
	const maxBody = 75 - len(prefix) - len(suffix)
	var out, cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		if out.Len() > 0 {
			out.WriteByte(' ')
		}
		out.WriteString(prefix + cur.String() + suffix)
		cur.Reset()
	}
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		var unit strings.Builder
		for _, c := range []byte(s[i : i+size]) {
			if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("!*+-/", c) >= 0 {
				unit.WriteByte(c)
			} else {
				fmt.Fprintf(&unit, "=%02X", c)
			}
		}
		if cur.Len()+unit.Len() > maxBody {
			flush()
		}
		cur.WriteString(unit.String())
		i += size
	}
	flush()
	return out.String()
}

const (
	foldWidth     = 78
	maxLineOctets = 998
)

// word, so an encoded word stays whole. A nil buf checks folding without
// writing the header.
func writeHeader(buf *strings.Builder, name, value string) error {
	isWSP := func(c byte) bool { return c == ' ' || c == '\t' }
	line := name + ": "
	start := 0
	emit := func(chunk string) error {
		if len(line) > len(name)+2 && len(line)+len(chunk) > foldWidth {
			if len(line) > maxLineOctets {
				return fmt.Errorf("%w: header %s has a word too long to fold within %d octets", ErrSendFailed, name, maxLineOctets)
			}
			if buf != nil {
				buf.WriteString(line + "\r\n")
			}
			line = ""
		}
		line += chunk
		return nil
	}
	for i := 1; i < len(value)-1; i++ {
		if isWSP(value[i]) && !isWSP(value[i+1]) {
			if err := emit(value[start:i]); err != nil {
				return err
			}
			start = i
		}
	}
	if err := emit(value[start:]); err != nil {
		return err
	}
	if len(line) > maxLineOctets {
		return fmt.Errorf("%w: header %s has a word too long to fold within %d octets", ErrSendFailed, name, maxLineOctets)
	}
	if buf != nil {
		buf.WriteString(line + "\r\n")
	}
	return nil
}

func encodeAddrHeader(s string) string {
	s = scrubHeaderValue(s)
	if isASCII(s) {
		return s
	}
	addrs, err := mail.ParseAddressList(s)
	if err != nil {
		return s
	}
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = a.String()
	}
	return strings.Join(out, ", ")
}

func encodeParam(key, v string) string {
	if isASCII(v) {
		if q := key + "=" + quoteParamValue(v); len(q) <= maxParamOctets {
			return q
		}
	}
	var units []string
	for _, r := range textsafe.StripInvisible(v) {
		if r < 0x20 || r == 0x7f || textsafe.IsC1(r) {
			continue
		}
		var u strings.Builder
		for _, c := range utf8.AppendRune(nil, r) {
			if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
				u.WriteByte(c)
			} else {
				fmt.Fprintf(&u, "%%%02X", c)
			}
		}
		units = append(units, u.String())
	}
	if single := key + "*=utf-8''" + strings.Join(units, ""); len(single) <= maxParamOctets {
		return single
	}
	var sections []string
	var cur strings.Builder
	for _, u := range units {
		if cur.Len() > 0 && cur.Len()+len(u) > paramSectionOctets {
			sections = append(sections, cur.String())
			cur.Reset()
		}
		if cur.Len() == 0 {
			if len(sections) == 0 {
				cur.WriteString(key + "*0*=utf-8''")
			} else {
				fmt.Fprintf(&cur, "%s*%d*=", key, len(sections))
			}
		}
		cur.WriteString(u)
	}
	if cur.Len() > 0 {
		sections = append(sections, cur.String())
	}
	return strings.Join(sections, "; ")
}

const (
	maxParamOctets     = 900
	paramSectionOctets = foldWidth - 4
)

func scrubHeaderValue(s string) string {
	s = textsafe.StripInvisible(s)
	if strings.ContainsFunc(s, textsafe.IsC1) {
		s = strings.Map(func(r rune) rune {
			if textsafe.IsC1(r) {
				return -1
			}
			return r
		}, s)
	}
	buf := make([]byte, 0, len(s))
	for i := range len(s) {
		if textsafe.IsControlByte(s[i]) {
			buf = textsafe.AppendPercentByte(buf, s[i])
			continue
		}
		buf = append(buf, s[i])
	}
	return string(buf)
}

func assertNoHeaderInjection(field, value string) error {
	for i := range len(value) {
		if b := value[i]; b < 0x20 || b == 0x7f {
			return fmt.Errorf("%w: header %q contains illegal control byte 0x%02X (C0/DEL): refusing to send to prevent SMTP header injection",
				ErrSendFailed, field, b)
		}
	}
	for _, r := range value {
		if textsafe.IsC1(r) || textsafe.IsInvisible(r) {
			return fmt.Errorf("%w: header %q contains invisible or C1 control codepoint U+%04X: refusing to send to prevent display spoofing",
				ErrSendFailed, field, r)
		}
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: header %q is not valid UTF-8 (raw C1 control bytes?): refusing to send",
			ErrSendFailed, field)
	}
	return nil
}
