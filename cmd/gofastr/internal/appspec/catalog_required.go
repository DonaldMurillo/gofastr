package appspec

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

// validateCatalogRequired refuses a catalog block missing what its
// framework/ui component panics without at render: the generator hands
// the prop straight to the component, so a block that loads here would
// 500 its whole screen on every request.
//
//   - stat_card: ui.StatCard requires Label, and Value unless a source
//     computes it. The label is props.label; text: (the entity_list
//     heading key) is accepted on any block and was silently dropped.
//   - link_button: ui.LinkButton requires Label and Href, and refuses an
//     unsafe or protocol-relative Href.
//   - hero: each CTA is a ui.LinkButton, drawn when its text is set, so
//     cta_text needs cta_href and secondary_text needs secondary_href.
//   - markdown: ui.Markdown requires Source, the block's text:.
//
// A number or bool where the component takes text decodes as int64 or
// bool, which the generator reads as "": refused with a hint to quote it.
// A hero's optional text (eyebrow, title, subtitle and both CTA labels)
// is checked the same way, since a dropped CTA label drops the CTA.
func validateCatalogRequired(screenName, kind string, block AppSpecBlock) error {
	text := func(key string) error {
		if v, ok := block.Props[key]; ok && v != nil {
			if _, isText := v.(string); !isText {
				return fmt.Errorf("app spec: screen %q %s block %s must be text; quote %v", screenName, kind, key, v)
			}
		}
		return nil
	}
	need := func(key string) error {
		if err := text(key); err != nil {
			return err
		}
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
	case "hero":
		for _, key := range []string{"eyebrow", "title", "subtitle", "cta_text", "secondary_text"} {
			if err := text(key); err != nil {
				return err
			}
		}
		for _, cta := range [][2]string{{"cta_text", "cta_href"}, {"secondary_text", "secondary_href"}} {
			if s, _ := block.Props[cta[0]].(string); s != "" {
				if err := href(cta[1]); err != nil {
					return err
				}
			}
		}
	case "markdown":
		if strings.TrimSpace(block.Text) == "" {
			return fmt.Errorf("app spec: screen %q markdown block text is required: ui.Markdown refuses to render without it", screenName)
		}
	}
	return nil
}
