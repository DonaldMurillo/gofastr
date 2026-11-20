package webbotauth

import (
	"fmt"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

type requestCtx struct {
	method        string
	scheme        string
	authority     string // normalized: lowercase host, default port dropped
	path          string // "" means absent; @path renders it as "/"
	query         string // raw query without the leading '?'
	requestTarget string // origin-form: path + '?' query when present
	targetURI     string
	header        http.Header
}

type responseCtx struct {
	status int
	header http.Header
	req    *requestCtx
}

func newRequestCtx(r *http.Request) *requestCtx {
	scheme := r.URL.Scheme
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	authority := normalizeAuthority(r.Host, scheme)
	path := r.URL.EscapedPath()
	query := r.URL.RawQuery
	reqTarget := path
	if query != "" {
		reqTarget += "?" + query
	}
	targetURI := scheme + "://" + authority + reqTarget
	return &requestCtx{
		method:        r.Method,
		scheme:        scheme,
		authority:     authority,
		path:          path,
		query:         query,
		requestTarget: reqTarget,
		targetURI:     targetURI,
		header:        r.Header,
	}
}

func normalizeAuthority(host, scheme string) string {
	if host == "" {
		return ""
	}
	h, port, err := splitAuthority(host)
	if err != nil || port == "" {
		return strings.ToLower(host)
	}
	if strings.Contains(h, ":") { // bare IPv6 literal, with or without the default port
		h = "[" + h + "]"
	}
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return strings.ToLower(h)
	}
	return strings.ToLower(h) + ":" + port
}

func splitAuthority(host string) (string, string, error) {
	if strings.HasPrefix(host, "[") {
		end := strings.IndexByte(host, ']')
		if end < 0 {
			return "", "", fmt.Errorf("malformed authority %q", host)
		}
		h := host[1:end]
		rest := host[end+1:]
		if rest == "" {
			return h, "", nil
		}
		if !strings.HasPrefix(rest, ":") {
			return "", "", fmt.Errorf("malformed authority %q", host)
		}
		return h, rest[1:], nil
	}
	if i := strings.LastIndexByte(host, ':'); i >= 0 && !strings.Contains(host[i+1:], ":") {
		return host[:i], host[i+1:], nil
	}
	return host, "", nil
}

func buildSignatureBase(req *requestCtx, covered []sfItem, params sfParams) (string, error) {
	if req == nil {
		return "", fmt.Errorf("no request context for signature base")
	}
	return buildSignatureBaseFor(covered, params, func(comp sfItem) (string, error) {
		return componentValue(req, comp)
	})
}

func buildResponseSignatureBase(resp *responseCtx, covered []sfItem, params sfParams) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("no response context for signature base")
	}
	return buildSignatureBaseFor(covered, params, func(comp sfItem) (string, error) {
		return responseComponentValue(resp, comp)
	})
}

func buildSignatureBaseFor(covered []sfItem, params sfParams, resolve func(sfItem) (string, error)) (string, error) {
	var b strings.Builder
	seen := make(map[string]bool, len(covered))
	for _, comp := range covered {
		if comp.typ != sfString {
			return "", fmt.Errorf("covered component is not a string identifier")
		}
		id := serializeItem(comp)
		if seen[id] {
			return "", fmt.Errorf("duplicate covered component %s", id)
		}
		seen[id] = true
		if comp.str == "@signature-params" {
			return "", fmt.Errorf("@signature-params must not be a covered component")
		}
		val, err := resolve(comp)
		if err != nil {
			return "", err
		}
		b.WriteString(id)
		b.WriteString(": ")
		b.WriteString(val)
		b.WriteString("\n")
	}
	b.WriteString("\"@signature-params\": ")
	b.WriteString(serializeInnerList(sfInnerList{items: covered, params: params}))
	base := b.String()
	for i := range len(base) {
		if base[i] > 0x7f {
			return "", fmt.Errorf("signature base contains non-ASCII bytes")
		}
	}
	return base, nil
}

func componentValue(req *requestCtx, comp sfItem) (string, error) {
	name := comp.str
	if strings.HasPrefix(name, "@") {
		for _, pr := range comp.p.list {
			if pr.key == "req" {
				return "", fmt.Errorf("req parameter on a request message component %q", name)
			}
			if !(name == "@query-param" && pr.key == "name") {
				return "", fmt.Errorf("unknown parameter %q on derived component %q", pr.key, name)
			}
		}
		return derivedComponentValue(req, name, comp.p)
	}
	if err := validateFieldParams(comp.p, false); err != nil {
		return "", err
	}
	return fieldValue(req.header, name, comp.p)
}

func responseComponentValue(resp *responseCtx, comp sfItem) (string, error) {
	name := comp.str
	reqFlagged := reqParamTrue(comp.p)
	if strings.HasPrefix(name, "@") {
		for _, pr := range comp.p.list {
			if pr.key != "req" && !(name == "@query-param" && pr.key == "name") {
				return "", fmt.Errorf("unknown parameter %q on derived component %q", pr.key, name)
			}
		}
		if reqFlagged {
			if resp.req == nil {
				return "", fmt.Errorf("req parameter with no request context for %q", name)
			}
			return derivedComponentValue(resp.req, name, stripReq(comp.p))
		}
		if name == "@status" {
			return fmt.Sprintf("%d", resp.status), nil
		}
		return "", fmt.Errorf("derived component %q requires the req flag on a response", name)
	}
	if err := validateFieldParams(comp.p, true); err != nil {
		return "", err
	}
	if reqFlagged {
		if resp.req == nil {
			return "", fmt.Errorf("req parameter with no request context for field %q", name)
		}
		return fieldValue(resp.req.header, name, stripReq(comp.p))
	}
	return fieldValue(resp.header, name, comp.p)
}

func stripReq(p sfParams) sfParams {
	out := sfParams{}
	for _, pr := range p.list {
		if pr.key != "req" {
			out.list = append(out.list, pr)
		}
	}
	return out
}

func reqParamTrue(p sfParams) bool {
	v, ok := p.get("req")
	if !ok {
		return false
	}
	return v.typ != sfBool || v.b
}

func validateFieldParams(p sfParams, allowReq bool) error {
	for _, pr := range p.list {
		switch pr.key {
		case "key", "sf", "bs":
		case "req":
			if !allowReq {
				return fmt.Errorf("req parameter on a request message component")
			}
		case "tr":
			return fmt.Errorf("trailer fields are not available to this verifier")
		default:
			return fmt.Errorf("unknown parameter %q on field component", pr.key)
		}
	}
	return nil
}

func derivedComponentValue(req *requestCtx, name string, p sfParams) (string, error) {
	switch name {
	case "@method":
		return req.method, nil
	case "@target-uri":
		return req.targetURI, nil
	case "@authority":
		return req.authority, nil
	case "@scheme":
		return req.scheme, nil
	case "@request-target":
		return req.requestTarget, nil
	case "@path":
		if req.path == "" {
			return "/", nil
		}
		return req.path, nil
	case "@query":
		return "?" + req.query, nil
	case "@query-param":
		nameParam, ok := p.get("name")
		if !ok || nameParam.typ != sfString {
			return "", fmt.Errorf("@query-param requires a name string parameter")
		}
		return queryParamValue(req.query, nameParam.str)
	case "@status":
		return "", fmt.Errorf("@status is response-only")
	default:
		return "", fmt.Errorf("unknown derived component %q", name)
	}
}

func queryParamValue(rawQuery, name string) (string, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", fmt.Errorf("query parse: %w", err)
	}
	vs, ok := values[name]
	if !ok || len(vs) == 0 {
		return "", fmt.Errorf("query parameter %q not present", name)
	}
	if len(vs) > 1 {
		return "", fmt.Errorf("query parameter %q occurs %d times", name, len(vs))
	}
	return urlencodedEncode(vs[0]), nil
}

func urlencodedEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '*':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		}
	}
	return b.String()
}

func fieldValue(header http.Header, name string, p sfParams) (string, error) {
	key := textproto.CanonicalMIMEHeaderKey(name)
	values := header.Values(key)
	if len(values) == 0 {
		return "", fmt.Errorf("field %q not present in message", name)
	}
	if k, ok := p.get("key"); ok {
		if k.typ != sfString {
			return "", fmt.Errorf("key parameter on %q is not a string", name)
		}
		combined := combineFieldValues(values)
		dict, err := parseSFDictionary(combined)
		if err != nil {
			return "", fmt.Errorf("field %q is not a parseable dictionary: %w", name, err)
		}
		m := dict.get(k.str)
		if m == nil {
			return "", fmt.Errorf("dictionary member %q not present in %q", k.str, name)
		}
		s, err := serializeMemberValue(m)
		if err != nil {
			return "", fmt.Errorf("dictionary member %q of %q: %w", k.str, name, err)
		}
		return s, nil
	}
	if p.has("bs") {
		items := make([]sfItem, 0, len(values))
		for _, v := range values {
			trimmed := strings.Trim(v, " \t")
			items = append(items, sfItem{typ: sfBytes, bs: []byte(trimmed)})
		}
		return serializeList(items), nil
	}
	if p.has("sf") {
		combined := combineFieldValues(values)
		return strictSerializeField(combined)
	}
	return combineFieldValues(values), nil
}

func combineFieldValues(values []string) string {
	trimmed := make([]string, len(values))
	for i, v := range values {
		trimmed[i] = strings.Trim(v, " \t")
	}
	return strings.Join(trimmed, ", ")
}

func strictSerializeField(s string) (string, error) {
	if _, err := parseSFDictionary(s); err == nil {
		return serializeDictionaryString(s)
	}
	if it, err := parseSFItem(s); err == nil {
		return serializeItem(*it), nil
	}
	p := &sfParser{s: s}
	var items []sfItem
	p.skipOWS()
	if p.eof() {
		return "", fmt.Errorf("empty structured field")
	}
	for {
		it, err := p.parseItem()
		if err != nil {
			return "", fmt.Errorf("not a parseable structured field")
		}
		items = append(items, *it)
		p.skipOWS()
		if p.eof() {
			break
		}
		if p.peek() != ',' {
			return "", fmt.Errorf("not a parseable structured field")
		}
		p.pos++
		p.skipOWS()
		if p.eof() {
			return "", fmt.Errorf("trailing comma in structured field")
		}
	}
	return serializeList(items), nil
}

func serializeDictionaryString(s string) (string, error) {
	d, err := parseSFDictionary(s)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, m := range d.members {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(m.key)
		if !m.bare {
			b.WriteByte('=')
			if m.item != nil {
				writeItem(&b, *m.item)
			} else if m.list != nil {
				b.WriteString(serializeInnerList(*m.list))
			}
		}
	}
	return b.String(), nil
}
