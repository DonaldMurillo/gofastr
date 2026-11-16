package uihost

import (
	"encoding/json"
	"strings"
)

// PostalAddress is the schema.org PostalAddress of an OrganizationConfig.
type PostalAddress struct {
	Street     string
	Locality   string
	Region     string
	PostalCode string
	Country    string
}

// OrganizationConfig configures the Organization JSON-LD block.
type OrganizationConfig struct {
	Name string
	URL  string
	Logo string
	// Email and Phone fill the contactPoint, emitted when either is set.
	Email string
	Phone string
	// ContactType defaults to "customer support".
	ContactType string
	SameAs      []string
	Address     PostalAddress
}

// WithOrganization embeds Organization JSON-LD in every full page head.
func WithOrganization(cfg OrganizationConfig) Option {
	return func(ds *UIHost) {
		ds.organization = &cfg
	}
}

func ldJSONScript(body []byte) string {
	return `<script type="application/ld+json">` +
		strings.ReplaceAll(string(body), "</", `<\/`) +
		`</` + `script>`
}

type ldContactPoint struct {
	Type        string `json:"@type"`
	ContactType string `json:"contactType,omitempty"`
	Email       string `json:"email,omitempty"`
	Telephone   string `json:"telephone,omitempty"`
}

type ldPostalAddress struct {
	Type            string `json:"@type"`
	StreetAddress   string `json:"streetAddress,omitempty"`
	AddressLocality string `json:"addressLocality,omitempty"`
	AddressRegion   string `json:"addressRegion,omitempty"`
	PostalCode      string `json:"postalCode,omitempty"`
	AddressCountry  string `json:"addressCountry,omitempty"`
}

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
		return ""
	}
	return ldJSONScript(body)
}
