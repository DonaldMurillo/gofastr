package urlprefix

import (
	"strings"
	"testing"
)

// benchPage is a page about the size of an entity list: a nav, a table
// of linked rows with forms, and inline script and style the tokenizer
// passes through.
func benchPage() []byte {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><link rel="stylesheet" href="/__gofastr/app.css"><style>.x{color:red}</style></head><body>`)
	b.WriteString(`<nav><a href="/">Home</a><a href="/orders">Orders</a><a href="/es/orders" hreflang="es">ES</a></nav><table>`)
	for i := range 200 {
		n := strings.Repeat("x", i%7+1)
		b.WriteString(`<tr><td><a href="/orders/` + n + `" data-cui-rpc="/orders/` + n + `/rpc">Order ` + n + `</a></td>`)
		b.WriteString(`<td><img src="/static/` + n + `.png" alt=""></td><td><form action="/orders/` + n + `/delete" method="post"><button>Delete</button></form></td></tr>`)
	}
	b.WriteString(`</table><script>window.x = "/orders";</script></body></html>`)
	return []byte(b.String())
}

var benchRule = Rule{Prefix: "/es", Locales: []string{"en", "es", "pt-br"}}

func BenchmarkHTML(b *testing.B) {
	page := benchPage()
	b.SetBytes(int64(len(page)))
	b.ReportAllocs()
	for b.Loop() {
		HTML(page, benchRule)
	}
}

func BenchmarkPath(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		Path("/orders/42?tab=items", benchRule)
	}
}
