package appspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

func validateCatalogRequired(screenName, kind string, block AppSpecBlock) error {
	if err := validateCatalogText(screenName, kind, block); err != nil {
		return err
	}
	need := func(key string) error {
		if s, _ := block.Props[key].(string); strings.TrimSpace(s) == "" {
			return fmt.Errorf("app spec: screen %q %s block %s is required: the component refuses to render without it", screenName, kind, key)
		}
		return nil
	}
	href := func(key string) error {
		if err := need(key); err != nil {
			return err
		}
		if h := block.Props[key].(string); !urlsafe.OK(h, urlsafe.Anchor) {
			return fmt.Errorf("app spec: screen %q %s block %s %q is not a safe link: use an http(s), mailto, fragment or root-relative URL", screenName, kind, key, h)
		}
		return nil
	}
	switch kind {
	case "stat_card":
		if err := need("label"); err != nil {
			if block.Text != "" {
				return fmt.Errorf("%w (text: %q is the entity_list heading key; a stat_card reads props.label)", err, block.Text)
			}
			return err
		}
		if _, sourced := block.Props["source"].(map[string]any); !sourced {
			return need("value")
		}
	case "link_button":
		if err := need("label"); err != nil {
			return err
		}
		return href("href")
	case "page_header":
		return need("title")
	case "hero":
		for _, cta := range [][2]string{{"cta_text", "cta_href"}, {"secondary_text", "secondary_href"}} {
			if s, _ := block.Props[cta[0]].(string); s != "" {
				if err := href(cta[1]); err != nil {
					return err
				}
			}
		}
	case "callout":
		if s, _ := block.Props["title"].(string); strings.TrimSpace(s) == "" && strings.TrimSpace(block.Text) == "" {
			return fmt.Errorf("app spec: screen %q callout block needs props.title or text: headless.Alert refuses to render a callout that says nothing", screenName)
		}
	case "pricing":
		plans, _ := block.Props["plans"].([]any)
		for i, p := range plans {
			plan, _ := p.(map[string]any)
			for _, key := range []string{"name", "price", "period", "description", "cta_text", "cta_href"} {
				if v, ok := plan[key]; ok && v != nil {
					if _, isText := v.(string); !isText {
						return fmt.Errorf("app spec: screen %q pricing block plan %d %s must be text; quote %v", screenName, i+1, key, v)
					}
				}
			}
			if v, ok := plan["features"]; ok && v != nil {
				feats, isList := v.([]any)
				if !isList {
					return fmt.Errorf("app spec: screen %q pricing block plan %d features must be a list of text", screenName, i+1)
				}
				for j, f := range feats {
					if _, isText := f.(string); !isText {
						return fmt.Errorf("app spec: screen %q pricing block plan %d features entry %d must be text; quote %v", screenName, i+1, j+1, f)
					}
				}
			}
			label, _ := plan["cta_text"].(string)
			h, _ := plan["cta_href"].(string)
			if label == "" && h == "" {
				continue
			}
			if strings.TrimSpace(h) == "" {
				return fmt.Errorf("app spec: screen %q pricing block plan %d cta_href is required with cta_text: the component refuses to render without it", screenName, i+1)
			}
			if !urlsafe.OK(h, urlsafe.Anchor) {
				return fmt.Errorf("app spec: screen %q pricing block plan %d cta_href %q is not a safe link: use an http(s), mailto, fragment or root-relative URL", screenName, i+1, h)
			}
		}
	case "markdown":
		if strings.TrimSpace(block.Text) == "" {
			return fmt.Errorf("app spec: screen %q markdown block text is required: ui.Markdown refuses to render without it", screenName)
		}
	}
	return nil
}

var catalogTextProps = map[string][]string{
	"page_header": {"title", "eyebrow", "subtitle"},
	"hero":        {"eyebrow", "title", "subtitle", "cta_text", "cta_href", "secondary_text", "secondary_href", "image", "media"},
	"section":     {"heading", "eyebrow", "description", "label", "class", "id"},
	"card":        {"heading", "text"},
	"stat_card":   {"label", "value"},
	"bar_chart":   {"title"},
	"pie_chart":   {"title"},
	"line_chart":  {"title"},
	"link_button": {"label", "href", "variant"},
	"callout":     {"title"},
}

// CatalogTextProps returns the props a catalog kind takes as text.
func CatalogTextProps(kind string) []string {
	return slices.Clone(catalogTextProps[kind])
}

func validateCatalogText(screenName, kind string, block AppSpecBlock) error {
	for _, key := range catalogTextProps[kind] {
		if _, sourced := block.Props["source"].(map[string]any); sourced && kind == "stat_card" && key == "value" {
			continue
		}
		if v, ok := block.Props[key]; ok && v != nil {
			if _, isText := v.(string); !isText {
				return fmt.Errorf("app spec: screen %q %s block %s must be text; quote %v", screenName, kind, key, v)
			}
		}
	}
	return nil
}
