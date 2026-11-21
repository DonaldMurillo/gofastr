package handler

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const (
	maxCSPReportBody    = 64 << 10
	maxCSPReportsPerReq = 16
	maxCSPReportField   = 256
)

// CSPReport is one browser Content-Security-Policy or Trusted Types
// violation, read from either report format, with every field scrubbed of
// control bytes and capped, and URLs cut at their query.
type CSPReport struct {
	DocumentURL        string
	BlockedURL         string
	EffectiveDirective string
	Disposition        string // "enforce" or "report"
	Sample             string
	SourceFile         string
	LineNumber         int
	ColumnNumber       int
	StatusCode         int
}

type legacyCSPReport struct {
	DocumentURI        string `json:"document-uri"`
	BlockedURI         string `json:"blocked-uri"`
	EffectiveDirective string `json:"effective-directive"`
	ViolatedDirective  string `json:"violated-directive"`
	Disposition        string `json:"disposition"`
	ScriptSample       string `json:"script-sample"`
	SourceFile         string `json:"source-file"`
	LineNumber         int    `json:"line-number"`
	ColumnNumber       int    `json:"column-number"`
	StatusCode         int    `json:"status-code"`
}

type reportingAPIReport struct {
	Type string `json:"type"`
	Body struct {
		DocumentURL        string `json:"documentURL"`
		BlockedURL         string `json:"blockedURL"`
		EffectiveDirective string `json:"effectiveDirective"`
		Disposition        string `json:"disposition"`
		Sample             string `json:"sample"`
		SourceFile         string `json:"sourceFile"`
		LineNumber         int    `json:"lineNumber"`
		ColumnNumber       int    `json:"columnNumber"`
		StatusCode         int    `json:"statusCode"`
	} `json:"body"`
}

// CSPReportHandler answers a POST of application/reports+json (the
// Reporting API) or application/csp-report (report-uri) with 204 and calls
// fn once per CSP violation it holds, at most 16 per request. Another
// method is 405, another type 415, and a body over 64 KiB, one that does
// not parse, or one UnmarshalStrict refuses (a duplicate or case-folded
// key, invalid UTF-8) is 400.
func CSPReportHandler(fn func(*http.Request, CSPReport)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if ct != "application/reports+json" && ct != "application/csp-report" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxCSPReportBody+1))
		if err != nil || len(body) > maxCSPReportBody {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reports, ok := parseCSPReports(ct, body)
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if fn != nil {
			for _, rep := range reports {
				fn(r, rep)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func parseCSPReports(ct string, body []byte) ([]CSPReport, bool) {
	if ct == "application/csp-report" {
		var env map[string]legacyCSPReport
		if UnmarshalStrict(body, &env) != nil {
			return nil, false
		}
		l, ok := env["csp-report"]
		if !ok {
			return nil, false
		}
		directive := l.EffectiveDirective
		if directive == "" {
			directive, _, _ = strings.Cut(strings.TrimSpace(l.ViolatedDirective), " ")
		}
		return []CSPReport{cleanCSPReport(CSPReport{
			DocumentURL: l.DocumentURI, BlockedURL: l.BlockedURI, EffectiveDirective: directive,
			Disposition: l.Disposition, Sample: l.ScriptSample, SourceFile: l.SourceFile,
			LineNumber: l.LineNumber, ColumnNumber: l.ColumnNumber, StatusCode: l.StatusCode,
		})}, true
	}
	var list []reportingAPIReport
	if UnmarshalStrict(body, &list) != nil {
		return nil, false
	}
	var out []CSPReport
	for _, e := range list {
		if e.Type != "csp-violation" {
			continue
		}
		if len(out) == maxCSPReportsPerReq {
			break
		}
		b := e.Body
		out = append(out, cleanCSPReport(CSPReport{
			DocumentURL: b.DocumentURL, BlockedURL: b.BlockedURL, EffectiveDirective: b.EffectiveDirective,
			Disposition: b.Disposition, Sample: b.Sample, SourceFile: b.SourceFile,
			LineNumber: b.LineNumber, ColumnNumber: b.ColumnNumber, StatusCode: b.StatusCode,
		}))
	}
	return out, true
}

func cleanCSPReport(c CSPReport) CSPReport {
	c.DocumentURL = cspField(cspURL(c.DocumentURL))
	c.BlockedURL = cspField(cspURL(c.BlockedURL))
	c.SourceFile = cspField(cspURL(c.SourceFile))
	c.EffectiveDirective = cspField(c.EffectiveDirective)
	c.Disposition = cspField(c.Disposition)
	c.Sample = cspField(c.Sample)
	return c
}

func cspURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" {
		s, _, _ = strings.Cut(s, "?")
		s, _, _ = strings.Cut(s, "#")
		return s
	}
	u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment, u.User = "", false, "", "", nil
	return u.String()
}

func cspField(s string) string {
	return textsafe.Truncate(textsafe.ScrubControlBytes(s), maxCSPReportField)
}
