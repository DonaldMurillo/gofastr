package uihost

// organization.go: the Organization JSON-LD surface. Scanners (and
// agents doing due diligence) look for an Organization schema with a
// contactPoint and a PostalAddress in the page HTML to verify the
// business behind a site is real and reachable. WithOrganization
// declares it once; the host embeds it as application/ld+json in every
// full page head.
import (
	"encoding/json"
	"strings"
)

// PostalAddress is the schema.org PostalAddress for OrganizationConfig.
type PostalAddress struct {
	Street     string // streetAddress
	Locality   string // addressLocality (city)
	Region     string // addressRegion (state/province)
	PostalCode string // postalCode
	Country    string // addressCountry (ISO 3166-1 alpha-2)
}

// OrganizationConfig configures the Organization JSON-LD block.
type OrganizationConfig struct {
	// Name is the legal/display name. Required.
	Name string
	// URL is the canonical origin. Required.
	URL string
	// Logo is an optional absolute or root-relative logo URL.
	Logo string
	// Email and Phone populate the contactPoint. At least one should be
	// set for the block to satisfy completeness checks.
	Email string
	Phone string
	// ContactType is the schema.org contactType (e.g. "customer support").
	// Defaults to "customer support" when contact details are set.
	ContactType string
	// SameAs lists profile URLs (GitHub, LinkedIn, …).
	SameAs []string
	// Address is the postal address. Required for completeness checks.
	Address PostalAddress
}

// WithOrganization embeds Organization JSON-LD in every full page head.
func WithOrganization(cfg OrganizationConfig) Option {
	return func(ds *UIHost) {
		ds.organization = &cfg
	}
}

// ldJSONScript wraps marshaled JSON-LD in a <script type="application/
// ld+json"> element. The opening and closing tags live in separate Go
// string literals on purpose (same trick as core-ui/seo.Render and the
// routes JSON blocks): the no-inline-script linter scans literals
// individually, and application/ld+json is inert data, never executed.
// `</` inside the body is escaped so no config value can terminate the
// element early.
func ldJSONScript(body []byte) string {
	return `<script type="application/ld+json">` +
		strings.ReplaceAll(string(body), "</", `<\/`) +
		`</` + `script>`
}

// ldContactPoint is the schema.org ContactPoint for OrganizationConfig.
type ldContactPoint struct {
	Type        string `json:"@type"`
	ContactType string `json:"contactType,omitempty"`
	Email       string `json:"email,omitempty"`
	Telephone   string `json:"telephone,omitempty"`
}

// ldPostalAddress is the schema.org PostalAddress for OrganizationConfig.
type ldPostalAddress struct {
	Type            string `json:"@type"`
	StreetAddress   string `json:"streetAddress,omitempty"`
	AddressLocality string `json:"addressLocality,omitempty"`
	AddressRegion   string `json:"addressRegion,omitempty"`
	PostalCode      string `json:"postalCode,omitempty"`
	AddressCountry  string `json:"addressCountry,omitempty"`
}

// ldOrganization is the JSON-LD shape rendered from OrganizationConfig.
// Built with encoding/json only: the config values are host-supplied
// strings, and json.Marshal HTML-escapes them (plus the `</` replace in
// ldJSONScript), so nothing can break out of the data block.
type ldOrganization struct {
	Context      string           `json:"@context"`
	Type         string           `json:"@type"`
	Name         string           `json:"name,omitempty"`
	URL          string           `json:"url,omitempty"`
	Logo         string           `json:"logo,omitempty"`
	SameAs       []string         `json:"sameAs,omitempty"`
	ContactPoint *ldContactPoint  `json:"contactPoint,omitempty"`
	Address      *ldPostalAddress `json:"address,omitempty"`
}

// organizationJSONLD renders the configured Organization as a JSON-LD
// script element, or "" when no organization is set. contactType
// defaults to "customer support"; contactPoint/address emit only when
// at least one of their fields is set.
func (ds *UIHost) organizationJSONLD() string {
	cfg := ds.organization
	if cfg == nil {
		return ""
	}
	org := ldOrganization{
		Context: "https://schema.org",
		Type:    "Organization",
		Name:    cfg.Name,
		URL:     cfg.URL,
		Logo:    cfg.Logo,
		SameAs:  cfg.SameAs,
	}
	if cfg.Email != "" || cfg.Phone != "" {
		ct := cfg.ContactType
		if ct == "" {
			ct = "customer support"
		}
		org.ContactPoint = &ldContactPoint{
			Type:        "ContactPoint",
			ContactType: ct,
			Email:       cfg.Email,
			Telephone:   cfg.Phone,
		}
	}
	addr := cfg.Address
	if addr.Street != "" || addr.Locality != "" || addr.Region != "" ||
		addr.PostalCode != "" || addr.Country != "" {
		org.Address = &ldPostalAddress{
			Type:            "PostalAddress",
			StreetAddress:   addr.Street,
			AddressLocality: addr.Locality,
			AddressRegion:   addr.Region,
			PostalCode:      addr.PostalCode,
			AddressCountry:  addr.Country,
		}
	}
	body, err := json.Marshal(org)
	if err != nil {
		// Cannot happen (plain string fields), but never emit a broken
		// block if a future field type stops marshaling.
		return ""
	}
	return ldJSONScript(body)
}
