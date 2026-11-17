// Package cssweight weighs the CSS a page ships over plain HTTP; [Measure]
// returns a [Weight] a gate compares against a cap.
package cssweight

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const maxBody = 16 << 20

// Sheet is one stylesheet the page ships.
type Sheet struct {
	// Source is the linked sheet's URL, or "inline #n" for the n-th <style>.
	Source string `json:"source"`
	Bytes  int    `json:"bytes"`
	GzipN  int    `json:"gzip"`
}

// Weight is the CSS one page ships.
type Weight struct {
	Sheets []Sheet `json:"sheets"`
}

// Bytes is the raw size of every sheet together.
func (w Weight) Bytes() int {
	n := 0
	for _, s := range w.Sheets {
		n += s.Bytes
	}
	return n
}

// Gzip is the size of every sheet together, each gzipped on its own.
func (w Weight) Gzip() int {
	n := 0
	for _, s := range w.Sheets {
		n += s.GzipN
	}
	return n
}

// Inline is the raw size of the <style> elements.
func (w Weight) Inline() int {
	n := 0
	for _, s := range w.Sheets {
		if strings.HasPrefix(s.Source, "inline #") {
			n += s.Bytes
		}
	}
	return n
}

func (w Weight) String() string {
	return fmt.Sprintf("%d sheets, %d B (%d B gzip, %d B inline)", len(w.Sheets), w.Bytes(), w.Gzip(), w.Inline())
}

// Measure fetches pageURL with c and weighs its inline and linked CSS. Any
// answer but 200, a redirect included, and a sheet using @import are errors.
func Measure(c *http.Client, pageURL string) (Weight, error) {
	page, err := url.Parse(pageURL)
	if err != nil {
		return Weight{}, err
	}
	nc := *c
	nc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c = &nc
	doc, err := fetch(c, pageURL)
	if err != nil {
		return Weight{}, err
	}
	inline, links, baseHref, err := collect(doc)
	if err != nil {
		return Weight{}, fmt.Errorf("parse %s: %w", pageURL, err)
	}
	if baseHref != "" {
		ref, err := url.Parse(baseHref)
		if err != nil {
			return Weight{}, fmt.Errorf("base href %q: %w", baseHref, err)
		}
		page = page.ResolveReference(ref)
	}
	var w Weight
	for i, css := range inline {
		src := fmt.Sprintf("inline #%d", i+1)
		if err := refuseImport(src, css); err != nil {
			return Weight{}, err
		}
		w.Sheets = append(w.Sheets, sheet(src, css))
	}
	seen := map[string]bool{}
	for _, href := range links {
		ref, err := url.Parse(href)
		if err != nil {
			return Weight{}, fmt.Errorf("stylesheet href %q: %w", href, err)
		}
		u := page.ResolveReference(ref).String()
		if seen[u] {
			continue
		}
		seen[u] = true
		css, err := fetch(c, u)
		if err != nil {
			return Weight{}, err
		}
		if err := refuseImport(u, css); err != nil {
			return Weight{}, err
		}
		w.Sheets = append(w.Sheets, sheet(u, css))
	}
	return w, nil
}

func refuseImport(source string, css []byte) error {
	if bytes.Contains(bytes.ToLower(css), []byte("@import")) {
		return fmt.Errorf("%s uses @import, which Measure does not follow", source)
	}
	return nil
}

func sheet(source string, css []byte) Sheet {
	return Sheet{Source: source, Bytes: len(css), GzipN: gzipLen(css)}
}

func fetch(c *http.Client, u string) ([]byte, error) {
	resp, err := c.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", u, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("GET %s: body over %d bytes", u, maxBody)
	}
	return body, nil
}

func collect(doc []byte) (inline [][]byte, links []string, baseHref string, err error) {
	z := html.NewTokenizer(bytes.NewReader(doc))
	inStyle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				return inline, links, baseHref, nil
			}
			return nil, nil, "", z.Err()
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch atom.Lookup(name) {
			case atom.Style:
				inStyle = true
				inline = append(inline, nil)
			case atom.Base:
				for hasAttr && baseHref == "" {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					if string(k) == "href" {
						baseHref = string(v)
					}
				}
			case atom.Link:
				var rel, href string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch string(k) {
					case "rel":
						rel = string(v)
					case "href":
						href = string(v)
					}
				}
				if href != "" && hasToken(rel, "stylesheet") {
					links = append(links, href)
				}
			}
		case html.TextToken:
			if inStyle {
				inline[len(inline)-1] = append(inline[len(inline)-1], z.Text()...)
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); atom.Lookup(name) == atom.Style {
				inStyle = false
			}
		}
	}
}

func hasToken(list, tok string) bool {
	for f := range strings.FieldsSeq(list) {
		if strings.EqualFold(f, tok) {
			return true
		}
	}
	return false
}

func gzipLen(b []byte) int {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Len()
}
