package main

import "testing"

// TestToSnakeCase pins the consolidated snake_case converter, including
// the acronym cases the two historic in-package converters got wrong in
// opposite directions: generate's toSnakeCase smashed leading acronyms
// ("HTTPServer" → "httpserver") and pack's camelToSnake split every
// uppercase letter ("HTTPServer" → "h_t_t_p_server").
func TestToSnakeCase(t *testing.T) {
	cases := []struct{ in, want string }{
		// Already snake / separator forms (historic behavior, kept).
		{"", ""},
		{"blog_posts", "blog_posts"},
		{"blog-posts", "blog_posts"},
		{"blog posts", "blog_posts"},
		{"_leading", "leading"},
		{"trailing_", "trailing"},
		{"a__b", "a_b"},
		{"-a-b-", "a_b"},

		// Plain CamelCase (historic behavior, kept).
		{"BlogPost", "blog_post"},
		{"OrderItem", "order_item"},
		{"A", "a"},
		{"AbC", "ab_c"},

		// Trailing acronyms (generate already handled these, kept).
		{"BlogPOST", "blog_post"},
		{"ParseURL", "parse_url"},

		// Leading + interior acronyms: the converged contract.
		// Old toSnakeCase: "httpserver" / "apikey"; old camelToSnake:
		// "h_t_t_p_server" / "a_p_i_key".
		{"HTTPServer", "http_server"},
		{"APIKey", "api_key"},
		{"OAuth2Token", "o_auth2_token"},
		{"UploadS3Bucket", "upload_s3_bucket"},

		// Digits: a lower/digit → upper boundary still breaks; an
		// uppercase run before a digit does not.
		{"V2Ray", "v2_ray"},
		{"HTTP2", "http2"},
		{"SHA256Sum", "sha256_sum"},

		// Adjacent acronym runs are one token: "HTTPURL" cannot be
		// split without a dictionary, and the historic generate
		// converter produced the same value here.
		{"ParseHTTPURL", "parse_httpurl"},
		{"HTTP_Server", "http_server"},
		{"api-key-Value", "api_key_value"},
	}
	for _, c := range cases {
		if got := toSnakeCase(c.in); got != c.want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestToKebabCase pins the consolidated kebab-case converter (the pack
// side of the theme-token round trip: "PrimaryFg" ↔ "primary-fg").
func TestToKebabCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"Primary", "primary"},
		{"PrimaryFg", "primary-fg"},
		{"SurfaceSoft", "surface-soft"},
		{"BorderStrong", "border-strong"},
		{"HTTPServer", "http-server"},
		{"APIKey", "api-key"},
		{"TextMuted", "text-muted"},
		{"primary", "primary"},
	}
	for _, c := range cases {
		if got := toKebabCase(c.in); got != c.want {
			t.Errorf("toKebabCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestToCamelCase pins the separator→PascalCase converter (unchanged
// behavior; it splits only on "_", "-" and " ", never inside CamelCase).
func TestToCamelCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"already", "Already"},
		{"blog_posts", "BlogPosts"},
		{"user-name", "UserName"},
		{"a b c", "ABC"},
		{"__", ""},
		{"two_fa_tokens", "TwoFaTokens"},
	}
	for _, c := range cases {
		if got := toCamelCase(c.in); got != c.want {
			t.Errorf("toCamelCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestToCamelJSONCases pins toCamelJSON (Pascal→lowerCamel for JSON
// tags). It composes toCamelCase, so CamelCase input passes through
// unsplit. That is the long-standing contract.
func TestToCamelJSONCases(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"user_name", "userName"},
		{"X", "x"},
		{"two_fa_tokens", "twoFaTokens"},
		{"APIKey", "aPIKey"},
	}
	for _, c := range cases {
		if got := toCamelJSON(c.in); got != c.want {
			t.Errorf("toCamelJSON(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLowerFirst(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"A", "a"},
		{"Hello", "hello"},
		{"XMLReader", "xMLReader"},
	}
	for _, c := range cases {
		if got := lowerFirst(c.in); got != c.want {
			t.Errorf("lowerFirst(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSnakeCamelRoundTrip pins the generator↔pack name invariant:
// snake_case names survive toCamelCase → toSnakeCase unchanged. This is
// the path typeNameToScreenName relies on when pack reverses generated
// type names back into blueprint screen names.
func TestSnakeCamelRoundTrip(t *testing.T) {
	names := []string{
		"blog_posts",
		"order_items",
		"two_fa_tokens",
		"user",
		"http_server",
		"api_key",
		"audit_log",
	}
	for _, n := range names {
		if got := toSnakeCase(toCamelCase(n)); got != n {
			t.Errorf("round trip %q: toSnakeCase(toCamelCase(%q)) = %q", n, n, got)
		}
	}
}
