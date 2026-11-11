package appspec

import "net/url"

// baseURLProblem says why app.base_url cannot be the generated app's
// sitemap origin ("" when it can). The generated app always runs uihost
// strict mode, whose invalidSitemapBaseURL panics the boot on the same
// shapes; keep the two in step. Empty is fine here: the generated
// appBaseURL() falls back to localhost.
func baseURLProblem(base string) string {
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	switch {
	case err != nil:
		return "does not parse"
	case u.Scheme != "http" && u.Scheme != "https":
		return "needs an http or https scheme"
	case u.Host == "":
		return "has no host"
	case u.User != nil:
		return "must not carry userinfo"
	case u.RawQuery != "" || u.Fragment != "" || u.ForceQuery:
		return "must not carry a query or fragment"
	case u.Path != "" && u.Path != "/":
		return "must be a bare origin with no path"
	}
	return ""
}
