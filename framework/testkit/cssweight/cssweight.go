// Package cssweight measures the CSS a GoFastr page makes the browser
// fetch or parse: every <style> element in the document and every
// <link rel="stylesheet"> it names, fetched once each.
//
// [Measure] works over plain HTTP, with no browser, so a gate can weigh
// every page an app serves in seconds. Sizes are reported raw and
// gzip-compressed, each sheet compressed on its own. For a linked sheet
// that is its transfer size. Inline sheets travel inside the document's
// one gzip stream, so their figure runs a little high, but it moves only
// when the CSS does, which is what a cap needs. A gate compares
// [Weight.Gzip] against a cap the app picks
// from today's numbers, so CSS growth has to raise the cap in the same
// change instead of arriving unnoticed:
//
//	w, err := cssweight.Measure(client, base+"/pricing")
//	if err != nil { t.Fatal(err) }
//	if w.Gzip() > 20<<10 { t.Errorf("/pricing ships %s", w) }
//
// Each app keeps its own page list and cap, the way axetest and nojstest
// are used.
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

// maxBody bounds one fetched document or stylesheet.
const maxBody = 16 << 20

// Sheet is one stylesheet the page ships.
type Sheet struct {
	// Source is the linked sheet's resolved URL, or "inline #n" for the
	// n-th <style> element (counting from 1).
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

// Gzip is the compressed size of every sheet together, each compressed
// on its own.
func (w Weight) Gzip() int {
	n := 0
	for _, s := range w.Sheets {
		n += s.GzipN
	}
	return n
}

// Inline is the raw size of the <style> elements: the part a return visit
// downloads again inside every document, where a linked sheet comes from
// the cache.
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

// Measure fetches pageURL with c and weighs its CSS. A linked sheet is
// fetched from the URL the page names, resolved against the page's
// <base href> or the page itself, and a sheet linked twice counts once.
// Any answer other than 200, for the page or a sheet, is an error, and so
// is a redirect, which Measure does not follow: a gate that weighed an
// error page, or the login page a lost session lands on, would pass for
// the wrong reason. A sheet that uses @import is an error too, since the
// CSS it pulls in would go unweighed.
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

// refuseImport errors on a sheet that uses @import. It matches the
// keyword anywhere, comments included, so it can refuse a sheet that only
// mentions it; that is the safe side for a gate.
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

// collect returns the text of each <style> element and the href of each
// <link> whose rel includes "stylesheet", in document order, and the
// first <base href>, the one the browser uses.
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

// hasToken reports whether the space-separated list holds tok, ASCII
// case-insensitively, the way the HTML spec reads rel.
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
