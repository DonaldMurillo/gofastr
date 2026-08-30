package webbotauth

// base.go: RFC 9421 signature base assembly (section 2.5).
//
// The base is an ASCII string: one line per covered component
// ("identifier" ":" SP value "\n"), terminated by the signature
// parameters line. Component ordering, @-prefixed derived components,
// parameter serialization, and canonical whitespace in field values all
// change the signed bytes; every rule here is taken from RFC 9421 and
// pinned by the test vectors, not reasoned into existence.
//
// Errors are deliberately total: RFC 9421 section 2.5 says any
// unresolvable component (missing field, unknown derived component,
// unknown parameter, missing dictionary member, unknown query param)
// MUST fail base generation, so a signature that covers anything this
// code cannot canonicalize is unverifiable rather than partially
// verified.

import (
	"fmt"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

// requestCtx is the request side of the signature context.
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

// responseCtx is a response message plus the request that triggered it,
// needed for the req flag (RFC 9421 section 2.4) when verifying a
// directory response possession proof (draft Appendix B).
type responseCtx struct {
	status int
	header http.Header
	req    *requestCtx
}

// newRequestCtx derives the request context from an *http.Request.
//
// scheme resolution: r.URL.Scheme when set (client-side requests),
// otherwise https when the connection is TLS, else http. For a
// TLS-terminating proxy deployment where the origin sees plain HTTP,
// this can disagree with what the signer saw; signatures covering
// @scheme or @target-uri then fail to verify, which is the honest
// outcome. @authority (the recommended component) is unaffected.
func newRequestCtx(r *http.Request) *requestCtx {
	scheme := r.URL.Scheme
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	authority := normalizeAuthority(r.Host, scheme)
	path := r.URL.Path
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

// normalizeAuthority lowercases the host and drops the default port for
// the scheme (RFC 9110 section 7.4), keeping IPv6 brackets intact.
func normalizeAuthority(host, scheme string) string {
	if host == "" {
		return ""
	}
	h, port, err := splitAuthority(host)
	defaultPort := (scheme == "https" && port == "443") || (scheme == "http" && port == "80")
	switch {
	case err != nil || port == "":
		return strings.ToLower(host)
	case defaultPort:
		return strings.ToLower(h)
	default:
		if strings.Contains(h, ":") { // bare IPv6 literal
			h = "[" + h + "]"
		}
		return strings.ToLower(h) + ":" + port
	}
}

// splitAuthority splits host:port, tolerating bracketed IPv6 literals.
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

// buildSignatureBase creates the signature base (RFC 9421 section 2.5)
// over the ordered covered components and the signature parameters.
// Every component identifier must resolve against the message or the
// whole base fails.
func buildSignatureBase(req *requestCtx, covered []sfItem, params sfParams) (string, error) {
	if req == nil {
		return "", fmt.Errorf("no request context for signature base")
	}
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
		val, err := componentValue(req, comp)
		if err != nil {
			return "", err
		}
		b.WriteString(id)
		b.WriteString(": ")
		b.WriteString(val)
		b.WriteString("\n")
	}
	// The signature parameters line is always last and holds the
	// covered identifiers in the same order as the base, plus the
	// signature parameters as inner-list parameters in sender order.
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

// buildResponseSignatureBase is the response-side variant used to
// verify a directory's possession proof (draft Appendix B): components
// resolve against the response, and the req flag pulls from the request
// that fetched the directory.
func buildResponseSignatureBase(resp *responseCtx, covered []sfItem, params sfParams) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("no response context for signature base")
	}
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
		val, err := responseComponentValue(resp, comp)
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

// componentValue resolves one covered component identifier against a
// request message.
func componentValue(req *requestCtx, comp sfItem) (string, error) {
	name := comp.str
	if strings.HasPrefix(name, "@") {
		// Derived components on a request take no parameters except
		// @query-param's name; req on a request is an error outright
		// (RFC 9421 section 2.5 step 2.5).
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

// responseComponentValue resolves one covered component against a
// response, routing req-flagged components to the triggering request.
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

// stripReq returns the parameters without the req flag, for resolving a
// req-flagged component against the request context itself.
func stripReq(p sfParams) sfParams {
	out := sfParams{}
	for _, pr := range p.list {
		if pr.key != "req" {
			out.list = append(out.list, pr)
		}
	}
	return out
}

// reqParamTrue reports whether the req flag is present and true
// (bare ;req or ;req=?1; an explicit ?0 leaves the component on the
// response).
func reqParamTrue(p sfParams) bool {
	v, ok := p.get("req")
	if !ok {
		return false
	}
	return v.typ != sfBool || v.b
}

// validateFieldParams enforces the field-component parameter rules:
// req must not target a request message, trailer values are
// unobtainable here, and unknown parameters fail the base.
func validateFieldParams(p sfParams, allowReq bool) error {
	for _, pr := range p.list {
		switch pr.key {
		case "key", "sf", "bs":
			// fine, applied in fieldValue
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

// derivedComponentValue computes a derived component (RFC 9421
// section 2.2) against a request context.
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
		// Absent query renders as the '?' character alone.
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

// queryParamValue applies RFC 9421 section 2.2.8: parse the query with
// application/x-www-form-urlencoded rules, then percent-encode the
// decoded name/value pair back. A name with multiple occurrences must
// not be covered.
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

// urlencodedEncode is the WHATWG "application/x-www-form-urlencoded
// percent-encode set" referenced by RFC 9421 section 2.2.8: keep ASCII
// alphanumerics and - _ . *, percent-encode every other byte (space as
// %20, tilde and parens included).
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

// fieldValue canonicalizes an HTTP field component (RFC 9421
// section 2.1): values combined in order with ", " after stripping
// leading/trailing OWS; the key parameter selects a dictionary member
// and strictly serializes its value; sf strictly serializes the whole
// field; bs wraps each value as a byte sequence.
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
		// RFC 9421 2.1.3: multiple field lines become a List of Byte
		// Sequences, not an Inner List.
		return serializeList(items), nil
	}
	if p.has("sf") {
		combined := combineFieldValues(values)
		return strictSerializeField(combined)
	}
	return combineFieldValues(values), nil
}

// combineFieldValues joins multiple field lines with ", " after OWS
// trimming (RFC 9421 section 2.1 / RFC 9110 section 5.2).
func combineFieldValues(values []string) string {
	trimmed := make([]string, len(values))
	for i, v := range values {
		trimmed[i] = strings.Trim(v, " \t")
	}
	return strings.Join(trimmed, ", ")
}

// strictSerializeField parses a value assumed to be a Structured Field
// of unknown type and re-serializes it canonically (the sf parameter,
// RFC 9421 section 2.1.1): try Dictionary, then bare Item, then List.
func strictSerializeField(s string) (string, error) {
	if _, err := parseSFDictionary(s); err == nil {
		return serializeDictionaryString(s)
	}
	if it, err := parseSFItem(s); err == nil {
		return serializeItem(*it), nil
	}
	// List: one or more comma-separated items.
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
	// RFC 9421 2.1.1: a List field re-serializes comma-separated.
	return serializeList(items), nil
}

// serializeDictionaryString parses then canonically re-serializes a
// dictionary field value.
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
